// Package agent runs the model ↔ tool loop. An App holds shared state
// (config, tools, skills); each Session is one conversation.
package agent

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/kxngHADES/asg-harness/internal/memory"
	"github.com/kxngHADES/asg-harness/internal/perms"
	"github.com/kxngHADES/asg-harness/internal/skills"
	"github.com/kxngHADES/asg-harness/internal/tools"
	bundled "github.com/kxngHADES/asg-harness/tools"
)

type App struct {
	Config  *config.Store
	Tools   *tools.Registry
	Skills  *skills.Manager
	Speaker *audio.Speaker
	// Perms remembers "always allow" answers across sessions.
	Perms *perms.Store
	// Memory holds notes the agent saves for future sessions.
	Memory *memory.Manager
	// Yolo auto-approves tool calls for this process only (the -yolo flag).
	Yolo bool

	capMu sync.Mutex
	caps  map[string]llm.ModelInfo // Ollama /api/show results by base URL + model
}

func NewApp(cfg *config.Store) *App {
	a := &App{
		Config:  cfg,
		Tools:   tools.Builtins(),
		Skills:  skills.NewManager(config.Dir()),
		Speaker: &audio.Speaker{},
		Perms:   perms.Load(config.Dir()),
		Memory:  memory.New(config.Dir()),
	}
	for _, t := range bundled.Bundled() {
		a.Tools.Add(t)
	}
	a.ReloadTools()
	return a
}

// ToolDirs are the folders searched for script tools, project first.
func ToolDirs() []string {
	return []string{filepath.Join(".asg", "tools"), filepath.Join(config.Dir(), "tools")}
}

// ReloadTools (re)loads script tools from ToolDirs. They never replace a
// built-in or bundled tool. It returns the manifests that failed to load.
func (a *App) ReloadTools() []error {
	for _, t := range a.Tools.All() {
		if t.Source != "builtin" && t.Source != "bundled" {
			a.Tools.Remove(t.Spec.Name)
		}
	}
	scripts, errs := tools.LoadScriptTools(ToolDirs()...)
	for _, t := range scripts {
		if a.Tools.Get(t.Spec.Name) == nil {
			a.Tools.Add(t)
		}
	}
	return errs
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
	AlwaysAllow       // remember for this project
	AlwaysAllowGlobal // remember everywhere
)

// Gate describes a tool call waiting for permission.
type Gate struct {
	Summary string
	// Scope is what an "always" answer covers, e.g. "git" for any git
	// command. Empty means the call can only be approved once.
	Scope string
}

// Events receives progress from a running turn.
type Events interface {
	Text(delta string)
	ToolStart(call llm.ToolCall, summary string)
	ToolEnd(call llm.ToolCall, result string, isErr bool)
	Approve(ctx context.Context, call llm.ToolCall, gate Gate) Decision
	Notice(msg string)
}

type Session struct {
	app     *App
	mu      sync.Mutex
	History []llm.Message
	allowed map[string]bool
	Usage   llm.Usage
	noTools bool // set when the model rejects tool definitions

	// Mode decides which tools the model gets and how it is instructed.
	Mode Mode
	// Plan is the latest plan written in plan mode, and PlanGoal the request
	// that produced it. Build mode keeps the plan in view; /loop can run it.
	Plan, PlanGoal string

	// Track is the domain and sub-mode (software/work, cyber/red, …).
	Track Track
	// Work is the folder holding plan.md, checklist.md, walkthrough.md and
	// activity.md for the current build; created on the first build turn.
	Work *Workspace
	// pendingImages are images returned by tools this round, shown to the
	// model after the tool results.
	pendingImages []llm.Image
}

func (a *App) NewSession() *Session {
	t := Track{Domain: DomainSoftware}
	if a.Config != nil {
		cfg := a.Config.Get()
		if d, ok := ParseDomain(cfg.Domain); ok {
			t.Domain = d
		}
		t, _ = t.WithFlavor(cfg.Flavor)
	}
	if t.Flavor == "" {
		t.Flavor = t.FlavorSpec().Name
	}
	return &Session{app: a, allowed: map[string]bool{}, Track: t}
}

// EnsureWork returns the session's work folder, creating it for request
// if there is none yet.
func (s *Session) EnsureWork(request string) *Workspace {
	if s.Work == nil {
		cwd, _ := os.Getwd()
		if w, err := newWorkspace(cwd, request); err == nil {
			s.Work = w
		}
	}
	return s.Work
}

func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = nil
	s.Usage = llm.Usage{}
	s.noTools = false
	s.Plan, s.PlanGoal = "", ""
	s.Work = nil
}

// SystemPrompt builds the system prompt for the current directory, skills
// and mode.
func (s *Session) SystemPrompt() string { return s.systemPrompt(s.Mode, "") }

func (s *Session) systemPrompt(mode Mode, extra string) string {
	cfg := s.app.Config.Get()
	cwd, _ := os.Getwd()
	var b strings.Builder
	b.WriteString(`You are ASG, an agentic assistant running in the user's terminal. You help with software engineering and general tasks.
You can call tools to read and edit files, run shell commands, search, and fetch web pages. Prefer acting with tools over guessing.
If information you need is missing and you cannot find it in the project, call ask_user — at any moment — instead of guessing.
Keep answers concise and use Markdown. When you change files, say what you changed. Never invent tool results.`)
	fmt.Fprintf(&b, "\n\nEnvironment:\n- Working directory: %s\n- OS: %s/%s\n- Date: %s\n", cwd, runtime.GOOS, runtime.GOARCH, time.Now().Format("2006-01-02"))

	for _, name := range []string{"ASG.md", "AGENTS.md", "CLAUDE.md"} {
		if data, err := os.ReadFile(filepath.Join(cwd, name)); err == nil {
			fmt.Fprintf(&b, "\nProject instructions from %s:\n%s\n", name, strings.TrimSpace(string(data)))
			break
		}
	}
	b.WriteString(s.app.Memory.Prompt(12000))
	if list := s.app.Skills.List(); len(list) > 0 {
		b.WriteString("\nAvailable skills (call the use_skill tool to load full instructions before doing a task one of them covers):\n")
		for _, sk := range list {
			fmt.Fprintf(&b, "- %s: %s\n", sk.Name, sk.Description)
		}
	}
	if cfg.SystemPrompt != "" {
		b.WriteString("\n" + cfg.SystemPrompt + "\n")
	}
	b.WriteString(s.Track.instructions())
	b.WriteString(mode.instructions())
	if s.Work != nil && extra == "" && !mode.ReadOnly() {
		b.WriteString(s.Work.instructions(s.Track))
	}
	if mode.orBuild() == ModeBuild && s.Plan != "" && extra == "" {
		fmt.Fprintf(&b, "\n\n## Current plan (from plan mode)\nGoal: %s\n%s\nWork through it in order. After each step check the result (read the file back, build or run tests) before moving on.\n", s.PlanGoal, clip(s.Plan, 4000))
	}
	b.WriteString(extra)
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
	all := append(s.app.Tools.All(), memoryTools(s.app.Memory)...)
	all = append(all, askTool())
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
	mode := s.Mode.orBuild()
	if mode == ModeLoop { // a plain Send in loop mode (e.g. the dashboard) builds
		mode = ModeBuild
	}
	if mode == ModeBuild {
		s.EnsureWork(user.Content)
	}
	reply, err := s.send(ctx, user, ev, turnOpts{mode: mode, maxRounds: cfg.MaxToolRounds})
	if errors.Is(err, errRoundLimit) {
		ev.Notice(fmt.Sprintf("stopped after %d tool rounds (raise max_tool_rounds in config)", cfg.MaxToolRounds))
		return "", nil
	}
	if err == nil {
		reply = s.selfReview(ctx, mode, "", reply, ev).answer
		if ctx.Err() != nil {
			return reply, ctx.Err()
		}
	}
	if err == nil && mode == ModePlan && len(ParseSteps(reply)) > 0 {
		s.Plan, s.PlanGoal = reply, user.Content
	}
	return reply, err
}

// errRoundLimit means a turn used every tool round without finishing.
var errRoundLimit = errors.New("tool round limit reached")

// turnOpts tunes one turn; the loop harness uses it for its planner,
// worker and reviewer turns.
type turnOpts struct {
	mode        Mode
	extra       string // appended to the system prompt
	system      string // replaces the system prompt entirely when set
	maxRounds   int
	noTools     bool
	temperature *float64
	// onTool, when set, is told about every finished tool call.
	onTool func(call llm.ToolCall, summary, result string, isErr bool)
}

// repeatLimit is how many identical tool calls a turn may make before the
// harness stops running them and tells the model to change course. Small
// models otherwise loop on the same read or the same failing command.
const repeatLimit = 3

func (s *Session) send(ctx context.Context, user llm.Message, ev Events, o turnOpts) (string, error) {
	cfg := s.app.Config.Get()
	provider, model, err := s.app.Provider()
	if err != nil {
		return "", err
	}
	_, pc := cfg.Active()
	ctx = withEvents(ctx, ev)
	user.Role = llm.RoleUser
	s.History = append(s.History, user)

	byName := map[string]*tools.Tool{}
	var specs []llm.ToolSpec
	for _, t := range s.toolset() {
		if o.mode.allows(t) {
			byName[t.Spec.Name] = t
			specs = append(specs, t.Spec)
		}
	}
	system := o.system
	if system == "" {
		system = s.systemPrompt(o.mode, o.extra)
	}
	temp := cfg.Temperature
	if o.temperature != nil {
		temp = o.temperature
	}
	seen := map[string]int{}

	for round := 0; round < max(o.maxRounds, 1); round++ {
		req := llm.Request{Model: model, System: system, Messages: s.History, Temperature: temp, NumCtx: numCtx(pc)}
		if !s.noTools && !o.noTools {
			req.Tools = specs
		}
		resp, err := provider.Chat(ctx, req, ev.Text)
		if err != nil && req.Tools != nil && strings.Contains(err.Error(), "does not support tools") {
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
		if len(msg.ToolCalls) == 0 && req.Tools != nil {
			// Small models often write the call out as text instead of
			// making it. Recover it rather than ending the turn early.
			msg.ToolCalls = recoverToolCalls(msg.Content, byName)
		}
		s.History = append(s.History, msg)
		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}

		for i, call := range msg.ToolCalls {
			sig := call.Name + "\x00" + canonicalArgs(call.Args)
			seen[sig]++
			var result string
			var isErr bool
			if seen[sig] > repeatLimit {
				result = fmt.Sprintf("Not run: you already made this exact %s call %d times and the result will not change. Use what you learned, try a different approach, or give your answer.", call.Name, seen[sig]-1)
				isErr = true
				ev.ToolEnd(call, "skipped: same call repeated", true)
			} else {
				result, isErr = s.runTool(ctx, call, byName, cfg.AutoApprove || s.app.Yolo, ev)
			}
			summary := describeCall(byName[call.Name], call)
			if o.onTool != nil {
				o.onTool(call, summary, result, isErr)
			}
			s.Work.Log(call, summary, result, isErr)
			s.History = append(s.History, llm.Message{Role: llm.RoleTool, Content: result, ToolCallID: call.ID, ToolName: call.Name})
			if ctx.Err() != nil {
				// Answer the remaining calls so the history stays well-formed.
				for _, rest := range msg.ToolCalls[i+1:] {
					s.History = append(s.History, llm.Message{Role: llm.RoleTool, Content: "cancelled", ToolCallID: rest.ID, ToolName: rest.Name})
				}
				s.pendingImages = nil
				return "", ctx.Err()
			}
		}
		if len(s.pendingImages) > 0 {
			// Tool results are text-only for most providers, so images a tool
			// returned (screenshots) follow as a user message.
			s.History = append(s.History, llm.Message{Role: llm.RoleUser,
				Content: fmt.Sprintf("[%d image(s) returned by the tool calls above — look at them]", len(s.pendingImages)),
				Images:  s.pendingImages})
			s.pendingImages = nil
		}
	}
	return "", errRoundLimit
}

// numCtx is the Ollama context window to request for a provider.
func numCtx(pc *config.ProviderConfig) int {
	if pc == nil || pc.Type != "ollama" || pc.NumCtx < 0 {
		return 0
	}
	if pc.NumCtx == 0 {
		return config.DefaultNumCtx
	}
	return pc.NumCtx
}

func describeCall(t *tools.Tool, call llm.ToolCall) string {
	if t == nil || t.Describe == nil {
		return ""
	}
	return t.Describe(call.Args)
}

// runTool runs one call through the permission gate and returns the result
// text and whether the call failed.
func (s *Session) runTool(ctx context.Context, call llm.ToolCall, byName map[string]*tools.Tool, auto bool, ev Events) (string, bool) {
	t := byName[call.Name]
	if t == nil {
		msg := fmt.Sprintf("error: unknown tool %q", call.Name)
		if s.app.Tools != nil && s.app.Tools.Get(call.Name) != nil {
			msg = fmt.Sprintf("error: %s is switched off in this mode (read-only). Use the read-only tools, then give your answer.", call.Name)
		}
		ev.ToolEnd(call, msg, true)
		return msg, true
	}
	summary := describeCall(t, call)
	// Writing the build's own working files (plan.md, checklist.md, …)
	// never needs permission.
	if t.NeedsApproval && !auto && !s.Work.containsCall(call) {
		cwd, _ := os.Getwd()
		rules, rerr := perms.RulesFor(call.Name, call.Args)
		key := strings.Join(rules, " ")
		if rerr != nil || (!s.allowed[key] && !s.app.Perms.Allowed(cwd, rules)) {
			gate := Gate{Summary: summary}
			if rerr == nil {
				gate.Scope = perms.Describe(rules)
			}
			d := ev.Approve(ctx, call, gate)
			if d == Deny {
				ev.ToolEnd(call, "denied", true)
				return "The user denied this tool call. Ask them how to proceed instead of retrying.", true
			}
			if rerr == nil && (d == AlwaysAllow || d == AlwaysAllowGlobal) {
				s.allowed[key] = true
				if err := s.app.Perms.Add(cwd, rules, d == AlwaysAllowGlobal); err != nil {
					ev.Notice("could not save permission: " + err.Error())
				}
			}
		}
	}
	ev.ToolStart(call, summary)
	var out string
	var err error
	if t.RunMedia != nil {
		var imgs []llm.Image
		out, imgs, err = t.RunMedia(ctx, call.Args)
		s.pendingImages = append(s.pendingImages, imgs...)
	} else {
		out, err = t.Run(ctx, call.Args)
	}
	if err != nil {
		msg := "error: " + err.Error()
		ev.ToolEnd(call, msg, true)
		return msg, true
	}
	ev.ToolEnd(call, out, false)
	return out, false
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
