package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/llm"
)

// Workspace is the folder where a build keeps the resources the user reads
// while it runs: plan.md, checklist.md, walkthrough.md, notes.md, a report
// for security and DevOps work, and activity.md — a log of every tool call
// that the harness writes itself, so it is complete even when the model
// forgets to take notes.
type Workspace struct {
	Dir string // absolute path
	Rel string // path relative to the project, for prompts
	// header opens activity.md; the folder itself is only created when
	// something is first logged or written, so a chat turn leaves nothing.
	header string
}

// ensure creates the folder and activity.md the first time it is needed.
func (w *Workspace) ensure() {
	// Checked by activity.md, not the folder: the model may have created
	// the folder itself by writing plan.md first.
	if _, err := os.Stat(filepath.Join(w.Dir, "activity.md")); err == nil {
		return
	}
	if os.MkdirAll(w.Dir, 0o755) == nil && w.header != "" {
		_ = os.WriteFile(filepath.Join(w.Dir, "activity.md"), []byte(w.header), 0o644)
	}
}

// Exists reports whether the folder has been created yet.
func (w *Workspace) Exists() bool {
	if w == nil {
		return false
	}
	_, err := os.Stat(w.Dir)
	return err == nil
}

// WorkRoot is where workspaces live inside a project.
const WorkRoot = ".asg/work"

var reSlug = regexp.MustCompile(`[^a-z0-9]+`)

// newWorkspace creates .asg/work/<date>-<slug> under project for a request.
func newWorkspace(project, request string) (*Workspace, error) {
	words := strings.Fields(strings.ToLower(request))
	if len(words) > 6 {
		words = words[:6]
	}
	slug := strings.Trim(reSlug.ReplaceAllString(strings.Join(words, "-"), "-"), "-")
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	if slug == "" {
		slug = "task"
	}
	base := time.Now().Format("2006-01-02") + "-" + slug
	rel := filepath.Join(WorkRoot, base)
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(project, rel)); os.IsNotExist(err) {
			break
		}
		rel = filepath.Join(WorkRoot, fmt.Sprintf("%s-%d", base, i))
	}
	return &Workspace{
		Dir:    filepath.Join(project, rel),
		Rel:    filepath.ToSlash(rel),
		header: fmt.Sprintf("# Activity log\n\nRequest: %s\nStarted: %s\n\n", oneLineClip(request, 300), time.Now().Format("2006-01-02 15:04")),
	}, nil
}

func (w *Workspace) write(name, content string) {
	w.ensure()
	_ = os.WriteFile(filepath.Join(w.Dir, name), []byte(content), 0o644)
}

// Log appends one tool call to activity.md.
func (w *Workspace) Log(call llm.ToolCall, summary, result string, isErr bool) {
	if w == nil {
		return
	}
	status := "ok"
	if isErr {
		status = "**error**"
	}
	line := fmt.Sprintf("- %s `%s` %s — %s: %s\n", time.Now().Format("15:04:05"), call.Name,
		oneLineClip(summary, 200), status, oneLineClip(result, 300))
	w.ensure()
	f, err := os.OpenFile(filepath.Join(w.Dir, "activity.md"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// Contains reports whether path (relative to the working directory or
// absolute) is inside the workspace.
func (w *Workspace) Contains(path string) bool {
	if w == nil || path == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(w.Dir, abs)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// containsCall reports whether a file-writing call targets the workspace.
func (w *Workspace) containsCall(call llm.ToolCall) bool {
	switch call.Name {
	case "write_file", "edit_file", "office_document":
	default:
		return false
	}
	var a struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(call.Args, &a)
	return w.Contains(a.Path)
}

// instructions tells the model which files to keep up to date.
func (w *Workspace) instructions(t Track) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n\n## Working files — %s/\n", w.Rel)
	b.WriteString("The user reads these while you work. Keep them current; writing inside this folder never needs permission:\n")
	b.WriteString("- plan.md — goal, approach and steps. Write it FIRST, before changing anything else.\n")
	b.WriteString("- checklist.md — one \"- [ ] item\" per step; change it to \"- [x]\" as soon as the step is done.\n")
	b.WriteString("- walkthrough.md — when you finish: what changed, where, why, and how to check it yourself.\n")
	b.WriteString("- notes.md — decisions, assumptions and open questions.\n")
	switch t.Domain {
	case DomainCyber, DomainDevOps:
		b.WriteString("- report.md — the full report of everything you did and found (see the domain rules).\n")
	}
	b.WriteString("activity.md is written by the harness: a log of every tool call. Don't edit it.\n")
	return b.String()
}

// loopFiles writes plan.md and checklist.md from a loop run, so they are
// always right even when the workers never touch them.
func (w *Workspace) loopFiles(st *LoopState) {
	if w == nil {
		return
	}
	var plan strings.Builder
	fmt.Fprintf(&plan, "# Plan\n\n**Objective:** %s\n\n**Track:** %s\n\n", st.Objective, st.Track)
	if st.Verify != "" {
		fmt.Fprintf(&plan, "**Verified by:** `%s`\n\n", st.Verify)
	}
	plan.WriteString("## Steps\n\n")
	for i, s := range st.Steps {
		fmt.Fprintf(&plan, "%d. %s\n", i+1, s.Title)
	}
	w.write("plan.md", plan.String())

	var cl strings.Builder
	done, total := st.Progress()
	fmt.Fprintf(&cl, "# Checklist — %d/%d · %s\n\n", done, total, st.Status)
	for _, s := range st.Steps {
		box := " "
		if s.Status == StepDone {
			box = "x"
		}
		fmt.Fprintf(&cl, "- [%s] %s", box, s.Title)
		if s.Failure != "" && s.Status != StepDone {
			fmt.Fprintf(&cl, "  \n  _last attempt: %s_", s.Failure)
		}
		cl.WriteString("\n")
	}
	w.write("checklist.md", cl.String())
}

// walkthrough writes walkthrough.md at the end of a loop run.
func (w *Workspace) walkthrough(st *LoopState) {
	if w == nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Walkthrough\n\n**Objective:** %s\n\n**Result:** %s — %s\n\n## What each step did\n\n", st.Objective, st.Status, st.Outcome)
	for i, s := range st.Steps {
		mark := "✗"
		if s.Status == StepDone {
			mark = "✓"
		}
		fmt.Fprintf(&b, "%d. %s **%s**", i+1, mark, s.Title)
		if s.Notes != "" {
			fmt.Fprintf(&b, "  \n   %s", s.Notes)
		}
		b.WriteString("\n")
	}
	if len(st.Reviews) > 0 {
		b.WriteString("\n## Final review\n\n")
		for _, r := range st.Reviews {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	if st.Verify != "" {
		fmt.Fprintf(&b, "\n## Check it yourself\n\n```\n%s\n```\n", st.Verify)
	}
	w.write("walkthrough.md", b.String())
}
