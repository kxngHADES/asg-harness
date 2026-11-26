package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/proc"
	"github.com/kxngHADES/asg-harness/internal/sprite"
)

// App is bound to the frontend: every exported method is callable from JS
// as window.go.main.App.<Method>.
type App struct {
	ctx   context.Context
	core  *agent.App
	store *Store

	setMu    sync.Mutex
	settings Settings

	mu   sync.Mutex
	live map[string]*liveSession

	// turnMu lets one turn run at a time. Tools resolve paths against the
	// process working directory, so each turn switches into its session's
	// folder; running two at once would mix them up.
	turnMu sync.Mutex

	schedMu  sync.Mutex
	stopTick chan struct{}

	// Updates (updater.go) and the tray icon (tray.go).
	upd      updater
	stopUpd  chan struct{}
	tray     *trayItems
	trayOn   atomic.Bool
	quitting atomic.Bool

	stopComputer func()

	// Hooks replace the Wails runtime when the UI is served for development
	// (devserve.go): events go to the browser and folder picks come from it.
	emitHook  func(name string, data any)
	pickHook  func(title string) (string, error)
	netBridge *networkBridge
}

// liveSession is a session loaded into memory.
type liveSession struct {
	mu        sync.Mutex
	file      *SessionFile
	sess      *agent.Session
	running   bool
	cancel    context.CancelFunc
	approvals map[string]chan agent.Decision
	asks      map[string]chan string
}

func NewApp(core *agent.App) *App {
	s := loadSettings()
	return &App{core: core, settings: s, store: &Store{dir: s.DataDir}, live: map[string]*liveSession{}}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.store.Init(); err != nil {
		// An external drive that is not plugged in: fall back to the default
		// folder for this run so the app still opens.
		a.store.setDir(DefaultDataDir())
		_ = a.store.Init()
	}
	a.startScheduler()
	a.stopComputer = a.startComputer()
	a.stopUpd = make(chan struct{})
	a.startUpdateChecks()
	// New install or just updated: make sure tools have uv + Python 3.12.
	go a.bootstrapRuntime()
	if addr := os.Getenv("ASG_MOBILE_ADDR"); addr != "off" {
		a.netBridge = newNetworkBridge(a, addr)
		_ = a.netBridge.start()
	}
	if ctx != nil { // nil when served for development (devserve.go)
		a.startTray()
	}
}

// beforeClose keeps ASG running in the tray when the window is closed,
// unless the user turned that off or chose Quit.
func (a *App) beforeClose(context.Context) bool {
	a.setMu.Lock()
	quitOnClose := a.settings.QuitOnClose
	a.setMu.Unlock()
	if a.quitting.Load() || quitOnClose || !a.trayOn.Load() {
		return false
	}
	wruntime.WindowHide(a.ctx)
	return true
}

// SetQuitOnClose saves whether closing the window quits ASG (true) or keeps
// it in the tray (false).
func (a *App) SetQuitOnClose(quit bool) error {
	a.setMu.Lock()
	a.settings.QuitOnClose = quit
	s := a.settings
	a.setMu.Unlock()
	return saveSettings(s)
}

// QuitApp quits ASG even when it would normally stay in the tray.
func (a *App) QuitApp() { a.quitApp() }

func (a *App) shutdown(context.Context) {
	a.mu.Lock()
	for _, ls := range a.live {
		ls.mu.Lock()
		if ls.cancel != nil {
			ls.cancel()
		}
		ls.mu.Unlock()
	}
	a.mu.Unlock()
	if a.stopTick != nil {
		close(a.stopTick)
	}
	if a.stopUpd != nil {
		close(a.stopUpd)
	}
	if a.netBridge != nil {
		a.netBridge.stop()
	}
	if a.stopComputer != nil {
		a.stopComputer()
	}
	a.stopTray()
}

func (a *App) emit(name string, data any) {
	if a.netBridge != nil {
		a.netBridge.broadcast(name, data)
	}
	if a.emitHook != nil {
		a.emitHook(name, data)
		return
	}
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, name, data)
	}
}

// ---------- bootstrap ----------

type ModelInfo struct {
	Provider  string   `json:"provider"`
	Model     string   `json:"model"`
	Type      string   `json:"type"`
	Providers []string `json:"providers"`
}

func (a *App) modelInfo() ModelInfo {
	cfg := a.core.Config.Get()
	name, pc := cfg.Active()
	mi := ModelInfo{Provider: name, Providers: cfg.ProviderNames()}
	if pc != nil {
		mi.Model, mi.Type = pc.Model, pc.Type
	}
	return mi
}

type Boot struct {
	Settings     Settings      `json:"settings"`
	DataDir      string        `json:"data_dir"`
	DataFallback bool          `json:"data_fallback"`
	Home         string        `json:"home"`
	Sessions     []SessionMeta `json:"sessions"`
	Projects     []Project     `json:"projects"`
	Model        ModelInfo     `json:"model"`
	HighlightCSS string        `json:"highlight_css"`
	OS           string        `json:"os"`
	Version      string        `json:"version"`
	User         string        `json:"user"`
	CrabIdle     string        `json:"crab_idle"`
	CrabWork     string        `json:"crab_work"`
	Tray         bool          `json:"tray"` // a tray icon is showing (Windows, Linux)
	Domains      []DomainInfo  `json:"domains"`
	Track        agent.Track   `json:"track"` // the default for new chats
	Computer     ComputerInfo  `json:"computer"`
}

func (a *App) Bootstrap() Boot {
	home, _ := os.UserHomeDir()
	user := os.Getenv("USERNAME")
	if user == "" {
		user = os.Getenv("USER")
	}
	a.setMu.Lock()
	st := a.settings
	a.setMu.Unlock()
	return Boot{
		Settings: st, DataDir: a.store.Dir(), DataFallback: !samePath(a.store.Dir(), st.DataDir),
		Home: home, Sessions: a.store.ListSessions(), Projects: a.store.Projects(),
		Model: a.modelInfo(), HighlightCSS: HighlightCSS(), OS: runtime.GOOS, Version: version, User: user,
		CrabIdle: sprite.SVG(sprite.Idle, 140, 3), CrabWork: sprite.SVG(sprite.Work, 110, 3), Tray: a.trayOn.Load(),
		Domains: a.ListDomains(), Track: a.DefaultTrack(), Computer: a.ComputerInfo(),
	}
}

func (a *App) RenderMarkdown(src string) string { return RenderMarkdown(src) }

// ---------- sessions ----------

type NewSessionOpts struct {
	Kind      string `json:"kind"`
	ProjectID string `json:"project_id"`
	Workdir   string `json:"workdir"`
	Mode      string `json:"mode"`
	Domain    string `json:"domain"`
	Flavor    string `json:"flavor"`
	Title     string `json:"title"`
}

func (a *App) ListSessions() []SessionMeta { return a.store.ListSessions() }

func (a *App) NewSession(o NewSessionOpts) (SessionMeta, error) {
	switch o.Kind {
	case KindChat, KindCode, KindArtifact, KindProject, KindScheduled:
	default:
		o.Kind = KindChat
	}
	if o.Kind == KindCode && o.Workdir == "" {
		return SessionMeta{}, errors.New("a coding session needs a folder")
	}
	if o.Workdir != "" {
		if st, err := os.Stat(o.Workdir); err != nil || !st.IsDir() {
			return SessionMeta{}, fmt.Errorf("folder not found: %s", o.Workdir)
		}
	}
	if o.ProjectID != "" && o.Kind == KindChat {
		o.Kind = KindProject
	}
	mode := o.Mode
	if _, ok := agent.ParseMode(mode); !ok || mode == "" {
		mode = string(agent.ModeBuild)
	}
	now := time.Now()
	t := a.resolveTrack(o.Domain, o.Flavor)
	f := &SessionFile{SessionMeta: SessionMeta{
		ID: newID("s"), Title: strings.TrimSpace(o.Title), Kind: o.Kind, ProjectID: o.ProjectID,
		Workdir: o.Workdir, Mode: mode, Domain: string(t.Domain), Flavor: t.Flavor,
		Created: now, Updated: now, Model: a.modelInfo().Model,
	}}
	if f.Kind == KindCode {
		f.Branch = gitBranch(f.Workdir)
	}
	if err := a.store.SaveSession(f); err != nil {
		return SessionMeta{}, err
	}
	a.emit("asg:sessions", nil)
	return f.SessionMeta, nil
}

// workdir is the folder a session's agent works in.
func (a *App) workdir(m SessionMeta) string {
	if m.Workdir != "" {
		return m.Workdir
	}
	if m.ProjectID != "" {
		if p := a.project(m.ProjectID); p != nil {
			if p.Folder != "" {
				return p.Folder
			}
			return a.store.ProjectFilesDir(p.ID)
		}
	}
	return a.store.SandboxDir(m.ID)
}

func (a *App) project(id string) *Project {
	for _, p := range a.store.Projects() {
		if p.ID == id {
			p := p
			return &p
		}
	}
	return nil
}

// instructions adds session-kind and project guidance to the system prompt.
func (a *App) instructions(m SessionMeta) string {
	var b strings.Builder
	if m.ProjectID != "" {
		if p := a.project(m.ProjectID); p != nil {
			fmt.Fprintf(&b, "## Project: %s\n", p.Name)
			if p.Description != "" {
				b.WriteString(p.Description + "\n")
			}
			if strings.TrimSpace(p.Instructions) != "" {
				b.WriteString("Project instructions (follow them):\n" + strings.TrimSpace(p.Instructions) + "\n")
			}
		}
	}
	switch m.Kind {
	case KindArtifact:
		b.WriteString(`
## Artifact session
The user wants a finished file (an artifact) they can open and keep. Produce it as a file in the working directory:
- a web page, app or interactive demo: ONE self-contained .html file (inline CSS and JS, no build step)
- a diagram or icon: .svg; notes, reports and docs: .md; Word/PowerPoint/Excel: the office_document tool
- data: .csv or .json
Write the file with write_file (or office_document), then reply with two or three sentences: what you made and how to use it.
When asked for changes, rewrite the same file rather than creating a new one.
`)
	case KindChat, KindScheduled:
		if m.Workdir == "" && m.ProjectID == "" {
			b.WriteString("\nYour working directory is a private sandbox folder for this conversation. Files you create there are kept with the chat and shown to the user as artifacts.\n")
		}
	}
	return b.String()
}

func (a *App) loadLive(id string) (*liveSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ls := a.live[id]; ls != nil {
		return ls, nil
	}
	f, err := a.store.LoadSession(id)
	if err != nil {
		return nil, err
	}
	s := a.core.NewSession()
	s.History = f.History
	s.Plan, s.PlanGoal = f.Plan, f.PlanGoal
	s.Usage = f.Usage
	if m, ok := agent.ParseMode(f.Mode); ok {
		s.Mode = m
	}
	if f.WorkDir != "" {
		if st, err := os.Stat(f.WorkDir); err == nil && st.IsDir() {
			rel, rerr := filepath.Rel(a.workdir(f.SessionMeta), f.WorkDir)
			if rerr != nil {
				rel = f.WorkDir
			}
			s.Work = &agent.Workspace{Dir: f.WorkDir, Rel: filepath.ToSlash(rel)}
		}
	}
	ls := &liveSession{file: f, sess: s, approvals: map[string]chan agent.Decision{}, asks: map[string]chan string{}}
	a.applyTrack(ls)
	a.live[id] = ls
	return ls, nil
}

// Item is one entry in a transcript as the UI draws it.
type Item struct {
	Type    string    `json:"type"` // user | assistant | tool | notice
	Text    string    `json:"text,omitempty"`
	HTML    string    `json:"html,omitempty"`
	Images  []string  `json:"images,omitempty"` // data URLs
	Tool    *ToolItem `json:"tool,omitempty"`
	Pending bool      `json:"pending,omitempty"`
}

type ToolItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Args    string `json:"args,omitempty"`
	Result  string `json:"result,omitempty"`
	Error   bool   `json:"error,omitempty"`
	Done    bool   `json:"done"`
}

type SessionView struct {
	Meta      SessionMeta `json:"meta"`
	Items     []Item      `json:"items"`
	Running   bool        `json:"running"`
	Workdir   string      `json:"workdir"`
	Sandbox   bool        `json:"sandbox"`
	Project   *Project    `json:"project,omitempty"`
	WorkFiles []WorkFile  `json:"work_files"`
	Artifacts []Artifact  `json:"artifacts"`
	Usage     llm.Usage   `json:"usage"`
}

func (a *App) OpenSession(id string) (SessionView, error) {
	ls, err := a.loadLive(id)
	if err != nil {
		return SessionView{}, err
	}
	ls.mu.Lock()
	running := ls.running
	meta := ls.file.SessionMeta
	var hist []llm.Message
	if !running {
		hist = ls.sess.History
	} else {
		hist = ls.file.History // the last saved state; live events fill in the rest
	}
	usage := ls.sess.Usage
	ls.mu.Unlock()
	v := SessionView{Meta: meta, Items: a.items(hist), Running: running, Workdir: a.workdir(meta),
		Sandbox: meta.Workdir == "" && meta.ProjectID == "", Usage: usage, WorkFiles: a.workFiles(ls)}
	if meta.ProjectID != "" {
		v.Project = a.project(meta.ProjectID)
	}
	for _, art := range a.store.Artifacts() {
		if art.SessionID == id {
			v.Artifacts = append(v.Artifacts, art)
		}
	}
	return v, nil
}

func (a *App) describe(name string, args json.RawMessage) string {
	if t := a.core.Tools.Get(name); t != nil && t.Describe != nil {
		return t.Describe(args)
	}
	switch name {
	case "ask_user", "use_skill", "save_memory", "delete_memory":
		var v map[string]any
		_ = json.Unmarshal(args, &v)
		for _, k := range []string{"question", "name"} {
			if s, ok := v[k].(string); ok {
				return s
			}
		}
	}
	return ""
}

// items turns a history into transcript entries.
func (a *App) items(h []llm.Message) []Item {
	out := []Item{}
	byID := map[string]*ToolItem{}
	for _, m := range h {
		switch m.Role {
		case llm.RoleUser:
			if isToolImageNote(m) {
				it := Item{Type: "notice", Text: "Images returned by tools"}
				for _, img := range m.Images {
					it.Images = append(it.Images, dataURL(img))
				}
				out = append(out, it)
				continue
			}
			it := Item{Type: "user", Text: userText(m.Content)}
			for _, img := range m.Images {
				it.Images = append(it.Images, dataURL(img))
			}
			out = append(out, it)
		case llm.RoleAssistant:
			if strings.TrimSpace(m.Content) != "" {
				out = append(out, Item{Type: "assistant", Text: m.Content, HTML: RenderMarkdown(m.Content)})
			}
			for _, c := range m.ToolCalls {
				t := &ToolItem{ID: c.ID, Name: c.Name, Summary: a.describe(c.Name, c.Args), Args: clipStr(string(c.Args), 4000)}
				byID[c.ID] = t
				out = append(out, Item{Type: "tool", Tool: t})
			}
		case llm.RoleTool:
			if t := byID[m.ToolCallID]; t != nil {
				t.Result, t.Done = clipStr(m.Content, 8000), true
				t.Error = toolFailed(m.Content)
			}
		}
	}
	return out
}

// userText hides the skill prompt that a skill run puts before the request.
func userText(s string) string {
	if strings.HasPrefix(s, "# Skill: ") {
		if i := strings.LastIndex(s, "\n---\nUser request: "); i >= 0 {
			name := strings.TrimSpace(strings.SplitN(strings.TrimPrefix(s, "# Skill: "), "\n", 2)[0])
			return "/" + name + " " + s[i+len("\n---\nUser request: "):]
		}
	}
	return s
}

func toolFailed(result string) bool {
	return strings.HasPrefix(result, "error:") || strings.HasPrefix(result, "The user denied") ||
		strings.HasPrefix(result, "Not run:") || result == "cancelled"
}

func dataURL(img llm.Image) string {
	return "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
}

type ImageIn struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"` // base64
}

// Send starts a turn in the background. Progress arrives as "asg:turn"
// events carrying the session id.
func (a *App) Send(id, text string, images []ImageIn, skill string) error {
	ls, err := a.loadLive(id)
	if err != nil {
		return err
	}
	msg := llm.Message{Content: text}
	for _, img := range images {
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			return fmt.Errorf("bad image: %w", err)
		}
		msg.Images = append(msg.Images, llm.Image{MediaType: img.MediaType, Data: data})
	}
	if skill != "" {
		sk := a.core.Skills.Get(skill)
		if sk == nil {
			return fmt.Errorf("no skill %q", skill)
		}
		msg.Content = agent.SkillPrompt(sk) + "\n\n---\nUser request: " + text
	}
	if strings.TrimSpace(msg.Content) == "" && len(msg.Images) > 0 {
		msg.Content = "Describe this image."
	}
	if strings.TrimSpace(msg.Content) == "" {
		return errors.New("empty message")
	}
	ls.mu.Lock()
	if ls.running {
		ls.mu.Unlock()
		return errors.New("this session is still working — stop it or wait")
	}
	ctx, cancel := context.WithCancel(context.Background())
	ls.running, ls.cancel = true, cancel
	if ls.file.Title == "" {
		ls.file.Title = autoTitle(text)
		ls.file.Updated = time.Now()
		_ = a.store.SaveSession(ls.file)
	}
	ls.mu.Unlock()
	shown := text
	if skill != "" {
		shown = "/" + skill + " " + text
	}
	var imgURLs []string
	for _, img := range msg.Images {
		imgURLs = append(imgURLs, dataURL(img))
	}
	a.emit("asg:turn", map[string]any{"session": id, "type": "user", "text": shown, "images": imgURLs})
	a.emit("asg:sessions", nil)
	go a.runTurn(ls, ctx, msg, false, false)
	return nil
}

func (a *App) runTurn(ls *liveSession, ctx context.Context, msg llm.Message, unattended, allowAll bool) (string, error) {
	id := ls.file.ID
	defer func() {
		ls.mu.Lock()
		ls.running = false
		if ls.cancel != nil {
			ls.cancel()
			ls.cancel = nil
		}
		ls.mu.Unlock()
	}()
	if !a.turnMu.TryLock() {
		a.emit("asg:turn", map[string]any{"session": id, "type": "status", "text": "Waiting for another session to finish…"})
		a.turnMu.Lock()
	}
	defer a.turnMu.Unlock()

	ls.mu.Lock()
	meta := ls.file.SessionMeta
	ls.mu.Unlock()
	dir := a.workdir(meta)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.finishTurn(ls, "", err)
		return "", err
	}
	if err := os.Chdir(dir); err != nil {
		err = fmt.Errorf("can't open the session folder %s: %w", dir, err)
		a.finishTurn(ls, "", err)
		return "", err
	}
	ls.sess.Instructions = a.instructions(meta)
	if m, ok := agent.ParseMode(meta.Mode); ok {
		ls.sess.Mode = m
	}
	ls.mu.Lock()
	a.applyTrack(ls)
	ls.mu.Unlock()
	a.emit("asg:turn", map[string]any{"session": id, "type": "status", "text": ""})
	ev := &turnEvents{a: a, ls: ls, id: id, unattended: unattended, allowAll: allowAll}
	start := time.Now()
	reply, err := ls.sess.Send(ctx, msg, ev)
	if errors.Is(err, context.Canceled) {
		err = nil
		reply = ""
		a.emit("asg:turn", map[string]any{"session": id, "type": "notice", "text": "Stopped."})
	}
	a.finishTurn(ls, reply, err)
	if err == nil {
		a.emit("asg:turn", map[string]any{"session": id, "type": "done", "seconds": time.Since(start).Seconds(),
			"input_tokens": ls.sess.Usage.InputTokens, "output_tokens": ls.sess.Usage.OutputTokens})
	}
	return reply, err
}

// finishTurn copies the agent's state into the session file and saves it.
func (a *App) finishTurn(ls *liveSession, reply string, err error) {
	ls.mu.Lock()
	f := ls.file
	f.History = ls.sess.History
	f.Plan, f.PlanGoal, f.Usage = ls.sess.Plan, ls.sess.PlanGoal, ls.sess.Usage
	if ls.sess.Work != nil && ls.sess.Work.Exists() {
		f.WorkDir = ls.sess.Work.Dir
	}
	f.Updated = time.Now()
	f.Model = a.modelInfo().Model
	f.LastError = ""
	if err != nil {
		f.LastError = clipStr(err.Error(), 300)
	}
	if p := lastAssistant(f.History); p != "" {
		f.Preview = oneLine(p, 140)
	}
	if f.Kind == KindCode {
		f.Branch = gitBranch(f.Workdir)
	}
	saveErr := a.store.SaveSession(f)
	ls.mu.Unlock()
	if err != nil {
		a.emit("asg:turn", map[string]any{"session": f.ID, "type": "error", "text": err.Error()})
	} else if saveErr != nil {
		a.emit("asg:turn", map[string]any{"session": f.ID, "type": "error", "text": "couldn't save the session: " + saveErr.Error()})
	}
	a.emit("asg:sessions", nil)
}

func lastAssistant(h []llm.Message) string {
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].Role == llm.RoleAssistant && strings.TrimSpace(h[i].Content) != "" {
			return h[i].Content
		}
	}
	return ""
}

func (a *App) Stop(id string) {
	a.mu.Lock()
	ls := a.live[id]
	a.mu.Unlock()
	if ls == nil {
		return
	}
	ls.mu.Lock()
	if ls.cancel != nil {
		ls.cancel()
	}
	ls.mu.Unlock()
}

func (a *App) Approve(id, reqID, decision string) error {
	a.mu.Lock()
	ls := a.live[id]
	a.mu.Unlock()
	if ls == nil {
		return errors.New("session not running")
	}
	ls.mu.Lock()
	ch := ls.approvals[reqID]
	ls.mu.Unlock()
	if ch == nil {
		return errors.New("that request is no longer waiting")
	}
	d := agent.Deny
	switch decision {
	case "allow":
		d = agent.Allow
	case "always":
		d = agent.AlwaysAllow
	case "always_global":
		d = agent.AlwaysAllowGlobal
	}
	select {
	case ch <- d:
	default:
	}
	return nil
}

func (a *App) Answer(id, reqID, answer string) error {
	a.mu.Lock()
	ls := a.live[id]
	a.mu.Unlock()
	if ls == nil {
		return errors.New("session not running")
	}
	ls.mu.Lock()
	ch := ls.asks[reqID]
	ls.mu.Unlock()
	if ch == nil {
		return errors.New("that question is no longer waiting")
	}
	select {
	case ch <- answer:
	default:
	}
	return nil
}

func (a *App) updateMeta(id string, fn func(f *SessionFile)) error {
	ls, err := a.loadLive(id)
	if err != nil {
		return err
	}
	ls.mu.Lock()
	fn(ls.file)
	err = a.store.SaveSession(ls.file)
	ls.mu.Unlock()
	a.emit("asg:sessions", nil)
	return err
}

func (a *App) RenameSession(id, title string) error {
	return a.updateMeta(id, func(f *SessionFile) { f.Title = strings.TrimSpace(title) })
}

func (a *App) PinSession(id string, pinned bool) error {
	return a.updateMeta(id, func(f *SessionFile) { f.Pinned = pinned })
}

func (a *App) SetSessionMode(id, mode string) error {
	if _, ok := agent.ParseMode(mode); !ok {
		return fmt.Errorf("unknown mode %q", mode)
	}
	return a.updateMeta(id, func(f *SessionFile) { f.Mode = mode })
}

func (a *App) MoveSessionToProject(id, projectID string) error {
	return a.updateMeta(id, func(f *SessionFile) {
		f.ProjectID = projectID
		if projectID != "" && f.Kind == KindChat {
			f.Kind = KindProject
		} else if projectID == "" && f.Kind == KindProject {
			f.Kind = KindChat
		}
	})
}

func (a *App) DeleteSession(id string) error {
	a.mu.Lock()
	if ls := a.live[id]; ls != nil {
		ls.mu.Lock()
		running := ls.running
		ls.mu.Unlock()
		if running {
			a.mu.Unlock()
			return errors.New("stop the session before deleting it")
		}
		delete(a.live, id)
	}
	a.mu.Unlock()
	err := a.store.DeleteSession(id)
	a.emit("asg:sessions", nil)
	return err
}

// WorkFile is one of a build's working files (plan.md, checklist.md, …).
type WorkFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	HTML string `json:"html"`
}

func (a *App) workFiles(ls *liveSession) []WorkFile {
	ls.mu.Lock()
	dir := ls.file.WorkDir
	if ls.sess.Work != nil && ls.sess.Work.Exists() {
		dir = ls.sess.Work.Dir
	}
	ls.mu.Unlock()
	out := []WorkFile{}
	if dir == "" {
		return out
	}
	order := []string{"plan.md", "checklist.md", "walkthrough.md", "notes.md", "report.md", "activity.md"}
	for _, name := range order {
		p := filepath.Join(dir, name)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		out = append(out, WorkFile{Name: name, Path: p, HTML: RenderMarkdown(clipStr(string(data), 60000))})
	}
	return out
}

func (a *App) WorkFiles(id string) ([]WorkFile, error) {
	ls, err := a.loadLive(id)
	if err != nil {
		return nil, err
	}
	return a.workFiles(ls), nil
}

// CodeFolder groups coding sessions by folder for the Code page.
type CodeFolder struct {
	Path     string        `json:"path"`
	Name     string        `json:"name"`
	Branch   string        `json:"branch"`
	Exists   bool          `json:"exists"`
	Updated  time.Time     `json:"updated"`
	Sessions []SessionMeta `json:"sessions"`
}

func (a *App) CodeFolders() []CodeFolder {
	byPath := map[string]*CodeFolder{}
	var order []string
	for _, m := range a.store.ListSessions() {
		if m.Kind != KindCode || m.Workdir == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(m.Workdir))
		cf := byPath[key]
		if cf == nil {
			_, err := os.Stat(m.Workdir)
			cf = &CodeFolder{Path: m.Workdir, Name: filepath.Base(m.Workdir), Exists: err == nil}
			byPath[key] = cf
			order = append(order, key)
		}
		cf.Sessions = append(cf.Sessions, m)
		if m.Updated.After(cf.Updated) {
			cf.Updated = m.Updated
		}
	}
	out := make([]CodeFolder, 0, len(order))
	for _, k := range order {
		cf := byPath[k]
		if cf.Exists {
			cf.Branch = gitBranch(cf.Path)
		}
		out = append(out, *cf)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// ---------- projects ----------

func (a *App) ListProjects() []Project { return a.store.Projects() }

func (a *App) SaveProject(p Project) (Project, error) {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return p, errors.New("give the project a name")
	}
	if p.Folder != "" {
		if st, err := os.Stat(p.Folder); err != nil || !st.IsDir() {
			return p, fmt.Errorf("folder not found: %s", p.Folder)
		}
	}
	list := a.store.Projects()
	now := time.Now()
	p.Updated = now
	found := false
	for i := range list {
		if list[i].ID == p.ID {
			p.Created = list[i].Created
			list[i] = p
			found = true
		}
	}
	if !found {
		p.ID, p.Created = newID("p"), now
		if p.Color == "" {
			p.Color = []string{"amber", "rose", "sky", "lime", "violet", "teal"}[len(list)%6]
		}
		list = append(list, p)
	}
	if err := a.store.SaveProjects(list); err != nil {
		return p, err
	}
	a.emit("asg:projects", nil)
	return p, nil
}

// DeleteProject removes the project; its chats stay, moved out of it.
func (a *App) DeleteProject(id string) error {
	var keep []Project
	for _, p := range a.store.Projects() {
		if p.ID != id {
			keep = append(keep, p)
		}
	}
	for _, m := range a.store.ListSessions() {
		if m.ProjectID == id {
			_ = a.MoveSessionToProject(m.ID, "")
		}
	}
	err := a.store.SaveProjects(keep)
	a.emit("asg:projects", nil)
	return err
}

// ---------- artifacts ----------

func (a *App) ListArtifacts() []Artifact { return a.store.Artifacts() }

type ArtifactView struct {
	Meta Artifact `json:"meta"`
	Path string   `json:"path"`
	Text string   `json:"text,omitempty"`
	HTML string   `json:"html,omitempty"` // rendered markdown or highlighted code
	Data string   `json:"data,omitempty"` // data URL for images and PDFs
}

func (a *App) GetArtifact(id string) (ArtifactView, error) {
	art, err := a.store.Artifact(id)
	if err != nil {
		return ArtifactView{}, err
	}
	p := a.store.ArtifactFile(art)
	v := ArtifactView{Meta: *art, Path: p}
	data, err := os.ReadFile(p)
	if err != nil {
		return v, err
	}
	switch {
	case art.Kind == "image" || art.Kind == "pdf":
		mt := map[string]string{"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif",
			"webp": "image/webp", "bmp": "image/bmp", "ico": "image/x-icon", "pdf": "application/pdf"}[art.Ext]
		if mt == "" {
			mt = "application/octet-stream"
		}
		v.Data = "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(data)
	case IsPreviewText(art.Kind):
		v.Text = clipStr(string(data), 2<<20)
		switch art.Kind {
		case "markdown":
			v.HTML = RenderMarkdown(v.Text)
		case "code", "data", "text":
			v.HTML = highlight(clipStr(v.Text, 400000), art.Ext)
		}
	}
	return v, nil
}

func (a *App) DeleteArtifact(id string) error {
	err := a.store.DeleteArtifact(id)
	a.emit("asg:artifacts", nil)
	return err
}

func (a *App) OpenArtifact(id string) error {
	art, err := a.store.Artifact(id)
	if err != nil {
		return err
	}
	return openPath(a.store.ArtifactFile(art))
}

func (a *App) RevealArtifact(id string) error {
	art, err := a.store.Artifact(id)
	if err != nil {
		return err
	}
	return revealPath(a.store.ArtifactFile(art))
}

// ExportArtifact saves a copy wherever the user picks.
func (a *App) ExportArtifact(id string) (string, error) {
	art, err := a.store.Artifact(id)
	if err != nil {
		return "", err
	}
	dst, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{Title: "Save artifact", DefaultFilename: art.Title})
	if err != nil || dst == "" {
		return "", err
	}
	return dst, copyFile(a.store.ArtifactFile(art), dst)
}

// ---------- files & folders ----------

func (a *App) PickFolder(title string) (string, error) {
	if a.pickHook != nil {
		return a.pickHook(title)
	}
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
}

func (a *App) OpenPath(p string) error   { return openPath(p) }
func (a *App) RevealPath(p string) error { return revealPath(p) }
func (a *App) OpenURL(u string) {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "mailto:") {
		wruntime.BrowserOpenURL(a.ctx, u)
	}
}

func openPath(p string) error {
	if _, err := os.Stat(p); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", p)
	case "darwin":
		cmd = exec.Command("open", p)
	default:
		cmd = exec.Command("xdg-open", p)
	}
	return proc.Hide(cmd).Start()
}

func revealPath(p string) error {
	if _, err := os.Stat(p); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", "/select,", p)
	case "darwin":
		cmd = exec.Command("open", "-R", p)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(p))
	}
	return proc.Hide(cmd).Start()
}

func gitBranch(dir string) string {
	if dir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := proc.Hide(exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD")).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ---------- settings ----------

func (a *App) GetSettings() Boot { return a.Bootstrap() }

func (a *App) SetTheme(theme string) error {
	switch theme {
	case "system", "light", "dark":
	default:
		return fmt.Errorf("unknown theme %q", theme)
	}
	a.setMu.Lock()
	a.settings.Theme = theme
	s := a.settings
	a.setMu.Unlock()
	return saveSettings(s)
}

func (a *App) anyRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ls := range a.live {
		ls.mu.Lock()
		r := ls.running
		ls.mu.Unlock()
		if r {
			return true
		}
	}
	return false
}

// ChangeDataDir asks for a folder and makes it the data folder. With move
// set, existing sessions, artifacts and sandboxes are moved there;
// otherwise the app starts using the folder as is (e.g. a drive that
// already holds ASG data).
func (a *App) ChangeDataDir(move bool) (string, error) {
	if a.anyRunning() {
		return "", errors.New("wait for running sessions to finish first")
	}
	dst, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Choose where ASG keeps sessions, artifacts and sandboxes", CanCreateDirectories: true})
	if err != nil || dst == "" {
		return "", err
	}
	if move {
		if err := a.store.MoveData(dst); err != nil {
			return "", err
		}
	} else {
		a.store.setDir(dst)
	}
	if err := a.store.Init(); err != nil {
		return "", err
	}
	a.mu.Lock()
	a.live = map[string]*liveSession{}
	a.mu.Unlock()
	a.setMu.Lock()
	a.settings.DataDir = dst
	s := a.settings
	a.setMu.Unlock()
	if err := saveSettings(s); err != nil {
		return dst, err
	}
	a.emit("asg:sessions", nil)
	a.emit("asg:projects", nil)
	return dst, nil
}

// ResetDataDir goes back to the default folder (without moving anything).
func (a *App) ResetDataDir() (string, error) {
	if a.anyRunning() {
		return "", errors.New("wait for running sessions to finish first")
	}
	d := DefaultDataDir()
	a.store.setDir(d)
	if err := a.store.Init(); err != nil {
		return "", err
	}
	a.mu.Lock()
	a.live = map[string]*liveSession{}
	a.mu.Unlock()
	a.setMu.Lock()
	a.settings.DataDir = d
	s := a.settings
	a.setMu.Unlock()
	a.emit("asg:sessions", nil)
	return d, saveSettings(s)
}

type StorageInfo struct {
	DataDir   string `json:"data_dir"`
	Sessions  int    `json:"sessions"`
	Artifacts int    `json:"artifacts"`
	Bytes     int64  `json:"bytes"`
	Config    string `json:"config"`
	Default   string `json:"default"`
	IsDefault bool   `json:"is_default"`
}

func (a *App) Storage() StorageInfo {
	dir := a.store.Dir()
	var size int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return StorageInfo{DataDir: dir, Sessions: len(a.store.ListSessions()), Artifacts: len(a.store.Artifacts()),
		Bytes: size, Config: a.core.Config.Path(), Default: DefaultDataDir(), IsDefault: samePath(dir, DefaultDataDir())}
}

// ---------- helpers ----------

// LogJS records a frontend error in desktop.log inside the data folder.
func (a *App) LogJS(msg string) {
	f, err := os.OpenFile(filepath.Join(a.store.Dir(), "desktop.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), clipStr(msg, 2000))
}

func autoTitle(s string) string {
	s = oneLine(s, 200)
	if len([]rune(s)) <= 52 {
		return s
	}
	r := []rune(s)[:52]
	cut := string(r)
	if i := strings.LastIndex(cut, " "); i > 24 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-") + "…"
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.NewReplacer("**", "", "`", "", "# ", "").Replace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func clipStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n… (clipped)"
}
