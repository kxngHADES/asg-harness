package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/memory"
	"github.com/kxngHADES/asg-harness/internal/perms"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

type gateEvents struct {
	answer Decision
	asked  []Gate
}

func (g *gateEvents) Text(string)                        {}
func (g *gateEvents) ToolStart(llm.ToolCall, string)     {}
func (g *gateEvents) ToolEnd(llm.ToolCall, string, bool) {}
func (g *gateEvents) Notice(string)                      {}
func (g *gateEvents) Approve(_ context.Context, _ llm.ToolCall, gate Gate) Decision {
	g.asked = append(g.asked, gate)
	return g.answer
}

func TestAlwaysAllowSurvivesSessions(t *testing.T) {
	home := t.TempDir()
	ran := 0
	shell := &tools.Tool{
		Spec:          llm.ToolSpec{Name: "bash"},
		NeedsApproval: true,
		Run:           func(context.Context, json.RawMessage) (string, error) { ran++; return "ok", nil },
	}
	byName := map[string]*tools.Tool{"bash": shell}
	call := func(cmd string) llm.ToolCall {
		args, _ := json.Marshal(map[string]string{"command": cmd})
		return llm.ToolCall{ID: "1", Name: "bash", Args: args}
	}
	newSession := func() *Session {
		app := &App{Perms: perms.Load(home), Memory: memory.New(home)}
		return app.NewSession()
	}

	ev := &gateEvents{answer: AlwaysAllow}
	newSession().runTool(context.Background(), call("git status"), byName, false, ev)
	if len(ev.asked) != 1 || ev.asked[0].Scope != "git" {
		t.Fatalf("first git call should ask with scope git, got %+v", ev.asked)
	}

	// A fresh session (as after restarting asg) must not ask again for git…
	ev2 := &gateEvents{answer: Deny}
	s := newSession()
	s.runTool(context.Background(), call("git log --oneline"), byName, false, ev2)
	if len(ev2.asked) != 0 {
		t.Errorf("git was asked again in a new session: %+v", ev2.asked)
	}
	// …but still asks for other programs.
	s.runTool(context.Background(), call("git status && rm -rf build"), byName, false, ev2)
	if len(ev2.asked) != 1 {
		t.Errorf("rm should still need approval")
	}
	if ran != 2 {
		t.Errorf("tool ran %d times, want 2", ran)
	}
}

func TestPerCallApproval(t *testing.T) {
	home := t.TempDir()
	level := map[string]tools.Approval{"look": tools.ApprovalNone, "click": tools.ApprovalAsk, "delete": tools.ApprovalAlways}
	ran := 0
	tool := &tools.Tool{
		Spec:          llm.ToolSpec{Name: "computer"},
		NeedsApproval: true,
		Approval:      func(a json.RawMessage) tools.Approval { return level[fieldOf(a, "action")] },
		Rules:         func(a json.RawMessage) []string { return []string{"computer:" + fieldOf(a, "action")} },
		Run:           func(context.Context, json.RawMessage) (string, error) { ran++; return "ok", nil },
	}
	byName := map[string]*tools.Tool{"computer": tool}
	call := func(action string) llm.ToolCall {
		return llm.ToolCall{ID: "1", Name: "computer", Args: json.RawMessage(`{"action":"` + action + `"}`)}
	}
	app := &App{Perms: perms.Load(home), Memory: memory.New(home)}
	s := app.NewSession()

	ev := &gateEvents{answer: AlwaysAllow}
	s.runTool(context.Background(), call("look"), byName, false, ev)
	if len(ev.asked) != 0 {
		t.Errorf("a look-only call asked: %+v", ev.asked)
	}
	s.runTool(context.Background(), call("click"), byName, false, ev)
	s.runTool(context.Background(), call("click"), byName, false, ev)
	if len(ev.asked) != 1 || ev.asked[0].Scope != "computer:click" {
		t.Errorf("click should ask once and remember its own rule, got %+v", ev.asked)
	}

	// Sensitive calls ask even in YOLO mode and are never remembered.
	ev2 := &gateEvents{answer: AlwaysAllowGlobal}
	s.runTool(context.Background(), call("delete"), byName, true, ev2)
	s.runTool(context.Background(), call("delete"), byName, true, ev2)
	if len(ev2.asked) != 2 || ev2.asked[0].Scope != "" {
		t.Errorf("sensitive calls must ask every time without an always option, got %+v", ev2.asked)
	}
	if g, _ := app.Perms.List(home); len(g) != 0 {
		t.Errorf("a sensitive answer was remembered globally: %v", g)
	}
	if ran != 5 {
		t.Errorf("ran %d times, want 5", ran)
	}
}

func TestPruneToolImages(t *testing.T) {
	img := []llm.Image{{MediaType: "image/png", Data: []byte{1}}}
	var h []llm.Message
	for i := 0; i < 5; i++ {
		h = append(h, llm.Message{Role: llm.RoleUser, Content: "[1 image(s) returned by the tool calls above — look at them]", Images: img})
	}
	h = append([]llm.Message{{Role: llm.RoleUser, Content: "look at my photo", Images: img}}, h...)
	pruneToolImages(h, 3)
	if len(h[0].Images) != 1 {
		t.Error("the user's own image was dropped")
	}
	for i, m := range h[1:] {
		if want := i >= 2; (len(m.Images) == 1) != want {
			t.Errorf("message %d kept images = %v, want %v", i, len(m.Images) == 1, want)
		}
	}
}
