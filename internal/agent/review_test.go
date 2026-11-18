package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

// reviewApp is testApp with self-review switched on.
func reviewApp(t *testing.T, baseURL string, passes int, domain, flavor string) *App {
	t.Helper()
	app := testApp(t, baseURL)
	err := app.Config.Update(func(c *config.Config) error {
		c.Review.Passes, c.Review.MaxPasses = passes, 0
		c.Domain, c.Flavor = domain, flavor
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func lastUser(r chatReq) string {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == "user" {
			return r.Messages[i].Content
		}
	}
	return ""
}

func TestSelfReviewRunsAtLeastFourPassesWithDifferentLenses(t *testing.T) {
	t.Chdir(t.TempDir())
	var lenses []string
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		u := lastUser(r)
		if strings.HasPrefix(u, "[Self-review pass") {
			if !strings.Contains(r.system(), "## Self-review") {
				t.Errorf("review pass without review rules in the system prompt")
			}
			lens, _, _ := strings.Cut(strings.SplitN(u, "Lens: ", 2)[1], "\n")
			lenses = append(lenses, lens)
			return reply{text: "REVIEW OK: checked it"}
		}
		return reply{text: "The answer is 42."}
	})
	app := reviewApp(t, srv.URL, 4, "pm", "plan")
	sess := app.NewSession()
	sess.Mode = ModeAsk
	got, err := sess.Send(context.Background(), llm.Message{Content: "what is the answer?"}, &gateEvents{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "The answer is 42." {
		t.Errorf("answer changed although every pass was ok: %q", got)
	}
	if len(lenses) != 4 {
		t.Fatalf("want exactly 4 review passes, got %d", len(lenses))
	}
	seen := map[string]bool{}
	for _, l := range lenses {
		seen[l] = true
	}
	if len(seen) != 4 || !strings.HasPrefix(lenses[0], "dependencies and order") {
		t.Errorf("each pass should use its own lens, PM lenses first: %q", lenses)
	}
	// The review exchange is folded out of the conversation.
	if len(sess.History) != 2 {
		t.Errorf("history should hold just the turn, got %d messages", len(sess.History))
	}
}

func TestSelfReviewFixesAndKeepsGoingUntilClean(t *testing.T) {
	t.Chdir(t.TempDir())
	passes := 0
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		if strings.HasPrefix(lastUser(r), "[Self-review pass") {
			passes++
			if passes == 4 { // the last required pass still finds a problem…
				return reply{text: "Found a mistake: the answer is 43."}
			}
			return reply{text: "**REVIEW OK** — nothing to fix"}
		}
		return reply{text: "The answer is 42."}
	})
	app := reviewApp(t, srv.URL, 4, "software", "project")
	sess := app.NewSession()
	sess.Mode = ModeAsk
	got, err := sess.Send(context.Background(), llm.Message{Content: "q"}, &gateEvents{})
	if err != nil {
		t.Fatal(err)
	}
	if passes != 5 { // …so one more pass runs to confirm the fix
		t.Errorf("want 5 passes (4 + 1 after a late fix), got %d", passes)
	}
	if !strings.Contains(got, "43") {
		t.Errorf("the revised answer should be returned: %q", got)
	}
	if last := sess.History[len(sess.History)-1]; !strings.Contains(last.Content, "43") {
		t.Errorf("history should keep the revised answer: %q", last.Content)
	}
}

func TestReviewOK(t *testing.T) {
	for in, want := range map[string]bool{
		"REVIEW OK: fine":                        true,
		"**Review OK** all good":                 true,
		"review passed":                          true,
		"> REVIEW OK":                            true,
		"I fixed the bug.":                       false,
		"The review ok line is below\nREVIEW OK": false,
	} {
		if got, _ := reviewOK(in); got != want {
			t.Errorf("reviewOK(%q) = %v, want %v", in, got, want)
		}
	}
}

type askEvents struct {
	gateEvents
	asked []string
}

func (a *askEvents) AskUser(_ context.Context, q string, choices []string) (string, error) {
	a.asked = append(a.asked, q+" "+strings.Join(choices, "/"))
	return "blue", nil
}

func TestAskUserPausesForTheUser(t *testing.T) {
	t.Chdir(t.TempDir())
	var final chatReq
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		if r.toolResults() == 0 {
			return reply{tool: "ask_user", args: map[string]any{"question": "Which colour?", "choices": []string{"red", "blue"}}}
		}
		final = r
		return reply{text: "Painting it blue."}
	})
	app := testApp(t, srv.URL)
	ev := &askEvents{}
	sess := app.NewSession()
	sess.Mode = ModePlan // read-only modes can still ask
	if _, err := sess.Send(context.Background(), llm.Message{Content: "paint it"}, ev); err != nil {
		t.Fatal(err)
	}
	if len(ev.asked) != 1 || ev.asked[0] != "Which colour? red/blue" {
		t.Fatalf("question not put to the user: %q", ev.asked)
	}
	last := final.Messages[len(final.Messages)-1]
	if last.Role != "tool" || !strings.Contains(last.Content, "The user answered: blue") {
		t.Errorf("answer not returned to the model: %+v", last)
	}

	// With no front end that can ask, the model is told to assume.
	res, err := askTool().Run(context.Background(), json.RawMessage(`{"question":"x?"}`))
	if err != nil || res != NoUserAnswer {
		t.Errorf("no asker: got %q, %v", res, err)
	}
}

func TestBuildKeepsWorkFolderAndWritesItWithoutAsking(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var system string
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		system = r.system()
		if r.toolResults() == 0 {
			// The model writes plan.md where the system prompt said.
			i := strings.Index(system, WorkRoot+"/")
			rel := strings.Fields(system[i:])[0]
			return reply{tool: "write_file", args: map[string]any{"path": strings.TrimSuffix(rel, "/") + "/plan.md", "content": "# Plan\n"}}
		}
		return reply{text: "Wrote the plan."}
	})
	app := testApp(t, srv.URL)
	app.Yolo = false
	ev := &gateEvents{answer: Deny}
	sess := app.NewSession()
	if _, err := sess.Send(context.Background(), llm.Message{Content: "Add a login page"}, ev); err != nil {
		t.Fatal(err)
	}
	if len(ev.asked) != 0 {
		t.Errorf("writing the work folder should not ask: %+v", ev.asked)
	}
	w := sess.Work
	if w == nil || !strings.Contains(w.Rel, "add-a-login-page") {
		t.Fatalf("work folder not created from the request: %+v", w)
	}
	if !strings.Contains(system, "checklist.md") || !strings.Contains(system, "walkthrough.md") {
		t.Errorf("system prompt should name the working files")
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "plan.md")); err != nil {
		t.Errorf("plan.md not written: %v", err)
	}
	log, _ := os.ReadFile(filepath.Join(w.Dir, "activity.md"))
	if !strings.Contains(string(log), "`write_file`") {
		t.Errorf("activity.md should log the tool call:\n%s", log)
	}
	// Writing outside the work folder still asks.
	if w.containsCall(llm.ToolCall{Name: "write_file", Args: json.RawMessage(`{"path":"main.go"}`)}) {
		t.Error("main.go must not count as inside the work folder")
	}
	if w.containsCall(llm.ToolCall{Name: "write_file", Args: json.RawMessage(`{"path":"` + filepath.ToSlash(filepath.Join(w.Rel, "..", "..", "x")) + `"}`)}) {
		t.Error("paths escaping the work folder must not count as inside it")
	}
}

func TestToolImagesAreShownToTheModel(t *testing.T) {
	t.Chdir(t.TempDir())
	var withImage chatReq
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		if r.toolResults() == 0 {
			return reply{tool: "look", args: map[string]any{}}
		}
		withImage = r
		return reply{text: "I see a red square."}
	})
	app := testApp(t, srv.URL)
	app.Tools.Add(&tools.Tool{
		Spec: llm.ToolSpec{Name: "look", Parameters: map[string]any{"type": "object"}},
		RunMedia: func(context.Context, json.RawMessage) (string, []llm.Image, error) {
			return "took a picture", []llm.Image{{MediaType: "image/png", Data: []byte("png")}}, nil
		},
	})
	sess := app.NewSession()
	sess.Mode = ModeAsk
	if _, err := sess.Send(context.Background(), llm.Message{Content: "look"}, &gateEvents{}); err != nil {
		t.Fatal(err)
	}
	msgs := withImage.Messages
	last := msgs[len(msgs)-1]
	if last.Role != "user" || len(last.Images) != 1 || msgs[len(msgs)-2].Role != "tool" {
		t.Errorf("image should follow the tool result as a user message, got %+v", last)
	}
}

func TestLoopFollowsTrackWithFinaleReviewAndWorkFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var planReviews, finalReviews int
	var finaleSeen, rulesInReviewer bool
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		sys, u := r.system(), lastUser(r)
		switch {
		case strings.HasPrefix(u, "[Plan review pass"):
			planReviews++
			return reply{text: "REVIEW OK: plan is fine"}
		case strings.Contains(sys, "Loop harness: planner"):
			if !strings.Contains(sys, "Caller first") || !strings.Contains(u, "software/work loop") {
				t.Errorf("planner should get the work loop phases")
			}
			return reply{text: "1. Write note.txt"}
		case strings.HasPrefix(sys, "You check whether"):
			rulesInReviewer = rulesInReviewer || strings.Contains(sys, "anticipation code")
			return reply{text: "PASS: done"}
		case strings.Contains(u, "[Self-review pass"):
			finalReviews++
			return reply{text: "REVIEW OK: files match"}
		case strings.Contains(u, "CURRENT STEP"):
			if strings.Contains(u, "Security test") {
				finaleSeen = true
			}
			if r.toolResults() == 0 {
				return reply{tool: "write_file", args: map[string]any{"path": filepath.Join(dir, "note.txt"), "content": "ok"}}
			}
			return reply{text: "Done."}
		}
		t.Errorf("unexpected request: %.100q", u)
		return reply{text: "?"}
	})
	app := reviewApp(t, srv.URL, 4, "software", "work")
	sess := app.NewSession()
	st, err := sess.RunLoop(context.Background(), "write a note", LoopOptions{Verify: catCmd("note.txt")}, &recEvents{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != LoopDone {
		t.Fatalf("status %s: %s", st.Status, st.Outcome)
	}
	if planReviews != 4 || finalReviews != 4 {
		t.Errorf("want 4 plan and 4 final review passes, got %d and %d", planReviews, finalReviews)
	}
	if !finaleSeen || len(st.Steps) != 2 || !st.FinaleAdded {
		t.Errorf("the security finale step should run after the plan passes: steps=%d", len(st.Steps))
	}
	if !rulesInReviewer {
		t.Error("the step reviewer should enforce the work rules")
	}
	for _, f := range []string{"plan.md", "checklist.md", "walkthrough.md", "activity.md"} {
		if _, err := os.Stat(filepath.Join(st.WorkDir, f)); err != nil {
			t.Errorf("%s missing from the work folder: %v", f, err)
		}
	}
	cl, _ := os.ReadFile(filepath.Join(st.WorkDir, "checklist.md"))
	if strings.Count(string(cl), "- [x]") != 2 || strings.Contains(string(cl), "- [ ]") {
		t.Errorf("checklist should have both steps ticked:\n%s", cl)
	}
	wt, _ := os.ReadFile(filepath.Join(st.WorkDir, "walkthrough.md"))
	if !strings.Contains(string(wt), "Final review") {
		t.Errorf("walkthrough should include the final review:\n%s", wt)
	}
}
