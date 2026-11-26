package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/llm"
)

// turnEvents forwards one turn's progress to the UI as "asg:turn" events
// and captures files the agent writes as artifacts.
type turnEvents struct {
	a  *App
	ls *liveSession
	id string
	// unattended turns (schedules) can't show a gate or a question.
	unattended bool
	allowAll   bool

	mu sync.Mutex
	n  int
}

func (e *turnEvents) send(kind string, v map[string]any) {
	if v == nil {
		v = map[string]any{}
	}
	v["session"], v["type"] = e.id, kind
	e.a.emit("asg:turn", v)
}

func (e *turnEvents) Text(d string) { e.send("text", map[string]any{"text": d}) }

func (e *turnEvents) Notice(m string) { e.send("notice", map[string]any{"text": m}) }

func (e *turnEvents) ToolStart(c llm.ToolCall, summary string) {
	e.send("tool_start", map[string]any{"id": c.ID, "name": c.Name, "summary": summary, "args": clipStr(string(c.Args), 4000)})
}

func (e *turnEvents) ToolEnd(c llm.ToolCall, result string, isErr bool) {
	e.send("tool_end", map[string]any{"id": c.ID, "name": c.Name, "result": clipStr(result, 8000), "error": isErr})
	if !isErr {
		e.capture(c)
	}
}

func (e *turnEvents) ReviewUpdate(pass, total int, lens string, ok bool, note string) {
	e.send("review", map[string]any{"pass": pass, "total": total, "lens": lens, "ok": ok, "note": note})
}

func (e *turnEvents) nextID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.n++
	return fmt.Sprintf("r%d-%d", time.Now().UnixNano(), e.n)
}

func (e *turnEvents) Approve(ctx context.Context, c llm.ToolCall, gate agent.Gate) agent.Decision {
	if gate.Sensitive && (e.allowAll || e.unattended) {
		e.Notice(fmt.Sprintf("%s is a sensitive action that a person must approve — skipped in this unattended run.", c.Name))
		return agent.Deny
	}
	if e.allowAll || e.inSandbox(c) {
		return agent.Allow
	}
	if e.unattended {
		e.Notice(fmt.Sprintf("%s needs permission and nobody is there to give it — skipped. Turn on “Allow tools” for this schedule to let it run.", c.Name))
		return agent.Deny
	}
	id := e.nextID()
	ch := make(chan agent.Decision, 1)
	e.ls.mu.Lock()
	e.ls.approvals[id] = ch
	e.ls.mu.Unlock()
	defer func() {
		e.ls.mu.Lock()
		delete(e.ls.approvals, id)
		e.ls.mu.Unlock()
	}()
	e.send("approval", map[string]any{"id": id, "name": c.Name, "summary": gate.Summary, "scope": gate.Scope, "args": clipStr(string(c.Args), 4000)})
	select {
	case d := <-ch:
		e.send("approval_done", map[string]any{"id": id})
		return d
	case <-ctx.Done():
		e.send("approval_done", map[string]any{"id": id})
		return agent.Deny
	}
}

// inSandbox reports whether a file-writing call stays inside the session's
// own sandbox folder. Those never need permission: the sandbox exists so a
// chat can make files without touching anything else.
func (e *turnEvents) inSandbox(c llm.ToolCall) bool {
	switch c.Name {
	case "write_file", "edit_file", "office_document":
	default:
		return false
	}
	e.ls.mu.Lock()
	meta := e.ls.file.SessionMeta
	e.ls.mu.Unlock()
	if meta.Workdir != "" || meta.ProjectID != "" {
		return false
	}
	var args struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(c.Args, &args) != nil || args.Path == "" {
		return false
	}
	return within(e.a.store.SandboxDir(meta.ID), args.Path)
}

// AskUser implements agent.Asker.
func (e *turnEvents) AskUser(ctx context.Context, question string, choices []string) (string, error) {
	if e.unattended {
		return agent.NoUserAnswer, nil
	}
	id := e.nextID()
	ch := make(chan string, 1)
	e.ls.mu.Lock()
	e.ls.asks[id] = ch
	e.ls.mu.Unlock()
	defer func() {
		e.ls.mu.Lock()
		delete(e.ls.asks, id)
		e.ls.mu.Unlock()
	}()
	if choices == nil {
		choices = []string{}
	}
	e.send("ask", map[string]any{"id": id, "question": question, "choices": choices})
	select {
	case ans := <-ch:
		e.send("ask_done", map[string]any{"id": id, "answer": ans})
		return ans, nil
	case <-ctx.Done():
		e.send("ask_done", map[string]any{"id": id})
		return "", ctx.Err()
	}
}

// capture keeps files written by write_file / office_document as
// artifacts. Coding sessions only keep documents and images, so source
// files don't flood the library; a build's own working files never count.
func (e *turnEvents) capture(c llm.ToolCall) {
	if c.Name != "write_file" && c.Name != "office_document" {
		return
	}
	var args struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(c.Args, &args) != nil || args.Path == "" {
		return
	}
	p := args.Path
	if !filepath.IsAbs(p) {
		if abs, err := filepath.Abs(p); err == nil { // the turn runs in the session folder
			p = abs
		}
	}
	if e.ls.sess.Work != nil && e.ls.sess.Work.Contains(p) {
		return
	}
	e.ls.mu.Lock()
	meta := e.ls.file.SessionMeta
	e.ls.mu.Unlock()
	if meta.Kind == KindCode {
		switch ArtifactKind(filepath.Ext(p)) {
		case "document", "pdf", "image":
		default:
			return
		}
	}
	if meta.Title == "" {
		meta.Title = "Untitled"
	}
	art, err := e.a.store.Capture(meta, p)
	if err != nil {
		return
	}
	if art.Versions == 1 {
		e.ls.mu.Lock()
		e.ls.file.Artifacts++
		e.ls.mu.Unlock()
	}
	e.send("artifact", map[string]any{"artifact": art})
	e.a.emit("asg:artifacts", nil)
}
