package agent

// The loop harness: instead of one long conversation, an objective is run
// as a cycle the harness controls —
//
//	plan → for each step: work → review → (retry | split | ask the user)
//	     → verify the whole project → (add a fix step | finish)
//
// Every phase gets a fresh, short context holding only what it needs: the
// objective, the plan with progress marks, short notes from earlier steps,
// and the one thing to do now. That keeps a 9B model inside the part of its
// context window it actually handles well. Success is judged by tool
// results and a real verification command, not by the worker's own claims.

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/perms"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepDone    StepStatus = "done"
	StepFailed  StepStatus = "failed"
)

// LoopStep is one small unit of work in a loop run.
type LoopStep struct {
	Title    string     `json:"title"`
	Status   StepStatus `json:"status"`
	Attempts int        `json:"attempts"`
	// Depth is 1 for steps made by splitting a step that kept failing;
	// those are not split again.
	Depth int `json:"depth,omitempty"`
	// Context is extra input for the worker that is too long for the
	// checklist (e.g. the failing verification output).
	Context string `json:"context,omitempty"`
	// Notes say what was done, for the steps that follow.
	Notes string `json:"notes,omitempty"`
	// Failure is why the latest attempt was rejected.
	Failure string `json:"failure,omitempty"`
}

// Loop run states.
const (
	LoopRunning = "running"
	LoopDone    = "done"
	LoopBlocked = "blocked" // needs the user; resume with their answer
	LoopPaused  = "paused"  // interrupted or out of iterations; resumable
	LoopFailed  = "failed"
)

// LoopState is the whole run; it is saved after every change so a run can
// be resumed after Ctrl+C, a question to the user, or a restart.
type LoopState struct {
	Objective  string      `json:"objective"`
	Project    string      `json:"project"`
	Verify     string      `json:"verify,omitempty"`
	Steps      []*LoopStep `json:"steps"`
	Iteration  int         `json:"iteration"`
	Fixes      int         `json:"fixes"`
	HumanNotes []string    `json:"human_notes,omitempty"`
	Status     string      `json:"status"`
	Outcome    string      `json:"outcome,omitempty"`
	Started    time.Time   `json:"started"`
	Updated    time.Time   `json:"updated"`
	// Track is the domain/flavor the run follows.
	Track Track `json:"track"`
	// WorkDir is the folder with plan.md, checklist.md, walkthrough.md.
	WorkDir string `json:"work_dir,omitempty"`
	// FinaleAdded is set once the track's final steps (security test,
	// report, …) have been added after the plan passed verification.
	FinaleAdded bool `json:"finale_added,omitempty"`
	// Reviewed is set once the final multi-pass review has run; Reviews
	// holds one line per pass.
	Reviewed bool     `json:"reviewed,omitempty"`
	Reviews  []string `json:"reviews,omitempty"`
}

// Current returns the step being worked on next, or nil.
func (st *LoopState) Current() *LoopStep {
	for _, s := range st.Steps {
		if s.Status != StepDone {
			return s
		}
	}
	return nil
}

// Progress returns done and total step counts.
func (st *LoopState) Progress() (done, total int) {
	for _, s := range st.Steps {
		if s.Status == StepDone {
			done++
		}
	}
	return done, len(st.Steps)
}

// Checklist renders the plan with progress marks: ✓ done, ▶ current,
// ✗ last attempt failed, · pending.
func (st *LoopState) Checklist() string {
	var b strings.Builder
	cur := st.Current()
	for i, s := range st.Steps {
		mark := "·"
		switch {
		case s.Status == StepDone:
			mark = "✓"
		case s == cur && s.Failure != "":
			mark = "✗"
		case s == cur:
			mark = "▶"
		}
		fmt.Fprintf(&b, "%s %d. %s\n", mark, i+1, s.Title)
	}
	return b.String()
}

// LoopEvents is implemented by front ends that draw loop progress. Others
// get progress as notices.
type LoopEvents interface {
	Events
	LoopUpdate(st *LoopState, msg string)
}

func report(ev Events, st *LoopState, msg string) {
	if le, ok := ev.(LoopEvents); ok {
		le.LoopUpdate(st, msg)
		return
	}
	ev.Notice(msg)
}

// LoopOptions starts or resumes a run.
type LoopOptions struct {
	// Steps seeds the plan (e.g. from plan mode) so planning is skipped.
	Steps []string
	// Verify overrides the configured / detected verification command;
	// "none" disables verification.
	Verify string
	// Resume continues the saved run for this project.
	Resume bool
	// Notes is the user's answer when resuming a blocked run.
	Notes string
}

var (
	zero = 0.0
	low  = 0.2
)

// RunLoop drives an objective to completion with the loop harness. It
// returns the final state; a cancelled context pauses the run (resumable).
func (s *Session) RunLoop(ctx context.Context, objective string, opt LoopOptions, ev Events) (*LoopState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lc := s.app.Config.Get().Loop
	cwd, _ := os.Getwd()

	var st *LoopState
	if opt.Resume {
		var err error
		if st, err = LoadLoop(cwd); err != nil {
			return nil, err
		}
		if n := strings.TrimSpace(opt.Notes); n != "" {
			st.HumanNotes = append(st.HumanNotes, n)
		}
		for _, step := range st.Steps {
			if step.Status == StepFailed {
				step.Status, step.Attempts = StepPending, 0
			}
		}
		if st.Track.Domain != "" {
			s.Track = st.Track
		}
		if st.WorkDir != "" {
			if rel, err := filepath.Rel(cwd, st.WorkDir); err == nil {
				s.Work = &Workspace{Dir: st.WorkDir, Rel: filepath.ToSlash(rel)}
			}
		}
	} else {
		st = &LoopState{Objective: strings.TrimSpace(objective), Project: cwd, Started: time.Now(), Track: s.Track}
		for _, t := range opt.Steps {
			st.Steps = append(st.Steps, &LoopStep{Title: t, Status: StepPending})
		}
		s.Work = nil // every run gets its own work folder
	}
	if w := s.EnsureWork(st.Objective); w != nil {
		st.WorkDir = w.Dir
	}
	save := func() {
		saveLoop(st)
		s.Work.loopFiles(st)
	}
	switch {
	case opt.Verify != "":
		st.Verify = opt.Verify
	case !opt.Resume && lc.Verify != "":
		st.Verify = lc.Verify
	case !opt.Resume:
		st.Verify = DetectVerify(cwd)
	}
	if st.Verify == "none" {
		st.Verify = ""
	}
	st.Status, st.Outcome = LoopRunning, ""
	save()

	finish := func(status, outcome string) (*LoopState, error) {
		st.Status, st.Outcome = status, outcome
		save()
		s.Work.walkthrough(st)
		report(ev, st, outcome)
		// Leave a record in the conversation so the user can ask about it.
		s.History = append(s.History,
			llm.Message{Role: llm.RoleUser, Content: "[loop] " + st.Objective},
			llm.Message{Role: llm.RoleAssistant, Content: LoopSummary(st)})
		return st, nil
	}
	pause := func(err error) (*LoopState, error) {
		st.Status, st.Outcome = LoopPaused, "paused — /loop resume to continue"
		save()
		return st, err
	}

	if len(st.Steps) == 0 {
		report(ev, st, "planning")
		steps, err := s.planLoop(ctx, st, ev)
		if err != nil {
			if ctx.Err() != nil {
				return pause(ctx.Err())
			}
			st.Status, st.Outcome = LoopFailed, err.Error()
			save()
			return st, err
		}
		for _, t := range steps {
			st.Steps = append(st.Steps, &LoopStep{Title: t, Status: StepPending})
		}
		save()
	}
	verifyNote := "no verification command (set loop.verify in config or /loop verify <cmd>)"
	if st.Verify != "" {
		verifyNote = "verify with: " + st.Verify
	}
	report(ev, st, fmt.Sprintf("plan ready · %d steps · %s", len(st.Steps), verifyNote))

	for iters := 0; ; {
		step := st.Current()
		if step == nil {
			// Every step is done: prove it with the verification command,
			// then run the track's final steps, then review the whole result.
			outcome := "done — all steps complete"
			verified := false
			if st.Verify != "" {
				report(ev, st, "verifying: "+st.Verify)
				out, ok, skipped := s.runVerify(ctx, st.Verify, ev)
				if ctx.Err() != nil {
					return pause(ctx.Err())
				}
				switch {
				case skipped:
					outcome = "done — all steps complete (verification skipped)"
				case ok:
					outcome, verified = "done — all steps complete and `"+st.Verify+"` passes", true
				default:
					if st.Fixes >= 3 {
						return finish(LoopFailed, "stopped — `"+st.Verify+"` still fails after 3 fix attempts")
					}
					st.Fixes++
					st.Steps = append(st.Steps, &LoopStep{
						Title:   fmt.Sprintf("Fix the failures so `%s` passes", st.Verify),
						Status:  StepPending,
						Context: "The verification command currently fails with:\n" + tail(out, 2500),
					})
					save()
					continue
				}
			}
			if finale := st.Track.FlavorSpec().Finale; !st.FinaleAdded && len(finale) > 0 {
				st.FinaleAdded = true
				for _, t := range finale {
					st.Steps = append(st.Steps, &LoopStep{Title: t, Status: StepPending})
				}
				save()
				report(ev, st, fmt.Sprintf("plan passed — adding %d final step(s) for %s", len(finale), st.Track))
				continue
			}
			if !st.Reviewed {
				st.Reviewed = true
				changed := s.finalReview(ctx, st, ev)
				if ctx.Err() != nil {
					st.Reviewed = false
					return pause(ctx.Err())
				}
				save()
				if changed && verified {
					// The review changed files: verify again (and add fix
					// steps if that now fails) before finishing.
					report(ev, st, "the final review changed things — verifying again")
					continue
				}
			}
			return finish(LoopDone, outcome)
		}
		if iters >= lc.MaxIterations {
			return finish(LoopPaused, fmt.Sprintf("paused after %d iterations (loop.max_iterations) — /loop resume to keep going", iters))
		}
		iters++
		st.Iteration++
		step.Attempts++
		idx := stepIndex(st, step)
		label := fmt.Sprintf("step %d/%d", idx+1, len(st.Steps))
		if step.Attempts > 1 {
			label += fmt.Sprintf(" · attempt %d", step.Attempts)
		}
		report(ev, st, label+": "+step.Title)

		w := work{}
		reply, err := s.workStep(ctx, st, step, &w, ev)
		switch {
		case ctx.Err() != nil:
			step.Attempts--
			return pause(ctx.Err())
		case errors.Is(err, errRoundLimit):
			step.Failure = fmt.Sprintf("ran out of tool rounds (%d) before finishing — do less per call and finish sooner", lc.StepRounds)
		case err != nil:
			step.Attempts--
			st.Status, st.Outcome = LoopFailed, err.Error()
			save()
			return st, err
		default:
			if q := blockedQuestion(reply); q != "" {
				step.Attempts--
				return finish(LoopBlocked, "needs you: "+q+" — answer with /loop resume <answer>")
			}
			pass, reason, rerr := s.reviewStep(ctx, st, step, reply, &w, ev)
			if ctx.Err() != nil {
				return pause(ctx.Err())
			}
			if rerr != nil {
				pass, reason = w.fallbackVerdict()
			}
			if pass {
				step.Status, step.Failure = StepDone, ""
				step.Notes = w.notes(reply)
				report(ev, st, fmt.Sprintf("✓ %s — %s", label, reason))
				save()
				continue
			}
			step.Failure = reason
		}
		report(ev, st, fmt.Sprintf("✗ %s — %s", label, step.Failure))

		if step.Attempts >= lc.StepAttempts {
			if step.Depth > 0 {
				step.Status = StepFailed
				return finish(LoopBlocked, fmt.Sprintf("stuck on step %d after %d attempts: %s — help with /loop resume <hint>", idx+1, step.Attempts, step.Failure))
			}
			subs, err := s.splitStep(ctx, st, step, ev)
			if ctx.Err() != nil {
				return pause(ctx.Err())
			}
			if err != nil || len(subs) < 2 {
				step.Status = StepFailed
				return finish(LoopBlocked, fmt.Sprintf("stuck on step %d after %d attempts: %s — help with /loop resume <hint>", idx+1, step.Attempts, step.Failure))
			}
			var repl []*LoopStep
			for _, t := range subs {
				repl = append(repl, &LoopStep{Title: t, Status: StepPending, Depth: 1, Context: step.Context})
			}
			st.Steps = append(st.Steps[:idx], append(repl, st.Steps[idx+1:]...)...)
			report(ev, st, fmt.Sprintf("split step %d into %d smaller steps", idx+1, len(subs)))
		}
		save()
	}
}

// child returns a fresh session for one phase. It shares the parent's
// "always allow" answers so the user is not asked twice.
func (s *Session) child() *Session {
	c := s.app.NewSession()
	c.allowed = s.allowed
	c.noTools = s.noTools
	c.Track = s.Track
	c.Work = s.Work
	return c
}

func (s *Session) absorb(c *Session) {
	s.Usage.InputTokens = c.Usage.InputTokens
	s.Usage.OutputTokens += c.Usage.OutputTokens
	s.noTools = s.noTools || c.noTools
}

const plannerRules = `
## Loop harness: planner
You plan work that a small model will carry out one step at a time with a fresh memory each step.
So each step must stand on its own: say which file(s) to touch and what to change in plain words.
Keep steps small — one change in one or two files. Put tests or checks in their own step when useful.
Every step must CHANGE something or CHECK something. Do not make steps that only list or read files — each worker reads what it needs itself.
Do not repeat a step. Fewer steps is better: if the objective is one small change, the plan can be 1–2 steps.
Write steps as plain sentences, never as tool calls.`

func (s *Session) planLoop(ctx context.Context, st *LoopState, ev Events) ([]string, error) {
	c := s.child()
	defer s.absorb(c)
	var b strings.Builder
	fmt.Fprintf(&b, "OBJECTIVE: %s\n\n", st.Objective)
	b.WriteString("Make a plan to reach this objective in this project.\n")
	b.WriteString("First take a quick look at the relevant files (list_dir, glob, grep, read_file) — only a few calls.\n")
	b.WriteString("Then reply with ONLY a numbered list of 2–8 steps.")
	if f := st.Track.FlavorSpec(); len(f.Phases) > 0 {
		fmt.Fprintf(&b, "\nThe steps must follow the %s loop from the system prompt, in order (merge or skip phases that don't apply).", st.Track)
	}
	if st.Verify != "" {
		fmt.Fprintf(&b, "\nAt the end the harness runs `%s` to check the work.", st.Verify)
	}
	reply, err := c.send(ctx, llm.Message{Content: b.String()}, ev, turnOpts{mode: ModePlan, extra: plannerRules, maxRounds: 8, temperature: &low})
	if errors.Is(err, errRoundLimit) || (err == nil && len(ParseSteps(reply)) == 0) {
		// Out of rounds, or prose instead of a list: ask once more, no tools.
		reply, err = c.send(ctx, llm.Message{Content: "Stop exploring. Reply now with ONLY the numbered list of steps."}, ev,
			turnOpts{mode: ModePlan, extra: plannerRules, maxRounds: 1, noTools: true, temperature: &low})
	}
	if err != nil {
		return nil, err
	}
	steps := ParseSteps(reply)
	if len(steps) == 0 {
		steps = []string{st.Objective} // no usable plan: treat it as one step
	}
	return c.reviewPlan(ctx, steps, ev), nil
}

// planLenses are the angles the planner re-checks its plan from.
var planLenses = []string{
	"order and dependencies — can each step be done using only what the steps before it produce? Where code is added, does the caller come before the function it calls?",
	"completeness — if every step is done, is the objective fully reached, including the tests, docs, checks or report the domain loop requires?",
	"size — is every step one small, concrete change that can be finished in one go? Split big steps and merge trivial ones",
	"scope — does any step do something the objective does not need right now? Remove it",
}

// reviewPlan has the planner go over its plan Review.Passes times (at
// least 4 by default), each time from a different angle, and returns the
// corrected plan.
func (s *Session) reviewPlan(ctx context.Context, steps []string, ev Events) []string {
	cfg := s.app.Config.Get().Review
	if cfg.Passes <= 0 {
		return steps
	}
	lastOK := true
	for i := 0; i < cfg.MaxPasses; i++ {
		if i >= cfg.Passes && lastOK {
			break
		}
		lens := planLenses[i%len(planLenses)]
		var b strings.Builder
		fmt.Fprintf(&b, "[Plan review pass %d of at least %d]\nLens: %s\n\nThe plan:\n", i+1, cfg.Passes, lens)
		for j, t := range steps {
			fmt.Fprintf(&b, "%d. %s\n", j+1, t)
		}
		b.WriteString("\nCheck the plan from this angle only. If you find a real mistake, reply with ONLY the corrected full numbered list. Otherwise reply with one line: REVIEW OK: <what you checked>.")
		reply, err := s.send(ctx, llm.Message{Content: b.String()}, quiet{ev}, turnOpts{
			mode: ModePlan, extra: plannerRules, maxRounds: 1, noTools: true, temperature: &low,
		})
		if err != nil {
			return steps
		}
		total := cfg.Passes
		if i >= cfg.Passes {
			total = cfg.MaxPasses
		}
		ok, why := reviewOK(reply)
		fixed := ParseSteps(reply)
		if !ok && (len(fixed) == 0 || slices.Equal(fixed, steps)) {
			ok, why = true, "plan unchanged"
		}
		lastOK = ok
		if ok {
			reviewReport(ev, i+1, total, "plan "+lensName(lens), true, why)
			continue
		}
		reviewReport(ev, i+1, total, "plan "+lensName(lens), false, fmt.Sprintf("plan revised: %d → %d steps", len(steps), len(fixed)))
		steps = fixed
	}
	return steps
}

// finalReview goes over the finished run several times, in a fresh
// context, with tools so it can check and fix the actual files. It
// reports whether anything was changed.
func (s *Session) finalReview(ctx context.Context, st *LoopState, ev Events) bool {
	if s.app.Config.Get().Review.Passes <= 0 {
		return false
	}
	report(ev, st, "final review of the whole result")
	c := s.child()
	defer s.absorb(c)
	pre := fmt.Sprintf("OBJECTIVE: %s\n\nThe loop harness has finished every step. What was done:\n%s\nYou are now reviewing the finished work. Read the files that were changed; do not trust the summary.",
		st.Objective, clip(LoopSummary(st), 3500))
	res := c.selfReview(ctx, ModeBuild, pre, "", ev)
	st.Reviews = res.notes
	return res.changed
}

const workerRules = `
## Loop harness: you are doing ONE step of a larger plan
- Do only the CURRENT STEP. Later steps are handled later, by you, with a fresh memory.
- Use tools to make the change. Describing a change is not making it.
- Read a file before editing it, and copy old_string for edit_file exactly from what read_file showed.
- After changing something, check it: read it back, or build / run the tests.
- Finish with 1–3 sentences saying what you changed.
- If you need information only the user has (requirements, credentials, a decision), call ask_user and continue with the answer. If you truly cannot continue at all, reply with one line starting with BLOCKED: and your question.`

// work records what a worker did during one attempt.
type work struct {
	calls   []string
	lastErr bool
	files   []string
}

func (w *work) record(call llm.ToolCall, summary, result string, isErr bool) {
	status := "ok"
	if isErr {
		status = "ERROR"
	}
	w.lastErr = isErr
	w.calls = append(w.calls, fmt.Sprintf("%d. %s %s — %s: %s", len(w.calls)+1, call.Name, oneLineClip(summary, 120), status, oneLineClip(result, 240)))
	if !isErr && (call.Name == "write_file" || call.Name == "edit_file") {
		var a struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(call.Args, &a)
		if a.Path != "" && !contains(w.files, a.Path) {
			w.files = append(w.files, a.Path)
		}
	}
}

// notes is what later steps learn about this one.
func (w *work) notes(reply string) string {
	n := oneLineClip(reply, 400)
	if len(w.files) > 0 {
		n += " [files: " + strings.Join(w.files, ", ") + "]"
	}
	return n
}

// fallbackVerdict is used when the reviewer gives no usable answer.
func (w *work) fallbackVerdict() (bool, string) {
	switch {
	case len(w.calls) == 0:
		return false, "no tools were used, so nothing was checked or changed"
	case w.lastErr:
		return false, "the last tool call failed"
	}
	return true, "tools ran without errors (reviewer gave no verdict)"
}

func (s *Session) workStep(ctx context.Context, st *LoopState, step *LoopStep, w *work, ev Events) (string, error) {
	c := s.child()
	defer s.absorb(c)
	var b strings.Builder
	fmt.Fprintf(&b, "OBJECTIVE: %s\n\nPLAN (✓ done, ▶ current):\n%s\n", st.Objective, st.Checklist())
	var notes []string
	for i, x := range st.Steps {
		if x.Status == StepDone && x.Notes != "" {
			notes = append(notes, fmt.Sprintf("- step %d: %s", i+1, x.Notes))
		}
	}
	if len(notes) > 0 {
		// Only the most recent notes: older ones matter less and cost context.
		if len(notes) > 6 {
			notes = notes[len(notes)-6:]
		}
		fmt.Fprintf(&b, "WHAT EARLIER STEPS DID:\n%s\n\n", strings.Join(notes, "\n"))
	}
	if len(st.HumanNotes) > 0 {
		fmt.Fprintf(&b, "NOTES FROM THE USER:\n- %s\n\n", strings.Join(st.HumanNotes, "\n- "))
	}
	if step.Failure != "" {
		fmt.Fprintf(&b, "YOUR PREVIOUS ATTEMPT AT THIS STEP WAS REJECTED: %s\nCheck the current state of the files first, then fix that.\n\n", step.Failure)
	}
	if step.Context != "" {
		fmt.Fprintf(&b, "%s\n\n", step.Context)
	}
	fmt.Fprintf(&b, "CURRENT STEP (%d of %d): %s", stepIndex(st, step)+1, len(st.Steps), step.Title)
	if st.Verify != "" {
		fmt.Fprintf(&b, "\n\nThe project's check command is `%s`.", st.Verify)
	}
	return c.send(ctx, llm.Message{Content: b.String()}, ev, turnOpts{
		mode: ModeBuild, extra: workerRules, maxRounds: s.app.Config.Get().Loop.StepRounds, onTool: w.record,
	})
}

const reviewerSystem = `You check whether a worker really finished one step of a coding task.
Judge from the tool results, not from what the worker says about itself.
A step that needed a change is not done unless an edit_file / write_file / bash call that makes the change succeeded.
Reply with exactly one line, either
PASS: <short reason>
or
FAIL: <what is missing or broken, as an instruction for the next attempt>`

var reVerdict = regexp.MustCompile(`(?i)\b(PASS|FAIL)(?:ED)?\b\s*[:\-–—]?\s*(.*)`)

func (s *Session) reviewStep(ctx context.Context, st *LoopState, step *LoopStep, reply string, w *work, ev Events) (bool, string, error) {
	c := s.child()
	defer s.absorb(c)
	var b strings.Builder
	fmt.Fprintf(&b, "OBJECTIVE: %s\nSTEP TO CHECK: %s\n\nTOOL CALLS:\n", st.Objective, step.Title)
	calls := w.calls
	if len(calls) > 20 {
		calls = append([]string{fmt.Sprintf("(%d earlier calls omitted)", len(calls)-20)}, calls[len(calls)-20:]...)
	}
	if len(calls) == 0 {
		b.WriteString("(none — the worker made no tool calls)\n")
	} else {
		b.WriteString(strings.Join(calls, "\n") + "\n")
	}
	fmt.Fprintf(&b, "\nWORKER'S FINAL MESSAGE:\n%s\n\nWas the step done? One line: PASS: … or FAIL: …", clip(reply, 1500))
	system := reviewerSystem
	if rules := st.Track.FlavorSpec().Rules; len(rules) > 0 {
		// The track's hard rules (e.g. no anticipation code) are part of done.
		system += "\nFAIL the step if the tool calls show it broke one of these rules:\n- " + strings.Join(rules, "\n- ")
	}
	verdict, err := c.send(ctx, llm.Message{Content: b.String()}, quiet{ev}, turnOpts{
		maxRounds: 1, noTools: true, temperature: &zero, system: system,
	})
	if err != nil {
		return false, "", err
	}
	m := reVerdict.FindStringSubmatch(verdict)
	if m == nil {
		return false, "", errors.New("no verdict")
	}
	reason := oneLineClip(m[2], 300)
	if reason == "" {
		reason = strings.ToLower(m[1])
	}
	return strings.EqualFold(m[1], "PASS"), reason, nil
}

func (s *Session) splitStep(ctx context.Context, st *LoopState, step *LoopStep, ev Events) ([]string, error) {
	c := s.child()
	defer s.absorb(c)
	prompt := fmt.Sprintf("OBJECTIVE: %s\n\nThis step has failed %d times:\n%s\nLatest problem: %s\n\nBreak it into 2–4 smaller steps that are each easy to finish on their own. Reply with ONLY a numbered list.",
		st.Objective, step.Attempts, step.Title, step.Failure)
	reply, err := c.send(ctx, llm.Message{Content: prompt}, quiet{ev}, turnOpts{
		mode: ModePlan, extra: plannerRules, maxRounds: 1, noTools: true, temperature: &low,
	})
	if err != nil {
		return nil, err
	}
	return ParseSteps(reply), nil
}

// runVerify runs the verification command through the normal permission
// gate. skipped is true when the user declined to run it.
func (s *Session) runVerify(ctx context.Context, cmd string, ev Events) (out string, ok, skipped bool) {
	bash := s.app.Tools.Get("bash")
	if bash == nil {
		return "", false, true
	}
	args, _ := json.Marshal(map[string]any{"command": cmd, "timeout": 600})
	call := llm.ToolCall{ID: fmt.Sprintf("verify_%d", time.Now().UnixNano()), Name: "bash", Args: args}
	cfg := s.app.Config.Get()
	out, isErr := s.runTool(ctx, call, map[string]*tools.Tool{"bash": bash}, cfg.AutoApprove || s.app.Yolo, ev)
	if isErr && strings.HasPrefix(out, "The user denied") {
		return out, false, true
	}
	failed := isErr || strings.Contains(out, "\n[exit: ") || strings.HasSuffix(out, "[timed out]")
	return out, !failed, false
}

// blockedQuestion returns the question when the worker said BLOCKED:.
func blockedQuestion(reply string) string {
	for _, line := range strings.Split(reply, "\n") {
		l := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "*_#> "))
		if len(l) >= 8 && strings.EqualFold(l[:8], "BLOCKED:") {
			if q := strings.TrimSpace(strings.Trim(l[8:], "*_ ")); q != "" {
				return q
			}
			return "the worker needs your input"
		}
	}
	return ""
}

// LoopSummary is a plain-text report of a run.
func LoopSummary(st *LoopState) string {
	done, total := st.Progress()
	var b strings.Builder
	fmt.Fprintf(&b, "Loop %s: %s\nObjective: %s\nSteps: %d/%d done · %d iterations\n\n%s", st.Status, st.Outcome, st.Objective, done, total, st.Iteration, st.Checklist())
	for i, x := range st.Steps {
		if x.Notes != "" {
			fmt.Fprintf(&b, "\nstep %d: %s", i+1, x.Notes)
		}
	}
	return b.String()
}

// DetectVerify guesses a project's build/test command from its files.
func DetectVerify(dir string) string {
	has := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case has("go.mod"):
		return "go build ./... && go test ./..."
	case has("Cargo.toml"):
		return "cargo test"
	case has("package.json"):
		var p struct {
			Scripts map[string]string `json:"scripts"`
		}
		if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil && json.Unmarshal(data, &p) == nil {
			if t := p.Scripts["test"]; t != "" && !strings.Contains(t, "no test specified") {
				return "npm test"
			}
			if p.Scripts["build"] != "" {
				return "npm run build"
			}
		}
	case has("pyproject.toml") || has("pytest.ini") || has("setup.py") || has("setup.cfg"):
		if has("tests") || has("test") {
			return "python -m pytest -q"
		}
	case has("Makefile"):
		if data, err := os.ReadFile(filepath.Join(dir, "Makefile")); err == nil && regexp.MustCompile(`(?m)^test:`).Match(data) {
			return "make test"
		}
	}
	return ""
}

// loopPath is where a project's loop state is kept (~/.asg/loops/<hash>.json),
// outside the project so runs never leave files in the repo.
func loopPath(project string) string {
	sum := sha1.Sum([]byte(perms.ProjectKey(project)))
	return filepath.Join(config.Dir(), "loops", hex.EncodeToString(sum[:])[:16]+".json")
}

// LoadLoop reads the saved loop run for a project.
func LoadLoop(project string) (*LoopState, error) {
	data, err := os.ReadFile(loopPath(project))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("no loop run saved for this project — start one with /loop <objective>")
	}
	if err != nil {
		return nil, err
	}
	var st LoopState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("read loop state: %w", err)
	}
	return &st, nil
}

func saveLoop(st *LoopState) {
	st.Updated = time.Now()
	p := loopPath(st.Project)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	data, _ := json.MarshalIndent(st, "", "  ")
	tmp := p + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

func stepIndex(st *LoopState, step *LoopStep) int {
	for i, x := range st.Steps {
		if x == step {
			return i
		}
	}
	return -1
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

func oneLineClip(s string, n int) string {
	return clip(strings.Join(strings.Fields(s), " "), n)
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// quiet forwards everything except streamed text, so reviewer and
// splitter chatter does not clutter the screen.
type quiet struct{ Events }

func (quiet) Text(string) {}
