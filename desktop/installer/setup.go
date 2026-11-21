package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
)

// Setup is bound to the wizard's UI (window.go.main.Setup).
type Setup struct {
	ctx       context.Context
	uninstall bool
	archive   []byte

	mu         sync.Mutex
	busy       bool
	appPath    string // what Launch starts after an install
	selfDelete string // install folder to clean up after the uninstaller exits
}

func NewSetup(uninstall bool) *Setup {
	return &Setup{uninstall: uninstall, archive: appArchive()}
}

func (s *Setup) startup(ctx context.Context) { s.ctx = ctx }

func (s *Setup) shutdown(context.Context) {
	s.mu.Lock()
	dir := s.selfDelete
	s.mu.Unlock()
	if dir != "" {
		scheduleSelfDelete(dir)
	}
}

// ---------- what the UI starts from ----------

// Paths are the three folders the wizard asks about.
type Paths struct {
	InstallDir string `json:"install_dir"`
	DataDir    string `json:"data_dir"`
	MemoryDir  string `json:"memory_dir"`
}

// Features says which options make sense on this OS.
type Features struct {
	DesktopShortcut bool   `json:"desktop_shortcut"`
	MenuShortcut    bool   `json:"menu_shortcut"`
	MenuLabel       string `json:"menu_label"`
	LaunchAtLogin   bool   `json:"launch_at_login"`
	AddToPath       bool   `json:"add_to_path"`
}

// ProviderInput is one model provider as the wizard edits it. An empty
// APIKey keeps the key already saved for that provider (HasKey).
type ProviderInput struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
	HasKey  bool   `json:"has_key"`
}

type Info struct {
	Version      string          `json:"version"`
	OS           string          `json:"os"`
	Arch         string          `json:"arch"`
	Mode         string          `json:"mode"` // install | uninstall
	Preview      bool            `json:"preview"`
	PayloadBytes int64           `json:"payload_bytes"`
	Defaults     Paths           `json:"defaults"`
	Current      Paths           `json:"current"`
	Features     Features        `json:"features"`
	Existing     *Record         `json:"existing,omitempty"`
	Providers    []ProviderInput `json:"providers"`
	Active       string          `json:"active"`
	Theme        string          `json:"theme"`
	ConfigPath   string          `json:"config_path"`
	HomeDir      string          `json:"home_dir"`
}

func (s *Setup) Info() Info {
	cfg, fresh := readConfig()
	desk := readDesktopSettings()
	defaults := Paths{
		InstallDir: defaultInstallDir(),
		DataDir:    filepath.Join(config.Dir(), "desktop"),
		MemoryDir:  filepath.Join(config.Dir(), "memory"),
	}
	cur := defaults
	if d, _ := desk["data_dir"].(string); d != "" {
		cur.DataDir = d
	}
	if cfg.MemoryDir != "" {
		cur.MemoryDir = cfg.MemoryDir
	}
	rec := loadRecord()
	if rec != nil && rec.InstallDir != "" {
		cur.InstallDir = rec.InstallDir
	}
	theme, _ := desk["theme"].(string)
	if theme == "" {
		theme = "system"
	}
	info := Info{
		Version: version, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Mode: "install", Preview: s.archive == nil,
		Defaults: defaults, Current: cur, Features: features(), Existing: rec,
		Providers: providerInputs(cfg, fresh), Active: cfg.ActiveProvider, Theme: theme,
		ConfigPath: filepath.Join(config.Dir(), "config.json"),
	}
	info.HomeDir, _ = os.UserHomeDir()
	if s.uninstall {
		info.Mode = "uninstall"
	}
	if s.archive != nil {
		info.PayloadBytes, _, _ = archiveInfo(s.archive)
	}
	return info
}

// readConfig reads config.json without creating it (the wizard only writes
// when the user presses Install). fresh is true when there is none yet.
func readConfig() (cfg config.Config, fresh bool) {
	data, err := os.ReadFile(filepath.Join(config.Dir(), "config.json"))
	if err != nil {
		return *config.Default(), true
	}
	c := config.Default()
	c.Providers = map[string]*config.ProviderConfig{}
	if json.Unmarshal(data, c) != nil || len(c.Providers) == 0 {
		return *config.Default(), true
	}
	return *c, false
}

// providerInputs lists the three standard providers plus any custom ones
// from the config. On a fresh machine only local Ollama starts switched on.
func providerInputs(cfg config.Config, fresh bool) []ProviderInput {
	std := config.Default().Providers
	names := []string{"ollama", "anthropic", "openai"}
	for _, n := range cfg.ProviderNames() {
		if std[n] == nil {
			names = append(names, n)
		}
	}
	out := make([]ProviderInput, 0, len(names))
	for _, n := range names {
		p, have := cfg.Providers[n]
		if !have {
			p = std[n]
		}
		in := ProviderInput{Name: n, Type: p.Type, BaseURL: p.BaseURL, Model: p.Model, HasKey: have && p.APIKey != ""}
		in.Enabled = have && (!fresh || n == "ollama")
		out = append(out, in)
	}
	return out
}

func readDesktopSettings() map[string]any {
	m := map[string]any{}
	if data, err := os.ReadFile(filepath.Join(config.Dir(), "desktop.json")); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

// ---------- small helpers for the UI ----------

// PickFolder opens the system folder picker, starting at start (or its
// nearest existing parent).
func (s *Setup) PickFolder(title, start string) (string, error) {
	for start != "" {
		if st, err := os.Stat(start); err == nil && st.IsDir() {
			break
		}
		parent := filepath.Dir(start)
		if parent == start {
			start = ""
			break
		}
		start = parent
	}
	return wruntime.OpenDirectoryDialog(s.ctx, wruntime.OpenDialogOptions{
		Title: title, DefaultDirectory: start, CanCreateDirectories: true})
}

// CheckPaths validates the folders; the map holds a message per bad field.
func (s *Setup) CheckPaths(p Paths) map[string]string {
	return checkPaths(p)
}

func checkPaths(p Paths) map[string]string {
	bad := map[string]string{}
	check := func(key, dir string) {
		if strings.TrimSpace(dir) == "" {
			bad[key] = "Pick a folder."
			return
		}
		if !filepath.IsAbs(dir) {
			bad[key] = "Use a full path, like " + filepath.Join(defaultHome(), "ASG") + "."
			return
		}
		if err := writable(dir); err != nil {
			bad[key] = "ASG can't write here (" + err.Error() + "). Pick a folder in your user profile, or run setup as administrator."
		}
	}
	check("install_dir", p.InstallDir)
	check("data_dir", p.DataDir)
	check("memory_dir", p.MemoryDir)
	if bad["install_dir"] == "" && p.InstallDir != "" {
		if st, err := os.Stat(p.InstallDir); err == nil && !st.IsDir() {
			bad["install_dir"] = "That's a file, not a folder."
		}
	}
	return bad
}

func defaultHome() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return string(filepath.Separator)
	}
	return h
}

// writable tests the folder, or its nearest existing parent, by creating
// and removing a temporary file. Nothing is left behind.
func writable(dir string) error {
	d := filepath.Clean(dir)
	for {
		if st, err := os.Stat(d); err == nil {
			if !st.IsDir() {
				return errors.New("not a folder")
			}
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			return errors.New("the drive doesn't exist")
		}
		d = parent
	}
	f, err := os.CreateTemp(d, ".asg-setup-*")
	if err != nil {
		return errors.New("permission denied")
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// TestResult is what TestProvider found.
type TestResult struct {
	OK     bool     `json:"ok"`
	Models []string `json:"models,omitempty"`
	Error  string   `json:"error,omitempty"`
}

// TestProvider connects to a provider and lists its models.
func (s *Setup) TestProvider(p ProviderInput) TestResult {
	pc := &config.ProviderConfig{Type: p.Type, BaseURL: strings.TrimSpace(p.BaseURL), APIKey: strings.TrimSpace(p.APIKey), Model: p.Model}
	if pc.APIKey == "" {
		if cfg, _ := readConfig(); cfg.Providers[p.Name] != nil {
			pc.APIKey = cfg.Providers[p.Name].APIKey
		}
	}
	if pc.Type == "anthropic" && pc.APIKey == "" {
		return TestResult{Error: "Add an API key first."}
	}
	prov, err := llm.New(pc)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	models, err := prov.ListModels(ctx)
	if err != nil {
		msg := err.Error()
		if p.Type == "ollama" && (strings.Contains(msg, "refused") || strings.Contains(msg, "actively refused")) {
			msg = "Ollama isn't running at " + pc.BaseURL + ". Start it (or install it from ollama.com) — you can still finish setup."
		}
		return TestResult{Error: msg}
	}
	sort.Strings(models)
	return TestResult{OK: true, Models: models}
}

// OpenURL opens a link (docs, provider key pages) in the browser.
func (s *Setup) OpenURL(u string) {
	if strings.HasPrefix(u, "https://") {
		wruntime.BrowserOpenURL(s.ctx, u)
	}
}

// ---------- providers → config ----------

var reProviderName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// applyProviders writes the wizard's providers into c. Switched-off
// providers are removed, empty keys keep the saved key, and active is
// used when it is switched on (otherwise the first one that is).
func applyProviders(c *config.Config, in []ProviderInput, active string) error {
	if c.Providers == nil {
		c.Providers = map[string]*config.ProviderConfig{}
	}
	var enabled []string
	for _, p := range in {
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if !p.Enabled {
			delete(c.Providers, name)
			continue
		}
		if !reProviderName.MatchString(name) {
			return fmt.Errorf("provider name %q: use lowercase letters, digits, - or _", p.Name)
		}
		switch p.Type {
		case "ollama", "openai", "anthropic":
		default:
			return fmt.Errorf("provider %s: unknown type %q", name, p.Type)
		}
		base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			return fmt.Errorf("provider %s: the address must start with http:// or https://", name)
		}
		if strings.TrimSpace(p.Model) == "" {
			return fmt.Errorf("provider %s: pick a model", name)
		}
		next := &config.ProviderConfig{Type: p.Type, BaseURL: base, Model: strings.TrimSpace(p.Model), APIKey: strings.TrimSpace(p.APIKey)}
		if old := c.Providers[name]; old != nil {
			next.NumCtx = old.NumCtx
			if next.APIKey == "" {
				next.APIKey = old.APIKey
			}
		}
		c.Providers[name] = next
		enabled = append(enabled, name)
	}
	if len(enabled) == 0 {
		return errors.New("switch on at least one provider")
	}
	c.ActiveProvider = enabled[0]
	for _, n := range enabled {
		if n == strings.ToLower(active) {
			c.ActiveProvider = n
		}
	}
	return nil
}

// writeDesktopSettings merges keys into ~/.asg/desktop.json, keeping any the
// desktop app added that the wizard doesn't know about.
func writeDesktopSettings(set map[string]any) error {
	m := readDesktopSettings()
	for k, v := range set {
		m[k] = v
	}
	p := filepath.Join(config.Dir(), "desktop.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}
