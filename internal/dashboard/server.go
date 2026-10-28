// Package dashboard serves a local browser UI for chatting, editing config
// and managing skills. It binds to 127.0.0.1 and every API call must carry
// a per-process random token, because the agent behind it can run commands.
package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/audio"
	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
)

//go:embed static/index.html
var static embed.FS

const mask = "••••"

type Server struct {
	app   *agent.App
	token string
	port  int

	mu       sync.Mutex
	sessions map[string]*webSession
}

type webSession struct {
	sess      *agent.Session
	mu        sync.Mutex
	cancel    context.CancelFunc
	approvals map[string]chan agent.Decision
}

var (
	running   *Server
	runningMu sync.Mutex
	runURL    string
)

// Ensure starts the dashboard in the background once and returns its URL.
func Ensure(app *agent.App) (string, error) {
	runningMu.Lock()
	defer runningMu.Unlock()
	if running != nil {
		return runURL, nil
	}
	s := newServer(app)
	ln, err := s.listen()
	if err != nil {
		return "", err
	}
	go http.Serve(ln, s.handler())
	running = s
	runURL = s.url()
	return runURL, nil
}

// Serve runs the dashboard in the foreground.
func Serve(app *agent.App, open bool) error {
	s := newServer(app)
	ln, err := s.listen()
	if err != nil {
		return err
	}
	fmt.Printf("ASG dashboard: %s\n(Ctrl+C to stop)\n", s.url())
	if open {
		OpenBrowser(s.url())
	}
	return http.Serve(ln, s.handler())
}

func newServer(app *agent.App) *Server {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return &Server{app: app, token: hex.EncodeToString(b), sessions: map[string]*webSession{}}
}

func (s *Server) listen() (net.Listener, error) {
	port := s.app.Config.Get().DashboardPort
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		// Port busy: fall back to any free port.
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	return ln, nil
}

func (s *Server) url() string {
	return fmt.Sprintf("http://127.0.0.1:%d/#token=%s", s.port, s.token)
}

// OpenBrowser opens url in the default browser.
func OpenBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		data, _ := static.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	})
	api := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.guard(h)) }
	api("GET /api/state", s.state)
	api("PUT /api/config", s.putConfig)
	api("GET /api/models", s.models)
	api("POST /api/chat", s.chat)
	api("POST /api/approve", s.approve)
	api("POST /api/stop", s.stop)
	api("POST /api/clear", s.clear)
	api("GET /api/history", s.history)
	api("POST /api/skills/import", s.importSkill)
	api("POST /api/skills", s.createSkill)
	api("GET /api/skills/{name}", s.getSkill)
	api("DELETE /api/skills/{name}", s.deleteSkill)
	api("POST /api/tts", s.tts)
	api("GET /api/voice", s.voice)
	api("GET /api/speech-models", s.speechModels)
	api("GET /api/voices", s.voices)
	return mux
}

// guard enforces the token and rejects foreign Host headers (DNS rebinding).
func (s *Server) guard(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		tok := r.Header.Get("X-ASG-Token")
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) != 1 {
			http.Error(w, "bad token — reopen the dashboard from the terminal", http.StatusUnauthorized)
			return
		}
		h(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func maskKey(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return mask
	}
	return mask + k[len(k)-4:]
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Config.Get()
	for _, p := range cfg.Providers {
		p.APIKey = maskKey(p.APIKey)
	}
	type toolInfo struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		NeedsApproval bool   `json:"needs_approval"`
	}
	var tools []toolInfo
	for _, t := range s.app.Tools.All() {
		tools = append(tools, toolInfo{t.Spec.Name, t.Spec.Description, t.NeedsApproval})
	}
	writeJSON(w, map[string]any{
		"config":      cfg,
		"config_path": s.app.Config.Path(),
		"skills":      s.app.Skills.List(),
		"skills_dir":  s.app.Skills.InstallDir(),
		"tools":       tools,
		"voices":      append([]string{}, audio.SystemVoices()...),
	})
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var next config.Config
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		fail(w, 400, err)
		return
	}
	if len(next.Providers) == 0 {
		fail(w, 400, errors.New("at least one provider is required"))
		return
	}
	for name, p := range next.Providers {
		switch p.Type {
		case "ollama", "openai", "anthropic":
		default:
			fail(w, 400, fmt.Errorf("provider %s: unknown type %q", name, p.Type))
			return
		}
	}
	err := s.app.Config.Update(func(cur *config.Config) error {
		for name, p := range next.Providers {
			if strings.HasPrefix(p.APIKey, mask) {
				if old := cur.Providers[name]; old != nil {
					p.APIKey = old.APIKey
				} else {
					p.APIKey = ""
				}
			}
		}
		*cur = next
		return nil
	})
	if err != nil {
		fail(w, 500, err)
		return
	}
	s.state(w, r)
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.Config.Get()
	pc := cfg.Providers[r.URL.Query().Get("provider")]
	if pc == nil {
		fail(w, 404, errors.New("unknown provider (save first)"))
		return
	}
	p, err := llm.New(pc)
	if err != nil {
		fail(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	models, err := p.ListModels(ctx)
	if err != nil {
		fail(w, 502, err)
		return
	}
	writeJSON(w, map[string]any{"models": models})
}

func (s *Server) session(id string) *webSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		id = "default"
	}
	ws := s.sessions[id]
	if ws == nil {
		ws = &webSession{sess: s.app.NewSession(), approvals: map[string]chan agent.Decision{}}
		s.sessions[id] = ws
	}
	return ws
}

type chatRequest struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
	Skill     string `json:"skill"`
	Images    []struct {
		MediaType string `json:"media_type"`
		Data      string `json:"data"` // base64
	} `json:"images"`
}

// sseEvents streams agent progress to the browser as server-sent events.
type sseEvents struct {
	w  http.ResponseWriter
	f  http.Flusher
	mu sync.Mutex
	ws *webSession
	n  int
}

func (e *sseEvents) send(v map[string]any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	data, _ := json.Marshal(v)
	fmt.Fprintf(e.w, "data: %s\n\n", data)
	e.f.Flush()
}

func (e *sseEvents) Text(d string) { e.send(map[string]any{"type": "text", "text": d}) }
func (e *sseEvents) Notice(m string) {
	e.send(map[string]any{"type": "notice", "text": m})
}
func (e *sseEvents) ToolStart(c llm.ToolCall, summary string) {
	e.send(map[string]any{"type": "tool_start", "id": c.ID, "name": c.Name, "summary": summary, "args": c.Args})
}
func (e *sseEvents) ToolEnd(c llm.ToolCall, result string, isErr bool) {
	e.send(map[string]any{"type": "tool_end", "id": c.ID, "name": c.Name, "result": result, "error": isErr})
}
func (e *sseEvents) Approve(ctx context.Context, c llm.ToolCall, summary string) agent.Decision {
	e.mu.Lock()
	e.n++
	id := fmt.Sprintf("a%d-%d", time.Now().UnixNano(), e.n)
	e.mu.Unlock()
	ch := make(chan agent.Decision, 1)
	e.ws.mu.Lock()
	e.ws.approvals[id] = ch
	e.ws.mu.Unlock()
	defer func() {
		e.ws.mu.Lock()
		delete(e.ws.approvals, id)
		e.ws.mu.Unlock()
	}()
	e.send(map[string]any{"type": "approval", "id": id, "name": c.Name, "summary": summary, "args": c.Args})
	select {
	case d := <-ch:
		return d
	case <-ctx.Done():
		return agent.Deny
	}
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	f, ok := w.(http.Flusher)
	if !ok {
		fail(w, 500, errors.New("streaming unsupported"))
		return
	}
	ws := s.session(req.SessionID)
	msg := llm.Message{Content: req.Text}
	for _, img := range req.Images {
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			fail(w, 400, fmt.Errorf("bad image data: %w", err))
			return
		}
		msg.Images = append(msg.Images, llm.Image{MediaType: img.MediaType, Data: data})
	}
	if req.Skill != "" {
		sk := s.app.Skills.Get(req.Skill)
		if sk == nil {
			fail(w, 404, fmt.Errorf("no skill %q", req.Skill))
			return
		}
		msg.Content = agent.SkillPrompt(sk) + "\n\n---\nUser request: " + req.Text
	}
	if strings.TrimSpace(msg.Content) == "" && len(msg.Images) > 0 {
		msg.Content = "Describe this image."
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	f.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	ws.mu.Lock()
	if ws.cancel != nil {
		ws.cancel()
	}
	ws.cancel = cancel
	ws.mu.Unlock()
	defer cancel()

	ev := &sseEvents{w: w, f: f, ws: ws}
	start := time.Now()
	reply, err := ws.sess.Send(ctx, msg, ev)
	if err != nil {
		ev.send(map[string]any{"type": "error", "text": err.Error()})
		return
	}
	u := ws.sess.Usage
	ev.send(map[string]any{"type": "done", "text": reply, "seconds": time.Since(start).Seconds(),
		"input_tokens": u.InputTokens, "output_tokens": u.OutputTokens})
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		ID        string `json:"id"`
		Decision  string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	ws := s.session(req.SessionID)
	ws.mu.Lock()
	ch := ws.approvals[req.ID]
	ws.mu.Unlock()
	if ch == nil {
		fail(w, 404, errors.New("no pending approval"))
		return
	}
	d := agent.Deny
	switch req.Decision {
	case "allow":
		d = agent.Allow
	case "always":
		d = agent.AlwaysAllow
	}
	ch <- d
	writeJSON(w, map[string]bool{"ok": true})
}

func sessionID(r *http.Request) string {
	var req struct {
		SessionID string `json:"session_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.SessionID == "" {
		req.SessionID = r.URL.Query().Get("session_id")
	}
	return req.SessionID
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	ws := s.session(sessionID(r))
	ws.mu.Lock()
	if ws.cancel != nil {
		ws.cancel()
	}
	ws.mu.Unlock()
	s.app.Speaker.Stop()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) clear(w http.ResponseWriter, r *http.Request) {
	s.session(sessionID(r)).sess.Clear()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	ws := s.session(r.URL.Query().Get("session_id"))
	type img struct {
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	}
	type msg struct {
		Role      string         `json:"role"`
		Content   string         `json:"content"`
		Images    []img          `json:"images,omitempty"`
		ToolCalls []llm.ToolCall `json:"tool_calls,omitempty"`
		ToolID    string         `json:"tool_call_id,omitempty"`
	}
	out := []msg{}
	for _, m := range ws.sess.History {
		mm := msg{Role: string(m.Role), Content: m.Content, ToolCalls: m.ToolCalls, ToolID: m.ToolCallID}
		for _, i := range m.Images {
			mm.Images = append(mm.Images, img{i.MediaType, base64.StdEncoding.EncodeToString(i.Data)})
		}
		out = append(out, mm)
	}
	writeJSON(w, map[string]any{"messages": out})
}

func (s *Server) importSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	got, err := s.app.Skills.Import(r.Context(), req.Source)
	if err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"imported": got, "skills": s.app.Skills.List()})
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	if _, err := s.app.Skills.Create(req.Name, req.Description, req.Body); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"skills": s.app.Skills.List()})
}

func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	sk := s.app.Skills.Get(r.PathValue("name"))
	if sk == nil {
		fail(w, 404, errors.New("not found"))
		return
	}
	writeJSON(w, map[string]any{"name": sk.Name, "description": sk.Description, "dir": sk.Dir, "body": sk.Body, "source": sk.Source})
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Skills.Remove(r.PathValue("name")); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, map[string]any{"skills": s.app.Skills.List()})
}

// voice reports which voice will read replies for the active chat model.
func (s *Server) voice(w http.ResponseWriter, r *http.Request) {
	opt := s.app.ResolveVoice(r.Context())
	writeJSON(w, map[string]any{"engine": opt.Engine, "model": opt.Model, "voice": opt.Voice, "label": opt.Label(), "why": opt.Why})
}

func (s *Server) speechModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.app.SpeechModels(r.Context(), r.URL.Query().Get("provider"))
	if err != nil {
		fail(w, 502, err)
		return
	}
	writeJSON(w, map[string]any{"models": models})
}

func (s *Server) voices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	voices, err := s.app.Voices(r.Context(), q.Get("provider"), q.Get("model"))
	if err != nil {
		fail(w, 502, err)
		return
	}
	writeJSON(w, map[string]any{"voices": voices})
}

// tts synthesizes one chunk of text with the resolved voice and returns
// the audio to the browser. For the system engine it answers with JSON so
// the browser uses its own speech synthesis (audio plays where you are).
func (s *Server) tts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, 400, err)
		return
	}
	opt := s.app.ResolveVoice(r.Context())
	if opt.Engine == "system" {
		writeJSON(w, map[string]any{"engine": "system", "voice": opt.Voice, "label": opt.Label()})
		return
	}
	data, ct, err := audio.Synthesize(r.Context(), audio.Clean(req.Text), opt)
	if err != nil {
		fail(w, 502, err)
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-ASG-Voice", opt.Label())
	w.Write(data)
}
