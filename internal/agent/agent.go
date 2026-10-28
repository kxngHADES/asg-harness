// Package agent runs the model ↔ tool loop. An App holds shared state
// (config, tools, skills); each Session is one conversation.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kxngHADES/asg-harness/internal/audio"
	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/skills"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

type App struct {
	Config  *config.Store
	Tools   *tools.Registry
	Skills  *skills.Manager
	Speaker *audio.Speaker
	// Yolo auto-approves tool calls for this process only (the -yolo flag).
	Yolo bool

	capMu sync.Mutex
	caps  map[string]llm.ModelInfo // Ollama /api/show results by base URL + model
}

func NewApp(cfg *config.Store) *App {
	return &App{
		Config:  cfg,
		Tools:   tools.Builtins(),
		Skills:  skills.NewManager(config.Dir()),
		Speaker: &audio.Speaker{},
	}
}

// Provider returns the active provider and its model.
func (a *App) Provider() (llm.Provider, string, error) {
	cfg := a.Config.Get()
	name, pc := cfg.Active()
	if pc == nil {
		return nil, "", fmt.Errorf("active provider %q is not configured", name)
	}
	p, err := llm.New(pc)
	return p, pc.Model, err
}

type Decision int

const (
	Deny Decision = iota
	Allow
	AlwaysAllow
)

// Events receives progress from a running turn.
type Events interface {
	Text(delta string)
	ToolStart(call llm.ToolCall, summary string)
	ToolEnd(call llm.ToolCall, result string, isErr bool)
	Approve(ctx context.Context, call llm.ToolCall, summary string) Decision
	Notice(msg string)
}

type Session struct {
	app     *App
	mu      sync.Mutex
	History []llm.Message
	allowed map[string]bool
	Usage   llm.Usage
	noTools bool // set when the model rejects tool definitions
}

func (a *App) NewSession() *Session {
	return &Session{app: a, allowed: map[string]bool{}}
}

func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = nil
	s.Usage = llm.Usage{}
	s.noTools = false
}

func (s *Session) Allowed() []string {
	var out []string
	for k := range s.allowed {
		out = append(out, k)
	}
	return out
}

// SystemPrompt builds the system prompt for the current directory and skills.
func (s *Session) SystemPrompt() string {
	cfg := s.app.Config.Get()
	cwd, _ := os.Getwd()
	var b strings.Builder
	b.WriteString(`You are ASG, an agentic assistant running in the user's terminal. You help with software engineering and general tasks.
You can call tools to read and edit files, run shell commands, search, and fetch web pages. Prefer acting with tools over guessing.
Keep answers concise and use Markdown. When you change files, say what you changed. Never invent tool results.`)
	fmt.Fprintf(&b, "\n\nEnvironment:\n- Working directory: %s\n- OS: %s/%s\n- Date: %s\n", cwd, runtime.GOOS, runtime.GOARCH, time.Now().Format("2006-01-02"))

	for _, name := range []string{"ASG.md", "AGENTS.md", "CLAUDE.md"} {
		if data, err := os.ReadFile(filepath.Join(cwd, name)); err == nil {
			fmt.Fprintf(&b, "\nProject instructions from %s:\n%s\n", name, strings.TrimSpace(string(data)))
			break
		}
	}
	if list := s.app.Skills.List(); len(list) > 0 {
		b.WriteString("\nAvailable skills (call the use_skill tool to load full instructions before doing a task one of them covers):\n")
		for _, sk := range list {
			fmt.Fprintf(&b, "- %s: %s\n", sk.Name, sk.Description)
		}
	}
	if cfg.SystemPrompt != "" {
		b.WriteString("\n" + cfg.SystemPrompt + "\n")
	}
	return b.String()
}

func (s *Session) skillTool() *tools.Tool {
	mgr := s.app.Skills
	return &tools.Tool{
		Spec: llm.ToolSpec{
			Name:        "use_skill",
			Description: "Load the full instructions of an installed skill by name.",
			Parameters: map[string]any{"type": "object", "required": []string{"name"},
				"properties": map[string]any{"name": map[string]any{"type": "string", "description": "Skill name"}}},
		},
		Describe: func(a json.RawMessage) string {
			var v struct{ Name string }
			_ = json.Unmarshal(a, &v)
			return v.Name
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var v struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(raw, &v)
			sk := mgr.Get(v.Name)
			if sk == nil {
				return "", fmt.Errorf("no skill named %q", v.Name)
			}
			return SkillPrompt(sk), nil
		},
	}
}

// SkillPrompt renders a skill's instructions for the model.
func SkillPrompt(sk *skills.Skill) string {
	return fmt.Sprintf("# Skill: %s\n(Skill files live in %s — read any referenced files from there.)\n\n%s", sk.Name, sk.Dir, sk.Body)
}

func (s *Session) toolset() []*tools.Tool {
	all := s.app.Tools.All()
	if len(s.app.Skills.List()) > 0 {
		all = append(all, s.skillTool())
	}
	return all
}

// Send runs one user turn to completion, executing tool calls as needed.
func (s *Session) Send(ctx context.Context, user llm.Message, ev Events) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg := s.app.Config.Get()
	provider, model, err := s.app.Provider()
	if err != nil {
		return "", err
	}
	user.Role = llm.RoleUser
	s.History = append(s.History, user)

	toolset := s.toolset()
	byName := map[string]*tools.Tool{}
	var specs []llm.ToolSpec
	for _, t := range toolset {
		byName[t.Spec.Name] = t
		specs = append(specs, t.Spec)
	}
	system := s.SystemPrompt()

	for round := 0; round < cfg.MaxToolRounds; round++ {
		req := llm.Request{Model: model, System: system, Messages: s.History, Temperature: cfg.Temperature}
		if !s.noTools {
			req.Tools = specs
		}
		resp, err := provider.Chat(ctx, req, ev.Text)
		if err != nil && !s.noTools && strings.Contains(err.Error(), "does not support tools") {
			s.noTools = true
			ev.Notice(fmt.Sprintf("%s does not support tool calling — continuing in chat-only mode", model))
			req.Tools = nil
			resp, err = provider.Chat(ctx, req, ev.Text)
		}
		if err != nil {
			if ctx.Err() != nil {
				s.History = append(s.History, llm.Message{Role: llm.RoleAssistant, Content: "[interrupted by user]"})
				return "", ctx.Err()
			}
			// Drop the dangling user message so the conversation stays valid.
			if round == 0 {
				s.History = s.History[:len(s.History)-1]
			}
			return "", err
		}
		s.Usage.InputTokens = resp.Usage.InputTokens
		s.Usage.OutputTokens += resp.Usage.OutputTokens
		msg := resp.Message
		s.History = append(s.History, msg)
		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}

		for i, call := range msg.ToolCalls {
			result := s.runTool(ctx, call, byName, cfg.AutoApprove || s.app.Yolo, ev)
			s.History = append(s.History, llm.Message{Role: llm.RoleTool, Content: result, ToolCallID: call.ID, ToolName: call.Name})
			if ctx.Err() != nil {
				// Answer the remaining calls so the history stays well-formed.
				for _, rest := range msg.ToolCalls[i+1:] {
					s.History = append(s.History, llm.Message{Role: llm.RoleTool, Content: "cancelled", ToolCallID: rest.ID, ToolName: rest.Name})
				}
				return "", ctx.Err()
			}
		}
	}
	ev.Notice(fmt.Sprintf("stopped after %d tool rounds (raise max_tool_rounds in config)", cfg.MaxToolRounds))
	return "", nil
}

func (s *Session) runTool(ctx context.Context, call llm.ToolCall, byName map[string]*tools.Tool, auto bool, ev Events) string {
	t := byName[call.Name]
	if t == nil {
		msg := fmt.Sprintf("error: unknown tool %q", call.Name)
		ev.ToolEnd(call, msg, true)
		return msg
	}
	summary := ""
	if t.Describe != nil {
		summary = t.Describe(call.Args)
	}
	if t.NeedsApproval && !auto && !s.allowed[call.Name] {
		switch ev.Approve(ctx, call, summary) {
		case Deny:
			msg := "The user denied this tool call. Ask them how to proceed instead of retrying."
			ev.ToolEnd(call, "denied", true)
			return msg
		case AlwaysAllow:
			s.allowed[call.Name] = true
		}
	}
	ev.ToolStart(call, summary)
	out, err := t.Run(ctx, call.Args)
	if err != nil {
		msg := "error: " + err.Error()
		ev.ToolEnd(call, msg, true)
		return msg
	}
	ev.ToolEnd(call, out, false)
	return out
}
