package agent

import (
	"strings"

	"github.com/kxngHADES/asg-harness/internal/tools"
)

// Mode shapes how the agent works on a turn: which tools it gets and what
// the system prompt asks of it. Small models do noticeably better when a
// turn has one job (look, plan, or change) instead of all three at once.
type Mode string

const (
	// ModeBuild is the default: every tool, behind the usual permission gates.
	ModeBuild Mode = "build"
	// ModePlan investigates read-only and ends with a numbered plan.
	ModePlan Mode = "plan"
	// ModeAsk answers questions about the code, read-only.
	ModeAsk Mode = "ask"
	// ModeLoop treats each message as an objective for the loop harness.
	ModeLoop Mode = "loop"
)

// Modes lists every mode in the order Shift+Tab cycles through them.
var Modes = []Mode{ModeBuild, ModePlan, ModeLoop, ModeAsk}

// ParseMode accepts a mode name or a short alias.
func ParseMode(s string) (Mode, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "build", "b", "code", "act":
		return ModeBuild, true
	case "plan", "p":
		return ModePlan, true
	case "ask", "a", "chat", "explain":
		return ModeAsk, true
	case "loop", "l", "auto":
		return ModeLoop, true
	}
	return "", false
}

// Next returns the mode after m in the Shift+Tab cycle.
func (m Mode) Next() Mode {
	for i, x := range Modes {
		if x == m.orBuild() {
			return Modes[(i+1)%len(Modes)]
		}
	}
	return ModeBuild
}

func (m Mode) orBuild() Mode {
	if m == "" {
		return ModeBuild
	}
	return m
}

// ReadOnly reports whether the mode withholds tools that change things.
func (m Mode) ReadOnly() bool { return m == ModePlan || m == ModeAsk }

// Describe is a one-line explanation shown when the user switches mode.
func (m Mode) Describe() string {
	switch m.orBuild() {
	case ModePlan:
		return "read-only: investigates and writes a step-by-step plan, changes nothing"
	case ModeAsk:
		return "read-only: answers questions about your code"
	case ModeLoop:
		return "your next message is an objective: plan → do → verify → review, step by step until done"
	}
	return "reads, edits and runs commands (asks before changes unless YOLO is on)"
}

// allows reports whether a tool is available in this mode.
func (m Mode) allows(t *tools.Tool) bool {
	if !m.ReadOnly() {
		return true
	}
	switch t.Spec.Name {
	case "web_fetch", "use_skill", "memory_save", "memory_delete", "ask_user", "screenshot":
		return true // reach out, look, or touch ~/.asg only — never change the project
	}
	return !t.NeedsApproval
}

// instructions is the system prompt addendum for the mode.
func (m Mode) instructions() string {
	switch m.orBuild() {
	case ModePlan:
		return `
## Mode: PLAN (read-only)
You are planning, not doing. Tools that edit files or run commands are switched off.
1. Look around first: list_dir, glob, grep and read_file the parts of the project the request touches.
2. Then reply with a plan as a numbered list ("1. ...", "2. ..."), 2–8 steps.
   Each step is one small, concrete change: name the file(s) and what to change, and how to check it worked.
3. After the list, mention any open questions or risks in one or two lines.
Do not write the code itself in the plan.`
	case ModeAsk:
		return `
## Mode: ASK (read-only)
Answer the user's question. Read files and search to ground your answer in the actual code; quote file paths and line numbers.
Tools that edit files or run commands are switched off — if a change is needed, describe it and suggest switching to build mode.`
	}
	return ""
}
