package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
)

// Session kinds. Each gets its own icon and colour in the UI.
const (
	KindChat      = "chat"      // general chat in a private sandbox folder
	KindCode      = "code"      // coding session bound to a folder
	KindArtifact  = "artifact"  // made to produce a file (page, doc, diagram)
	KindProject   = "project"   // chat inside a project
	KindScheduled = "scheduled" // started by a schedule
)

// Settings live in the harness home (not the data folder) so the data
// folder itself can move, e.g. to an external drive.
type Settings struct {
	DataDir string `json:"data_dir"`
	Theme   string `json:"theme"` // system | light | dark
	// QuitOnClose: closing the window quits instead of keeping ASG in the tray.
	QuitOnClose bool `json:"quit_on_close,omitempty"`
	// SkipUpdateCheck turns off the automatic check for new versions.
	SkipUpdateCheck bool `json:"skip_update_check,omitempty"`
	// SkippedVersion is a release the user chose not to be reminded about.
	SkippedVersion string `json:"skipped_version,omitempty"`
}

func settingsPath() string { return filepath.Join(config.Dir(), "desktop.json") }

// DefaultDataDir is where sessions live until the user picks another folder.
func DefaultDataDir() string { return filepath.Join(config.Dir(), "desktop") }

func loadSettings() Settings {
	s := Settings{DataDir: DefaultDataDir(), Theme: "system"}
	if data, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s.DataDir == "" {
		s.DataDir = DefaultDataDir()
	}
	if s.Theme == "" {
		s.Theme = "system"
	}
	return s
}

func saveSettings(s Settings) error {
	return writeJSONFile(settingsPath(), s)
}

// SessionMeta is the small part of a session used for listings.
type SessionMeta struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Kind       string `json:"kind"`
	ProjectID  string `json:"project_id,omitempty"`
	ScheduleID string `json:"schedule_id,omitempty"`
	// Workdir is the folder the agent works in. Empty means the session's
	// own sandbox inside the data folder (kept relative so the data folder
	// can move).
	Workdir   string    `json:"workdir,omitempty"`
	Mode      string    `json:"mode"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	Pinned    bool      `json:"pinned,omitempty"`
	Messages  int       `json:"messages"`
	Preview   string    `json:"preview,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Model     string    `json:"model,omitempty"`
	Artifacts int       `json:"artifacts,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

// SessionFile is everything needed to resume a session.
type SessionFile struct {
	SessionMeta
	History  []llm.Message `json:"history"`
	Plan     string        `json:"plan,omitempty"`
	PlanGoal string        `json:"plan_goal,omitempty"`
	Usage    llm.Usage     `json:"usage"`
	WorkDir  string        `json:"work_dir,omitempty"` // .asg/work/<task> folder of a build
}

// Project groups chats under shared instructions and, optionally, a folder.
type Project struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description,omitempty"`
	Instructions string    `json:"instructions,omitempty"`
	Folder       string    `json:"folder,omitempty"`
	Color        string    `json:"color,omitempty"`
	Created      time.Time `json:"created"`
	Updated      time.Time `json:"updated"`
}

// Artifact is a file a session produced, snapshotted into the data folder
// so it survives even if the original is changed or deleted.
type Artifact struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	File         string    `json:"file"` // name inside the artifact folder
	Kind         string    `json:"kind"` // html, markdown, image, svg, code, document, data, pdf, text
	Ext          string    `json:"ext"`
	SessionID    string    `json:"session_id"`
	SessionTitle string    `json:"session_title"`
	SessionKind  string    `json:"session_kind"`
	ProjectID    string    `json:"project_id,omitempty"`
	SourcePath   string    `json:"source_path"`
	Created      time.Time `json:"created"`
	Updated      time.Time `json:"updated"`
	Versions     int       `json:"versions"`
	Size         int64     `json:"size"`
}

// Store reads and writes everything under the data folder.
type Store struct {
	mu  sync.Mutex
	dir string
}

func (s *Store) Dir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dir
}

func (s *Store) setDir(d string) {
	s.mu.Lock()
	s.dir = d
	s.mu.Unlock()
}

func (s *Store) path(parts ...string) string {
	return filepath.Join(append([]string{s.Dir()}, parts...)...)
}

// Init creates the folder layout.
func (s *Store) Init() error {
	for _, d := range []string{"sessions", "artifacts", "sandbox", "projects"} {
		if err := os.MkdirAll(s.path(d), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SandboxDir(sessionID string) string { return s.path("sandbox", sessionID) }
func (s *Store) ProjectFilesDir(id string) string   { return s.path("projects", id, "files") }

// --- sessions ---

func (s *Store) SaveSession(f *SessionFile) error {
	f.Messages = countTurns(f.History)
	if err := writeJSONFile(s.path("sessions", f.ID+".json"), f); err != nil {
		return err
	}
	return writeJSONFile(s.path("sessions", f.ID+".meta.json"), f.SessionMeta)
}

func (s *Store) LoadSession(id string) (*SessionFile, error) {
	if !validID(id) {
		return nil, errors.New("bad session id")
	}
	var f SessionFile
	if err := readJSONFile(s.path("sessions", id+".json"), &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *Store) DeleteSession(id string) error {
	if !validID(id) {
		return errors.New("bad session id")
	}
	_ = os.Remove(s.path("sessions", id+".meta.json"))
	err := os.Remove(s.path("sessions", id+".json"))
	_ = os.RemoveAll(s.SandboxDir(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// ListSessions returns every session's metadata, newest first, pinned on top.
func (s *Store) ListSessions() []SessionMeta {
	matches, _ := filepath.Glob(s.path("sessions", "*.meta.json"))
	out := make([]SessionMeta, 0, len(matches))
	for _, m := range matches {
		var meta SessionMeta
		if readJSONFile(m, &meta) == nil && meta.ID != "" {
			out = append(out, meta)
		}
	}
	sortSessions(out)
	return out
}

func sortSessions(list []SessionMeta) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Pinned != list[j].Pinned {
			return list[i].Pinned
		}
		return list[i].Updated.After(list[j].Updated)
	})
}

// countTurns counts user and assistant text messages (not tool traffic).
func countTurns(h []llm.Message) int {
	n := 0
	for _, m := range h {
		if (m.Role == llm.RoleUser || m.Role == llm.RoleAssistant) && strings.TrimSpace(m.Content) != "" && !isToolImageNote(m) {
			n++
		}
	}
	return n
}

// isToolImageNote spots the synthetic user message the agent adds to show
// the model images that tools returned.
func isToolImageNote(m llm.Message) bool {
	return m.Role == llm.RoleUser && strings.HasPrefix(m.Content, "[") && strings.Contains(m.Content, "image(s) returned by the tool calls")
}

// --- projects ---

func (s *Store) Projects() []Project {
	var list []Project
	_ = readJSONFile(s.path("projects.json"), &list)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Updated.After(list[j].Updated) })
	return list
}

func (s *Store) SaveProjects(list []Project) error {
	return writeJSONFile(s.path("projects.json"), list)
}

// --- schedules ---

func (s *Store) Schedules() []Schedule {
	var list []Schedule
	_ = readJSONFile(s.path("schedules.json"), &list)
	return list
}

func (s *Store) SaveSchedules(list []Schedule) error {
	return writeJSONFile(s.path("schedules.json"), list)
}

// --- artifacts ---

func (s *Store) Artifacts() []Artifact {
	matches, _ := filepath.Glob(s.path("artifacts", "*", "artifact.json"))
	out := make([]Artifact, 0, len(matches))
	for _, m := range matches {
		var a Artifact
		if readJSONFile(m, &a) == nil && a.ID != "" {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

func (s *Store) Artifact(id string) (*Artifact, error) {
	if !validID(id) {
		return nil, errors.New("bad artifact id")
	}
	var a Artifact
	if err := readJSONFile(s.path("artifacts", id, "artifact.json"), &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) ArtifactFile(a *Artifact) string { return s.path("artifacts", a.ID, a.File) }

func (s *Store) DeleteArtifact(id string) error {
	if !validID(id) {
		return errors.New("bad artifact id")
	}
	return os.RemoveAll(s.path("artifacts", id))
}

// Capture snapshots src as an artifact of the session. Writing the same
// file again in the same session adds a version instead of a new artifact.
func (s *Store) Capture(sess SessionMeta, src string) (*Artifact, error) {
	info, err := os.Stat(src)
	if err != nil || info.IsDir() {
		return nil, fmt.Errorf("capture %s: not a file", src)
	}
	if info.Size() > 50<<20 {
		return nil, errors.New("file too large to keep as an artifact")
	}
	var a *Artifact
	for _, old := range s.Artifacts() {
		if old.SessionID == sess.ID && samePath(old.SourcePath, src) {
			old := old
			a = &old
			break
		}
	}
	now := time.Now()
	if a == nil {
		ext := strings.ToLower(filepath.Ext(src))
		a = &Artifact{
			ID: newID("a"), Title: filepath.Base(src), File: safeFileName(filepath.Base(src)),
			Kind: ArtifactKind(ext), Ext: strings.TrimPrefix(ext, "."), SessionID: sess.ID,
			ProjectID: sess.ProjectID, SourcePath: src, Created: now,
		}
	}
	a.SessionTitle, a.SessionKind = sess.Title, sess.Kind
	a.Updated, a.Size = now, info.Size()
	a.Versions++
	dir := s.path("artifacts", a.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := copyFile(src, filepath.Join(dir, a.File)); err != nil {
		return nil, err
	}
	return a, writeJSONFile(filepath.Join(dir, "artifact.json"), a)
}

// ArtifactKind groups a file extension for preview and icons.
func ArtifactKind(ext string) string {
	switch strings.TrimPrefix(strings.ToLower(ext), ".") {
	case "html", "htm":
		return "html"
	case "md", "markdown":
		return "markdown"
	case "svg":
		return "svg"
	case "png", "jpg", "jpeg", "gif", "webp", "bmp", "ico":
		return "image"
	case "pdf":
		return "pdf"
	case "docx", "pptx", "xlsx", "doc", "ppt", "xls", "odt":
		return "document"
	case "csv", "tsv", "json", "yaml", "yml", "toml", "xml":
		return "data"
	case "go", "rs", "py", "js", "ts", "tsx", "jsx", "css", "c", "h", "cpp", "java", "kt", "cs", "rb", "php",
		"sh", "ps1", "bat", "sql", "lua", "swift", "dart", "vue", "svelte", "zig":
		return "code"
	default:
		return "text"
	}
}

// IsPreviewText reports whether an artifact kind is shown as text.
func IsPreviewText(kind string) bool {
	switch kind {
	case "html", "markdown", "svg", "code", "data", "text":
		return true
	}
	return false
}

// --- moving the data folder ---

// MoveData copies everything from the current data folder into dst, then
// removes the old copy. dst must be empty or not exist.
func (s *Store) MoveData(dst string) error {
	src := s.Dir()
	if samePath(src, dst) {
		return nil
	}
	if rel, err := filepath.Rel(src, dst); err == nil && !strings.HasPrefix(rel, "..") {
		return errors.New("the new folder can't be inside the current one")
	}
	if entries, err := os.ReadDir(dst); err == nil && len(entries) > 0 {
		return errors.New("pick an empty folder (or create a new one) so nothing is overwritten")
	}
	if err := copyTree(src, dst); err != nil {
		return fmt.Errorf("copy to %s: %w", dst, err)
	}
	s.setDir(dst)
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("copied, but could not remove the old folder %s: %w", src, err)
	}
	return nil
}

// --- helpers ---

func newID(prefix string) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return prefix + strconv.FormatInt(time.Now().UnixMilli(), 36) + hex.EncodeToString(b)
}

// validID keeps ids from escaping their folders.
func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func safeFileName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." || name == "artifact.json" {
		name = "file_" + name
	}
	return name
}

// within reports whether p (absolute, or relative to the working
// directory) is inside dir, after resolving "..".
func within(dir, p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dir, abs)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func writeJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if strings.HasSuffix(p, ".tmp") {
			return nil
		}
		return copyFile(p, target)
	})
}
