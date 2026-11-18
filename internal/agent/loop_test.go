package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/memory"
	"github.com/kxngHADES/asg-harness/internal/perms"
	"github.com/kxngHADES/asg-harness/internal/skills"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

// chatReq is what the fake Ollama server sees.
type chatReq struct {
	Messages []struct {
		Role      string   `json:"role"`
		Content   string   `json:"content"`
		ToolCalls []any    `json:"tool_calls"`
		ToolName  string   `json:"tool_name"`
		Images    []string `json:"images"`
	} `json:"messages"`
	Tools   []any          `json:"tools"`
	Options map[string]any `json:"options"`
}

func (c chatReq) system() string {
	if len(c.Messages) > 0 && c.Messages[0].Role == "system" {
		return c.Messages[0].Content
	}
	return ""
}

func (c chatReq) firstUser() string {
	for _, m := range c.Messages {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
}

// toolResults counts tool messages in the conversation so far.
func (c chatReq) toolResults() int {
	n := 0
	for _, m := range c.Messages {
		if m.Role == "tool" {
			n++
		}
	}
	return n
}

type reply struct {
	text string
	tool string
	args map[string]any
}

// scriptedOllama serves /api/chat, answering each request with script(req).
func scriptedOllama(t *testing.T, script func(chatReq) reply) (*httptest.Server, *[]chatReq) {
	var mu sync.Mutex
	var seen []chatReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request: %v", err)
		}
		mu.Lock()
		seen = append(seen, req)
		mu.Unlock()
		rep := script(req)
		msg := map[string]any{"role": "assistant", "content": rep.text}
		if rep.tool != "" {
			msg["tool_calls"] = []any{map[string]any{"function": map[string]any{"name": rep.tool, "arguments": rep.args}}}
		}
		enc := json.NewEncoder(w)
		_ = enc.Encode(map[string]any{"message": msg, "done": false})
		_ = enc.Encode(map[string]any{"message": map[string]any{"role": "assistant"}, "done": true, "prompt_eval_count": 10, "eval_count": 5})
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func testApp(t *testing.T, baseURL string) *App {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ASG_HOME", home)
	store, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	err = store.Update(func(c *config.Config) error {
		c.Providers = map[string]*config.ProviderConfig{"ollama": {Type: "ollama", BaseURL: baseURL, Model: "qwen3.5:9b"}}
		c.ActiveProvider = "ollama"
		// These tests cover the loop mechanics; self-review and the track
		// finale have their own tests (review_test.go).
		c.Review.Passes = -1
		c.Domain, c.Flavor = "software", "project"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return &App{Config: store, Tools: tools.Builtins(), Skills: skills.NewManager(home), Perms: perms.Load(home), Memory: memory.New(home), Yolo: true}
}

type recEvents struct {
	gateEvents
	mu      sync.Mutex
	updates []string
}

func (r *recEvents) LoopUpdate(_ *LoopState, msg string) {
	r.mu.Lock()
	r.updates = append(r.updates, msg)
	r.mu.Unlock()
}

func catCmd(file string) string {
	if runtime.GOOS == "windows" {
		return "type " + file
	}
	return "cat " + file
}

func TestLoopPlansWorksReviewsAndVerifies(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	target := filepath.Join(dir, "hello.txt")
	lazyOnce := true

	srv, seen := scriptedOllama(t, func(r chatReq) reply {
		sys, user := r.system(), r.firstUser()
		switch {
		case strings.Contains(sys, "Loop harness: planner"):
			return reply{text: "Here is the plan:\n1. Create **hello.txt** containing hi\n2. Read hello.txt back to confirm it\n\nNo risks."}
		case strings.HasPrefix(sys, "You check whether"):
			if strings.Contains(user, "(none") {
				return reply{text: "FAIL: nothing was read back; call read_file on hello.txt"}
			}
			return reply{text: "PASS: the tool results show it"}
		case strings.Contains(user, "CURRENT STEP (1 of 2)"):
			if r.toolResults() == 0 {
				return reply{tool: "write_file", args: map[string]any{"path": target, "content": "hi"}}
			}
			return reply{text: "Created hello.txt."}
		case strings.Contains(user, "CURRENT STEP (2 of 2)"):
			if lazyOnce { // first attempt: claims success without checking
				lazyOnce = false
				return reply{text: "It is fine, trust me."}
			}
			if !strings.Contains(user, "PREVIOUS ATTEMPT AT THIS STEP WAS REJECTED") {
				t.Errorf("retry prompt does not carry the reviewer's feedback:\n%s", user)
			}
			if r.toolResults() == 0 {
				return reply{tool: "read_file", args: map[string]any{"path": target}}
			}
			return reply{text: "hello.txt contains hi."}
		}
		t.Errorf("unexpected request: system=%.80q user=%.120q", sys, user)
		return reply{text: "?"}
	})

	app := testApp(t, srv.URL)
	sess := app.NewSession()
	ev := &recEvents{}
	st, err := sess.RunLoop(context.Background(), "make hello.txt say hi", LoopOptions{Verify: catCmd("hello.txt")}, ev)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != LoopDone {
		t.Fatalf("status %s (%s), updates: %v", st.Status, st.Outcome, ev.updates)
	}
	if got, _ := os.ReadFile(target); string(got) != "hi" {
		t.Errorf("hello.txt = %q", got)
	}
	if len(st.Steps) != 2 || st.Steps[0].Title != "Create hello.txt containing hi" {
		t.Errorf("steps parsed wrong: %+v", st.Steps)
	}
	if st.Steps[1].Attempts != 2 {
		t.Errorf("step 2 should have needed a retry, attempts=%d", st.Steps[1].Attempts)
	}
	if !strings.Contains(st.Steps[0].Notes, "hello.txt") {
		t.Errorf("step notes should name the file: %q", st.Steps[0].Notes)
	}
	if !strings.Contains(st.Outcome, "passes") {
		t.Errorf("outcome should report verification: %q", st.Outcome)
	}
	// Every request asks Ollama for a usable context window.
	for _, r := range *seen {
		if r.Options["num_ctx"] != float64(config.DefaultNumCtx) {
			t.Fatalf("num_ctx not sent: %v", r.Options)
		}
	}
	// The run is saved and summarised into the conversation.
	if saved, err := LoadLoop(dir); err != nil || saved.Status != LoopDone {
		t.Errorf("saved state: %+v, %v", saved, err)
	}
	if n := len(sess.History); n != 2 || !strings.Contains(sess.History[1].Content, "Loop done") {
		t.Errorf("summary not added to history: %+v", sess.History)
	}
}

func TestLoopAddsFixStepWhenVerifyFails(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	target := filepath.Join(dir, "out.txt")
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		sys, user := r.system(), r.firstUser()
		switch {
		case strings.HasPrefix(sys, "You check whether"):
			return reply{text: "PASS"}
		case strings.Contains(user, "Fix the failures"):
			if !strings.Contains(user, "currently fails with") {
				t.Errorf("fix step lacks the failure output:\n%s", user)
			}
			if r.toolResults() == 0 {
				return reply{tool: "write_file", args: map[string]any{"path": target, "content": "ok"}}
			}
			return reply{text: "wrote out.txt"}
		}
		return reply{text: "did the thing"} // the single seeded step "changes" nothing
	})
	app := testApp(t, srv.URL)
	st, err := app.NewSession().RunLoop(context.Background(), "produce out.txt", LoopOptions{Steps: []string{"write out.txt"}, Verify: catCmd("out.txt")}, &recEvents{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != LoopDone || st.Fixes != 1 || len(st.Steps) != 2 {
		t.Fatalf("want done after one fix step, got %s fixes=%d steps=%d (%s)", st.Status, st.Fixes, len(st.Steps), st.Outcome)
	}
}

func TestLoopBlocksAndResumesWithUserAnswer(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		sys, user := r.system(), r.firstUser()
		switch {
		case strings.HasPrefix(sys, "You check whether"):
			return reply{text: "PASS: ok"}
		case strings.Contains(user, "NOTES FROM THE USER"):
			return reply{text: "Used port 8080 as told."}
		}
		return reply{text: "I looked around.\n**BLOCKED:** which port should the server use?"}
	})
	app := testApp(t, srv.URL)
	opts := LoopOptions{Steps: []string{"configure the server port"}, Verify: "none"}
	st, err := app.NewSession().RunLoop(context.Background(), "set up server", opts, &recEvents{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != LoopBlocked || !strings.Contains(st.Outcome, "which port") {
		t.Fatalf("want blocked with the question, got %s: %s", st.Status, st.Outcome)
	}
	st, err = app.NewSession().RunLoop(context.Background(), "", LoopOptions{Resume: true, Notes: "use 8080"}, &recEvents{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != LoopDone {
		t.Fatalf("resume should finish, got %s: %s", st.Status, st.Outcome)
	}
}

func TestLoopSplitsAStepThatKeepsFailing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		sys, user := r.system(), r.firstUser()
		switch {
		case strings.HasPrefix(sys, "You check whether"):
			if strings.Contains(user, "STEP TO CHECK: big step") {
				return reply{text: "FAIL: too much at once"}
			}
			return reply{text: "PASS"}
		case strings.Contains(user, "has failed"):
			return reply{text: "1. small part a\n2. small part b"}
		}
		return reply{text: "done"}
	})
	app := testApp(t, srv.URL)
	st, err := app.NewSession().RunLoop(context.Background(), "obj", LoopOptions{Steps: []string{"big step"}, Verify: "none"}, &recEvents{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != LoopDone || len(st.Steps) != 2 || st.Steps[0].Title != "small part a" || st.Steps[0].Depth != 1 {
		t.Fatalf("want the step split into two done sub-steps, got %s %+v", st.Status, st.Steps)
	}
}

func TestRepeatedToolCallIsStopped(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	f := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	var lastTool string
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		if n := r.toolResults(); n > 0 {
			lastTool = r.Messages[len(r.Messages)-1].Content
			if n >= 4 {
				return reply{text: "giving up"}
			}
		}
		return reply{tool: "read_file", args: map[string]any{"path": f}}
	})
	app := testApp(t, srv.URL)
	got, err := app.NewSession().Send(context.Background(), llm.Message{Content: "read a.txt"}, &gateEvents{})
	if err != nil || got != "giving up" {
		t.Fatalf("got %q, %v", got, err)
	}
	if !strings.Contains(lastTool, "Not run") {
		t.Errorf("4th identical call should be refused, got %q", lastTool)
	}
}

func TestPlanModeHidesWriteToolsAndSavesPlan(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var offered []string
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		for _, tl := range r.Tools {
			b, _ := json.Marshal(tl)
			var spec struct {
				Function struct{ Name string } `json:"function"`
			}
			_ = json.Unmarshal(b, &spec)
			offered = append(offered, spec.Function.Name)
		}
		if !strings.Contains(r.system(), "Mode: PLAN") {
			t.Errorf("plan instructions missing from system prompt")
		}
		return reply{text: "1. Add flag in main.go\n2. Add test"}
	})
	app := testApp(t, srv.URL)
	s := app.NewSession()
	s.Mode = ModePlan
	if _, err := s.Send(context.Background(), llm.Message{Content: "add a flag"}, &gateEvents{}); err != nil {
		t.Fatal(err)
	}
	for _, n := range offered {
		if n == "write_file" || n == "edit_file" || n == "bash" {
			t.Errorf("plan mode offered %s", n)
		}
	}
	if !contains(offered, "read_file") || !contains(offered, "grep") {
		t.Errorf("plan mode should keep read tools: %v", offered)
	}
	if s.PlanGoal != "add a flag" || len(ParseSteps(s.Plan)) != 2 {
		t.Errorf("plan not saved: %q / %q", s.PlanGoal, s.Plan)
	}
	if !strings.Contains(s.systemPrompt(ModeBuild, ""), "Current plan") {
		t.Errorf("build mode should carry the plan")
	}
}

func TestRecoveredTextToolCallRuns(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	f := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(f, []byte("secret-42"), 0o644)
	srv, _ := scriptedOllama(t, func(r chatReq) reply {
		if r.toolResults() == 0 {
			args, _ := json.Marshal(map[string]string{"path": f})
			return reply{text: fmt.Sprintf("<tool_call>\n{\"name\": \"read_file\", \"arguments\": %s}\n</tool_call>", args)}
		}
		return reply{text: "read: " + r.Messages[len(r.Messages)-1].Content}
	})
	app := testApp(t, srv.URL)
	got, err := app.NewSession().Send(context.Background(), llm.Message{Content: "read b"}, &gateEvents{})
	if err != nil || !strings.Contains(got, "secret-42") {
		t.Fatalf("text tool call was not executed: %q, %v", got, err)
	}
}

func osWrite(dir, name, body string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
}
