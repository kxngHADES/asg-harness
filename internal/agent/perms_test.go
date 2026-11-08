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
