// Package tui is the interactive terminal front end, built on Bubble Tea,
// Bubbles, Lip Gloss, Huh, Glamour and Harmonica. Read AGENTS.md before
// changing it.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"golang.org/x/term"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/dashboard"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/media"
	itools "github.com/kxngHADES/asg-harness/internal/tools"
)

type attachment struct {
	name string
	img  llm.Image
}

type REPL struct {
	app  *agent.App
	sess *agent.Session
	prog *tea.Program
	out  *outbox

	// stateMu guards sess.Mode and sess.Track, which the view reads while
	// the worker may change them.
	stateMu sync.RWMutex

	mu        sync.Mutex
	cancel    context.CancelFunc
	pending   []attachment
	lastReply string

	spinID atomic.Int64
	md     *mdStream // worker only
	spin   *spinner  // worker only
}

func New(app *agent.App) *REPL {
	return &REPL{app: app, sess: app.NewSession()}
}

type command struct {
	name, args, help string
}

// needsArg reports whether a command is useless without arguments, so the
// '/' menu inserts it for editing instead of running it.
func (c command) needsArg() bool { return strings.HasPrefix(c.args, "<") }

var commands = []command{
	{"/help", "", "show this help"},
	{"/model", "[name]", "pick a model from a menu, or set one by name"},
	{"/provider", "[name]", "pick a provider (ollama, anthropic, openai, ...) and then its model"},
	{"/mode", "[build|plan|loop|ask]", "pick how ASG works (or press shift+tab to cycle)"},
	{"/domain", "[software|docs|cyber|devops|pm] [flavor]", "pick what kind of work ASG does and its loop (or press ctrl+t to cycle)"},
	{"/software", "[work|project|hobby] [request]", "software domain: work = only code this build needs, caller first, test → doc → verify → security"},
	{"/docs", "[update|write] [request]", "docs domain: screenshot the rendered docs, fix, re-screenshot"},
	{"/cyber", "[red|blue] [request]", "security domain: authorized red or blue team work, ends with a full report"},
	{"/devops", "[review|pipeline] [request]", "DevOps domain: review and fix CI/CD pipelines"},
	{"/pm", "[plan|sprint|status] [request]", "project manager domain: plans, sprints, risks, status"},
	{"/plan", "[request]", "plan mode: investigate read-only and write a step-by-step plan"},
	{"/build", "[request]", "build mode: make changes (the default)"},
	{"/ask", "[question]", "ask mode: read-only questions about your code"},
	{"/loop", "[objective] | resume [answer] | status | verify <cmd>", "run an objective plan → do → verify → review until done"},
	{"/review", "[passes|off]", "how many times ASG re-checks every answer (default 4)"},
	{"/work", "", "show the work folder: plan, checklist, walkthrough, activity log"},
	{"/dashboard", "", "open the browser dashboard (chat, config, skills)"},
	{"/init", "", "write an ASG.md describing this project for future sessions"},
	{"/memory", "[note]", "browse saved memories, or ask ASG to remember something"},
	{"/permissions", "", `review and revoke remembered "always allow" answers`},
	{"/paste", "", "attach an image from the clipboard (or press Ctrl+V)"},
	{"/image", "<path>", "attach an image file (you can also drag files into the terminal)"},
	{"/models", "", "list models available from the active provider"},
	{"/voice", "[on|off|auto|…]", "speak replies aloud and choose who speaks (/voice help)"},
	{"/speak", "", "read the last reply aloud"},
	{"/skills", "", "list installed skills"},
	{"/skill", "add <src> | rm <name> | show <name>", "import a skill (path, URL, github:owner/repo[/path]) or remove one"},
	{"/tools", "", "list tools the model can call (reloads script tools)"},
	{"/tool", "new <name>", "create a script tool in ~/.asg/tools (see tools/README.md)"},
	{"/yolo", "", "toggle YOLO: run tools without asking (or press ctrl+y)"},
	{"/config", "", "show config file location and active settings"},
	{"/usage", "", "show token usage for this session"},
	{"/clear", "", "start a new conversation"},
	{"/exit", "", "quit"},
}

// --- output -------------------------------------------------------------

// post hands a message to the program from any goroutine without blocking.
func (r *REPL) post(msg tea.Msg) {
	if r.out != nil {
		r.out.post(msg)
	}
}

// println prints s into the scrollback above the live area.
func (r *REPL) println(s string) { r.post(tea.Println(s)()) }

// printf prints formatted text into the scrollback. One trailing newline is
// implied, as with the old line-based output.
func (r *REPL) printf(format string, a ...any) {
	r.println(strings.TrimSuffix(fmt.Sprintf(format, a...), "\n"))
}

// spinner is the working animation for one stretch of waiting.
type spinner struct {
	r    *REPL
	id   int64
	once sync.Once
}

func (r *REPL) startSpinner(label string) *spinner {
	s := &spinner{r: r, id: r.spinID.Add(1)}
	r.post(busyMsg{id: s.id, label: label})
	return s
}

func (s *spinner) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() { s.r.post(busyMsg{id: s.id}) })
}

// --- lifecycle ----------------------------------------------------------

// Run starts the interactive program and returns when the user quits.
func (r *REPL) Run() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("stdin is not a terminal; use -p for one-shot prompts")
	}
	detectBackground()
	r.prog = tea.NewProgram(newModel(r))
	r.out = newOutbox(r.prog)
	defer r.out.close()
	debugf("tui start")
	_, err := r.prog.Run()
	r.interrupt()
	debugf("tui exit err=%v", err)
	if errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

// interrupt stops the running turn, if any, and any speech.
func (r *REPL) interrupt() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.mu.Unlock()
	r.app.Speaker.Stop()
}

func (r *REPL) addPending(a attachment) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, a)
	return len(r.pending)
}

func (r *REPL) pendingCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pending)
}

func (r *REPL) hasPending() bool { return r.pendingCount() > 0 }

func (r *REPL) takePending() []attachment {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.pending
	r.pending = nil
	return p
}

// handle runs one submitted input on the worker goroutine and reports
// whether the user asked to quit.
func (r *REPL) handle(line string) (quit bool) {
	defer func() {
		if p := recover(); p != nil {
			debugf("panic handling %q: %v", line, p)
			r.printf("  %s", red(fmt.Sprintf("✕ internal error: %v", p)))
		}
	}()
	line = strings.TrimSpace(line)
	if line == "" && !r.hasPending() {
		return false
	}
	line = normalizeCommand(line)
	debugf("input %q", oneLine(line, 80))
	if strings.HasPrefix(line, "/") && !media.IsImagePath(strings.Fields(line)[0]) {
		return r.command(line)
	}
	r.send(line)
	return false
}

// bareCommands are words that, typed alone, almost certainly mean the
// command rather than a message to the model.
var bareCommands = map[string]bool{"clear": true, "exit": true, "quit": true, "help": true}

// normalizeCommand forgives "//model" typos and bare "clear"/"exit"/….
func normalizeCommand(line string) string {
	if strings.HasPrefix(line, "//") && !strings.HasPrefix(line, "///") {
		return line[1:]
	}
	if bareCommands[strings.ToLower(line)] {
		return "/" + strings.ToLower(line)
	}
	return line
}

// yolo reports whether tool calls currently run without asking.
func (r *REPL) yolo() bool { return r.app.Yolo || r.app.Config.Get().AutoApprove }

// toggleYolo flips YOLO for this run. Turning it off also clears a saved
// auto_approve so "off" always means off.
func (r *REPL) toggleYolo() bool {
	if r.yolo() {
		r.app.Yolo = false
		if r.app.Config.Get().AutoApprove {
			r.update(func(c *config.Config) error { c.AutoApprove = false; return nil })
		}
		return false
	}
	r.app.Yolo = true
	return true
}

func (r *REPL) yoloLine(on bool) string {
	if on {
		return "  " + badge(colAmber, "⚡ YOLO ON") + " " + amber("tools run without asking until you press ctrl+y again")
	}
	return "  " + slate("YOLO off — tool calls ask first")
}

func (r *REPL) commandNames() []string {
	var names []string
	for _, c := range commands {
		names = append(names, c.name)
	}
	for _, s := range r.app.Skills.List() {
		names = append(names, "/"+s.Name)
	}
	sort.Strings(names)
	return names
}

func (r *REPL) send(text string) {
	text, imgs, names := media.ExtractImages(text)
	for i, img := range imgs {
		r.addPending(attachment{name: names[i], img: img})
	}
	msg := llm.Message{Content: text}
	pending := r.takePending()
	var ns []string
	for _, a := range pending {
		msg.Images = append(msg.Images, a.img)
		ns = append(ns, a.name)
	}
	if len(ns) > 0 {
		r.printf("  %s", magenta("▣ attached "+strings.Join(ns, ", ")))
	}
	if strings.TrimSpace(msg.Content) == "" && len(msg.Images) > 0 {
		msg.Content = "Describe this image."
	}
	if r.mode() == agent.ModeLoop {
		if len(msg.Images) > 0 {
			r.printf("  %s", amber("! images are not passed to loop runs — describe what matters in the objective"))
		}
		r.loop(msg.Content, agent.LoopOptions{})
		return
	}
	r.run(msg)
}

// loop runs the loop harness on an objective (or resumes a saved run).
func (r *REPL) loop(objective string, opt agent.LoopOptions) {
	r.app.Speaker.Stop()
	r.md = r.newMDStream()
	r.spin = r.startSpinner("starting loop")
	start := time.Now()
	_, err := r.withCancel(func(ctx context.Context) (string, error) {
		_, err := r.sess.RunLoop(ctx, objective, opt, r)
		return "", err
	})
	r.spin.Stop()
	r.md.Flush()
	switch {
	case errors.Is(err, context.Canceled):
		r.printf("  %s %s\n", amber("■ loop paused"), slate("— /loop resume picks up where it stopped"))
		return
	case err != nil:
		r.printf("  %s\n", red(fmt.Sprintf("✕ %v", err)))
		return
	}
	u := r.sess.Usage
	r.printf("  %s", slate(fmt.Sprintf("── loop %.0fs · %d↓ tokens generated ──", time.Since(start).Seconds(), u.OutputTokens)))
	r.workHint()
}

// workHint points at the work folder after a turn that has one.
func (r *REPL) workHint() {
	if w := r.sess.Work; w.Exists() && r.mode() != agent.ModePlan && r.mode() != agent.ModeAsk {
		r.println(fit("  " + slate("work folder: ") + lime(w.Rel+"/") + " " + slate("(plan · checklist · walkthrough · activity — /work)")))
	}
}

func reviewState(c config.ReviewConfig) string {
	if c.Passes < 0 {
		return "off"
	}
	return fmt.Sprintf("%d passes (up to %d)", c.Passes, c.MaxPasses)
}

// withCancel runs fn with a context that Ctrl+C cancels.
func (r *REPL) withCancel(fn func(ctx context.Context) (string, error)) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancel = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.cancel = nil
		r.mu.Unlock()
		cancel()
	}()
	return fn(ctx)
}

func (r *REPL) run(msg llm.Message) {
	r.app.Speaker.Stop()
	r.md = r.newMDStream()
	r.spin = r.startSpinner("thinking")
	start := time.Now()
	reply, err := r.withCancel(func(ctx context.Context) (string, error) {
		return r.sess.Send(ctx, msg, r)
	})
	r.spin.Stop()
	r.md.Flush()
	switch {
	case errors.Is(err, context.Canceled):
		r.printf("  %s", amber("■ stopped"))
		return
	case err != nil:
		r.printf("  %s", red(fmt.Sprintf("✕ %v", err)))
		return
	}
	u := r.sess.Usage
	r.printf("  %s", slate(fmt.Sprintf("── %.1fs · %d↑ %d↓ tokens ──", time.Since(start).Seconds(), u.InputTokens, u.OutputTokens)))
	r.workHint()
	if r.mode() == agent.ModePlan && r.sess.Plan != "" && reply == r.sess.Plan {
		r.printf("  %s%s%s%s%s", slate("plan saved · "), lime("/loop"), slate(" runs it step by step with checks · "),
			lime("shift+tab"), slate(" to build mode works through it with you"))
	}
	r.lastReply = reply
	if r.app.Config.Get().Audio.Enabled {
		r.speak(reply)
	}
}

func (r *REPL) speak(text string) {
	done := r.app.Speaker.Speak(text, r.app.ResolveVoice(context.Background()))
	go func() {
		if err := <-done; err != nil {
			r.printf("%s", yellow(fmt.Sprintf("audio: %v", err)))
		}
	}()
}

// --- agent.Events -------------------------------------------------------

func (r *REPL) Text(delta string) {
	r.spin.Stop()
	r.md.Write(delta)
}

func (r *REPL) ToolStart(call llm.ToolCall, summary string) {
	r.spin.Stop()
	r.md.Flush()
	r.printf("  %s %s", cyanBold("◆ "+strings.ToUpper(call.Name)), slate(oneLine(summary, termWidth()-len(call.Name)-8)))
	r.spin = r.startSpinner("running " + call.Name)
}

func (r *REPL) ToolEnd(call llm.ToolCall, result string, isErr bool) {
	r.spin.Stop()
	paint, mark := slate, "┊"
	if isErr {
		paint, mark = red, "✕"
	}
	var lines []string
	for _, l := range strings.Split(preview(result, 4), "\n") {
		lines = append(lines, "  "+paint(mark+" "+l))
	}
	r.println(strings.Join(lines, "\n"))
	r.md = r.newMDStream()
	r.spin = r.startSpinner("thinking")
}

func (r *REPL) Notice(msg string) {
	r.spin.Stop()
	r.md.Flush()
	r.printf("  %s", amber("! "+msg))
}

// LoopUpdate draws loop harness progress (agent.LoopEvents).
func (r *REPL) LoopUpdate(st *agent.LoopState, msg string) {
	r.spin.Stop()
	r.md.Flush()
	paint := amber
	switch {
	case strings.HasPrefix(msg, "✓"):
		paint = green
	case strings.HasPrefix(msg, "✗"):
		paint = red
	}
	pre := ""
	if strings.HasPrefix(msg, "step ") {
		pre = "\n"
	}
	r.printf("%s  %s %s", pre, badge(colAmber, "⟳ LOOP"), paint(msg))
	if strings.HasPrefix(msg, "plan ready") || strings.HasPrefix(msg, "split ") || st.Status != agent.LoopRunning {
		r.printChecklist(st)
	}
	r.md = r.newMDStream()
	r.spin = r.startSpinner("working")
}

func (r *REPL) printChecklist(st *agent.LoopState) {
	var out []string
	for _, l := range strings.Split(strings.TrimRight(st.Checklist(), "\n"), "\n") {
		paint := slate
		switch {
		case strings.HasPrefix(l, "✓"):
			paint = green
		case strings.HasPrefix(l, "▶"):
			paint = amber
		case strings.HasPrefix(l, "✗"):
			paint = red
		}
		out = append(out, "    "+paint(oneLine(l, termWidth()-6)))
	}
	r.println(strings.Join(out, "\n"))
}

// Approve is the permission gate: the request is printed into the
// scrollback and answered with a Huh select (or y / a / g / n, or ctrl+y).
func (r *REPL) Approve(ctx context.Context, call llm.ToolCall, gate agent.Gate) agent.Decision {
	r.spin.Stop()
	r.md.Flush()
	detail := gate.Summary
	if call.Name == "write_file" || call.Name == "edit_file" {
		detail = describeEdit(call)
	}
	box := amberBold("GATE") + " " + bold(strings.ToUpper(call.Name)) + " requests permission\n" + strings.TrimRight(detail, "\n")
	r.println("\n" + gateBox.Render(box))

	dec := agent.Allow
	opts := []huh.Option[agent.Decision]{huh.NewOption("y  run it", agent.Allow)}
	keys := map[string]func(){"y": func() { dec = agent.Allow }, "n": func() { dec = agent.Deny }}
	if gate.Scope != "" {
		opts = append(opts,
			huh.NewOption("a  always allow "+gate.Scope+" in this project", agent.AlwaysAllow),
			huh.NewOption("g  always allow "+gate.Scope+" everywhere", agent.AlwaysAllowGlobal))
		keys["a"] = func() { dec = agent.AlwaysAllow }
		keys["g"] = func() { dec = agent.AlwaysAllowGlobal }
	}
	opts = append(opts, huh.NewOption("n  block", agent.Deny))
	sel := huh.NewSelect[agent.Decision]().
		Title("Allow " + strings.ToUpper(call.Name) + "?").
		Description("ctrl+y turns on YOLO and runs it").
		Options(opts...).Value(&dec)
	yoloed := false
	ok := r.runForm(huh.NewForm(huh.NewGroup(sel)), func(req *formReq) {
		req.hotkeys = keys
		req.onYolo = func() {
			if !r.yolo() {
				r.toggleYolo()
			}
			dec, yoloed = agent.Allow, true
		}
	})
	if !ok {
		r.interrupt()
		return agent.Deny
	}
	switch {
	case yoloed:
		r.println(r.yoloLine(true))
	case dec == agent.AlwaysAllow:
		r.printf("  %s", slate("remembered: "+gate.Scope+" is always allowed in this project (/permissions to undo)"))
	case dec == agent.AlwaysAllowGlobal:
		r.printf("  %s", slate("remembered: "+gate.Scope+" is always allowed everywhere (/permissions to undo)"))
	}
	if dec != agent.Deny {
		r.spin = r.startSpinner("running " + call.Name)
	}
	return dec
}

func describeEdit(call llm.ToolCall) string {
	var a map[string]any
	_ = json.Unmarshal(call.Args, &a)
	var b strings.Builder
	fmt.Fprintf(&b, "%v\n", a["path"])
	if old, ok := a["old_string"].(string); ok {
		for _, l := range strings.Split(preview(old, 6), "\n") {
			b.WriteString(red("- "+strings.TrimSpace(l)) + "\n")
		}
		nw, _ := a["new_string"].(string)
		for _, l := range strings.Split(preview(nw, 6), "\n") {
			b.WriteString(green("+ "+strings.TrimSpace(l)) + "\n")
		}
	} else if c, ok := a["content"].(string); ok {
		for _, l := range strings.Split(preview(c, 8), "\n") {
			b.WriteString(green("+ "+strings.TrimSpace(l)) + "\n")
		}
	}
	return b.String()
}

// --- slash commands -----------------------------------------------------

func (r *REPL) command(line string) (quit bool) {
	fields := strings.Fields(line)
	cmd, args := fields[0], fields[1:]
	rest := strings.TrimSpace(strings.TrimPrefix(line, cmd))
	ctx := context.Background()

	switch cmd {
	case "/exit", "/quit":
		return true
	case "/help":
		r.printHelp()
	case "/clear":
		r.sess.Clear()
		r.takePending()
		r.printf("%s", slate("conversation cleared"))
	case "/paste":
		img, err := media.ClipboardImage()
		if err != nil {
			r.printf("%s", yellow(err.Error()))
			return
		}
		r.addPending(attachment{name: "clipboard", img: img})
		r.printf("  %s", magenta(fmt.Sprintf("▣ image attached (%s, %d KB) — type your message", img.MediaType, len(img.Data)/1024)))
	case "/image":
		_, imgs, names := media.ExtractImages(rest)
		if len(imgs) == 0 {
			r.printf("%s", yellow(fmt.Sprintf("no image found at %q", rest)))
			return
		}
		for i, img := range imgs {
			r.addPending(attachment{name: names[i], img: img})
		}
		r.printf("  %s", magenta("▣ attached "+strings.Join(names, ", ")))
	case "/model":
		if len(args) == 0 {
			r.pickModel(ctx)
			return
		}
		r.update(func(c *config.Config) error { c.Providers[c.ActiveProvider].Model = rest; return nil })
		r.printf("%s", slate("model → "+rest))
	case "/models":
		p, cur, err := r.app.Provider()
		if err == nil {
			var models []string
			if models, err = p.ListModels(ctx); err == nil {
				var out []string
				for _, m := range models {
					mark := "  "
					if m == cur {
						mark = green("● ")
					}
					out = append(out, mark+m)
				}
				r.println(strings.Join(out, "\n"))
			}
		}
		if err != nil {
			r.printf("%s", red(err.Error()))
		}
	case "/provider", "/providers":
		cfg := r.app.Config.Get()
		if len(args) == 0 {
			r.pickProvider(ctx)
			return
		}
		if _, ok := cfg.Providers[args[0]]; !ok {
			r.printf("%s", red(fmt.Sprintf("no provider %q — add it in /dashboard or %s", args[0], r.app.Config.Path())))
			return
		}
		r.update(func(c *config.Config) error { c.ActiveProvider = args[0]; return nil })
		r.printf("%s", slate(fmt.Sprintf("provider → %s (%s)", args[0], r.app.Config.Get().Providers[args[0]].Model)))
	case "/init":
		r.initProject()
	case "/memory", "/memories":
		if rest != "" {
			r.send("Remember this for future sessions (save it with memory_save, choosing a fitting name and scope): " + rest)
			return
		}
		r.memoryMenu()
	case "/permissions", "/perms":
		r.permissionsMenu()
	case "/voice":
		r.voiceCommand(ctx, args)
	case "/speak":
		if r.lastReply == "" {
			r.printf("%s", slate("nothing to speak yet"))
			return
		}
		r.speak(r.lastReply)
	case "/skills":
		list := r.app.Skills.List()
		if len(list) == 0 {
			r.printf("%s", slate("no skills installed — try /skill add github:anthropics/skills/skills/pdf"))
			return
		}
		var out []string
		for _, s := range list {
			out = append(out, fmt.Sprintf("  %s %s", magenta(fmt.Sprintf("%-16s", s.Name)), slate(oneLine(s.Description, termWidth()-22))))
		}
		r.println(strings.Join(out, "\n"))
	case "/skill":
		r.skillCommand(ctx, args)
	case "/tools":
		r.listTools()
	case "/tool":
		if len(args) < 2 || args[0] != "new" {
			r.printf("usage: /tool new <name>  — creates a script tool in %s", agent.ToolDirs()[1])
			return
		}
		dir, err := itools.ScaffoldScriptTool(agent.ToolDirs()[1], args[1])
		if err != nil {
			r.printf("%s", red(err.Error()))
			return
		}
		r.app.ReloadTools()
		r.printf("  %s — edit tool.json and run.py, then /tools to reload", green("✓ created "+dir))
	case "/domain", "/domains":
		if len(args) == 0 {
			r.pickDomain()
			return
		}
		d, ok := agent.ParseDomain(args[0])
		if !ok {
			r.printf("%s", yellow(fmt.Sprintf("unknown domain %q — software, docs, cyber, devops or pm", args[0])))
			return
		}
		r.domainCommand(d, args[1:], strings.TrimSpace(strings.TrimPrefix(rest, args[0])))
	case "/software", "/docs", "/cyber", "/devops", "/pm":
		d, _ := agent.ParseDomain(strings.TrimPrefix(cmd, "/"))
		r.domainCommand(d, args, rest)
	case "/review":
		r.reviewCommand(args)
	case "/work":
		r.workCommand()
	case "/yolo":
		r.println(r.yoloLine(r.toggleYolo()))
	case "/mode", "/modes":
		if len(args) == 0 {
			r.pickMode()
			return
		}
		m, ok := agent.ParseMode(args[0])
		if !ok {
			r.printf("%s", yellow(fmt.Sprintf("unknown mode %q — build, plan, loop or ask", args[0])))
			return
		}
		r.modeCommand(m, strings.TrimSpace(strings.TrimPrefix(rest, args[0])))
	case "/plan":
		r.modeCommand(agent.ModePlan, rest)
	case "/build":
		r.modeCommand(agent.ModeBuild, rest)
	case "/ask":
		r.modeCommand(agent.ModeAsk, rest)
	case "/loop":
		r.loopCommand(args, rest)
	case "/dashboard":
		url, err := dashboard.Ensure(r.app)
		if err != nil {
			r.printf("%s", red(err.Error()))
			return
		}
		r.printf("%s", slate("dashboard running at "+url))
		dashboard.OpenBrowser(url)
	case "/config":
		r.printConfig()
	case "/usage":
		u := r.sess.Usage
		r.printf("  last context: %d tokens · generated: %d tokens · messages: %d", u.InputTokens, u.OutputTokens, len(r.sess.History))
	default:
		name := strings.TrimPrefix(cmd, "/")
		if sk := r.app.Skills.Get(name); sk != nil {
			req := rest
			if req == "" {
				req = "Follow the skill's instructions."
			}
			r.printf("  %s", magenta("✦ skill "+sk.Name))
			r.send(agent.SkillPrompt(sk) + "\n\n---\nUser request: " + req)
			return
		}
		r.printf("%s", yellow("unknown command "+cmd+" — /help lists commands"))
	}
	return false
}

func (r *REPL) printHelp() {
	var out []string
	out = append(out, "", "  "+badge(colLime, "COMMANDS"))
	for _, c := range commands {
		out = append(out, fmt.Sprintf("  %s %-36s %s", lime(fmt.Sprintf("%-13s", c.name)), c.args, slate(c.help)))
	}
	if sk := r.app.Skills.List(); len(sk) > 0 {
		out = append(out, "", "  "+badge(colLime, "SKILLS")+" "+slate("/<name> [request]"))
		for _, s := range sk {
			out = append(out, fmt.Sprintf("  %s %s", magenta(fmt.Sprintf("/%-20s", s.Name)), slate(oneLine(s.Description, termWidth()-26))))
		}
	}
	out = append(out, "", slate("Tips: ctrl+j (or end a line with \\) for a new line · paste multi-line text freely · ↑/↓ recall earlier inputs · @path/to/img.png attaches an image"), "")
	r.println(strings.Join(out, "\n"))
}

func (r *REPL) listTools() {
	errs := r.app.ReloadTools()
	var out []string
	for _, t := range r.app.Tools.All() {
		flag := "      "
		if t.NeedsApproval {
			flag = yellow("asks  ")
		}
		src := t.Source
		if src != "builtin" && src != "bundled" {
			src = "script"
		}
		out = append(out, fmt.Sprintf("  %s %s %s%s", cyan(fmt.Sprintf("%-16s", t.Spec.Name)), slate(fmt.Sprintf("%-8s", src)), flag, slate(oneLine(t.Spec.Description, termWidth()-36))))
	}
	out = append(out, fmt.Sprintf("  %s %s       %s", cyan(fmt.Sprintf("%-16s", "ask_user")), slate(fmt.Sprintf("%-8s", "builtin")), slate("pause and ask you a question whenever information is missing")))
	for _, err := range errs {
		out = append(out, "  "+amber(fmt.Sprintf("! %v", err)))
	}
	out = append(out, "  "+slate("script tools: "+strings.Join(agent.ToolDirs(), " · ")))
	r.println(strings.Join(out, "\n"))
}

func (r *REPL) printConfig() {
	cfg := r.app.Config.Get()
	name, pc := cfg.Active()
	verify := cfg.Loop.Verify
	if verify == "" {
		verify = "auto (nothing detected)"
		if d := agent.DetectVerify(workingDir()); d != "" {
			verify = "auto → " + d
		}
	}
	r.printf("  file:       %s\n  provider:   %s (%s @ %s)\n  model:      %s\n  mode:       %s\n  track:      %s\n  review:     %s\n  yolo:       %v (saved auto_approve: %v)\n  loop:       %d iterations · %d attempts/step · %d tool rounds/step · verify: %s\n  voice:      %v (%s)\n  skills dir: %s\n  memory:     %s · %s\n  permissions: %s",
		r.app.Config.Path(), name, pc.Type, pc.BaseURL, pc.Model, r.mode(), r.track(), reviewState(cfg.Review), r.yolo(), cfg.AutoApprove,
		cfg.Loop.MaxIterations, cfg.Loop.StepAttempts, cfg.Loop.StepRounds, verify,
		cfg.Audio.Enabled, cfg.Audio.Engine, r.app.Skills.InstallDir(),
		r.app.Memory.Dir("user"), r.app.Memory.Dir("project"), r.app.Perms.Path())
}

// modeCommand switches mode and, given a request, sends it in that mode.
func (r *REPL) modeCommand(m agent.Mode, request string) {
	if r.mode() != m {
		r.setMode(m)
		if m == agent.ModeLoop && r.sess.Plan != "" {
			r.printf("  %s", slate("/loop runs the plan you just made"))
		}
	}
	if request != "" {
		r.send(request)
	}
}

// domainCommand handles /domain and the /software, /docs, … shortcuts:
// [flavor] [request]. A first word that names a flavor picks it; the rest
// is sent as a request.
func (r *REPL) domainCommand(d agent.Domain, args []string, rest string) {
	t := agent.Track{Domain: d, Flavor: r.track().Flavor}
	if d != r.track().Domain {
		t.Flavor = agent.WorkflowFor(d).Flavors[0].Name
	}
	if len(args) > 0 {
		if ft, ok := t.WithFlavor(args[0]); ok {
			t = ft
			rest = strings.TrimSpace(strings.TrimPrefix(rest, args[0]))
		}
	}
	if t != r.track() {
		r.setTrack(t)
	}
	r.println(r.trackLine())
	if rest != "" {
		r.send(rest)
	}
}

// pickDomain is a two-step menu: domain, then flavor.
func (r *REPL) pickDomain() {
	cur := r.track()
	var items []menuItem
	sel := 0
	for i, w := range agent.Workflows {
		items = append(items, menuItem{Label: w.Label, Detail: w.Describe, Mark: w.Domain == cur.Domain, Value: string(w.Domain)})
		if w.Domain == cur.Domain {
			sel = i
		}
	}
	it := r.choose("Domain", items, sel)
	if it == nil {
		return
	}
	d, _ := agent.ParseDomain(it.Value)
	w := agent.WorkflowFor(d)
	flavor := w.Flavors[0].Name
	if len(w.Flavors) > 1 {
		items, sel = nil, 0
		for i, f := range w.Flavors {
			mark := d == cur.Domain && f.Name == cur.Flavor
			items = append(items, menuItem{Label: f.Name, Detail: f.Describe, Mark: mark, Value: f.Name})
			if mark {
				sel = i
			}
		}
		fi := r.choose(w.Label+" · flavor", items, sel)
		if fi == nil {
			return
		}
		flavor = fi.Value
	}
	r.domainCommand(d, []string{flavor}, flavor)
}

func (r *REPL) pickMode() {
	var items []menuItem
	sel := 0
	for i, m := range agent.Modes {
		items = append(items, menuItem{Label: string(m), Detail: m.Describe(), Mark: m == r.mode(), Value: string(m)})
		if m == r.mode() {
			sel = i
		}
	}
	if it := r.choose("Mode", items, sel); it != nil {
		m, _ := agent.ParseMode(it.Value)
		r.modeCommand(m, "")
	}
}

func (r *REPL) loopCommand(args []string, rest string) {
	cwd := workingDir()
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "":
		if r.sess.Plan != "" {
			steps := agent.ParseSteps(r.sess.Plan)
			r.printf("  %s", slate(fmt.Sprintf("running the plan from plan mode (%d steps)", len(steps))))
			r.loop(r.sess.PlanGoal, agent.LoopOptions{Steps: steps})
			r.sess.Plan, r.sess.PlanGoal = "", ""
			return
		}
		r.modeCommand(agent.ModeLoop, "")
		r.printf("  %s", slate(`type the objective, e.g. "add a --json flag to the list command, with tests"`))
	case "resume", "continue":
		r.loop("", agent.LoopOptions{Resume: true, Notes: strings.TrimSpace(strings.TrimPrefix(rest, args[0]))})
	case "status":
		st, err := agent.LoadLoop(cwd)
		if err != nil {
			r.printf("%s", slate(err.Error()))
			return
		}
		done, total := st.Progress()
		r.printf("  %s %s · %d/%d steps · %d iterations · updated %s\n  %s %s",
			badge(colAmber, "⟳ LOOP"), amber(string(st.Status)), done, total, st.Iteration, st.Updated.Format("Jan 2 15:04"), slate("objective:"), st.Objective)
		if st.Outcome != "" {
			r.printf("  %s", slate(st.Outcome))
		}
		r.printChecklist(st)
	case "verify":
		cmd := strings.TrimSpace(strings.TrimPrefix(rest, args[0]))
		if cmd == "" {
			r.printf("usage: /loop verify <command>  ·  /loop verify none  ·  /loop verify auto")
			return
		}
		if cmd == "auto" {
			cmd = ""
		}
		r.update(func(c *config.Config) error { c.Loop.Verify = cmd; return nil })
		if cmd == "" {
			cmd = "auto-detect (" + agent.DetectVerify(cwd) + ")"
		}
		r.printf("%s", slate("loop verification → "+cmd))
	default:
		r.loop(rest, agent.LoopOptions{})
	}
}

func (r *REPL) skillCommand(ctx context.Context, args []string) {
	if len(args) < 2 {
		r.printf("usage: /skill add <path|url|github:owner/repo[/path][@ref]>  ·  /skill rm <name>  ·  /skill show <name>")
		return
	}
	switch args[0] {
	case "add", "import", "install":
		sp := r.startSpinner("importing " + args[1])
		got, err := r.app.Skills.Import(ctx, args[1])
		sp.Stop()
		if err != nil {
			r.printf("%s", red(err.Error()))
			return
		}
		for _, s := range got {
			r.printf("%s %s  %s", green("✓"), magenta(s.Name), slate(s.Description))
		}
	case "rm", "remove", "delete":
		if err := r.app.Skills.Remove(args[1]); err != nil {
			r.printf("%s", red(err.Error()))
			return
		}
		r.printf("%s", slate("removed "+args[1]))
	case "show":
		sk := r.app.Skills.Get(args[1])
		if sk == nil {
			r.printf("%s", red(fmt.Sprintf("no skill %q", args[1])))
			return
		}
		r.printf("%s — %s\n%s", bold(sk.Name), sk.Description, slate(sk.Dir))
		r.println(gutterLines(renderMarkdown(sk.Body, termWidth()-4)))
	default:
		r.printf("unknown /skill subcommand %q", args[0])
	}
}

func (r *REPL) update(fn func(c *config.Config) error) {
	if err := r.app.Config.Update(fn); err != nil {
		r.printf("%s", red(err.Error()))
	}
}

func workingDir() string {
	cwd, _ := os.Getwd()
	return cwd
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
