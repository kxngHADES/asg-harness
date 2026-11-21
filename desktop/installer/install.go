package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/kxngHADES/asg-harness/internal/config"
)

// Options is everything the wizard collected.
type Options struct {
	Paths
	DesktopShortcut bool            `json:"desktop_shortcut"`
	MenuShortcut    bool            `json:"menu_shortcut"`
	LaunchAtLogin   bool            `json:"launch_at_login"`
	AddToPath       bool            `json:"add_to_path"`
	Theme           string          `json:"theme"`
	Providers       []ProviderInput `json:"providers"`
	Active          string          `json:"active"`
}

// Record remembers what an install created so it can be removed again.
// It lives in ~/.asg/install.json.
type Record struct {
	Version    string    `json:"version"`
	InstallDir string    `json:"install_dir"`
	AppPath    string    `json:"app_path"`
	Files      []string  `json:"files"`               // top-level entries in InstallDir
	Shortcuts  []string  `json:"shortcuts,omitempty"` // desktop / menu entries, PATH links
	Autostart  string    `json:"autostart,omitempty"`
	Installed  time.Time `json:"installed"`
}

func recordPath() string { return filepath.Join(config.Dir(), "install.json") }

func loadRecord() *Record {
	data, err := os.ReadFile(recordPath())
	if err != nil {
		return nil
	}
	var r Record
	if json.Unmarshal(data, &r) != nil || r.InstallDir == "" {
		return nil
	}
	return &r
}

func saveRecord(r *Record) error {
	if err := os.MkdirAll(filepath.Dir(recordPath()), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(recordPath(), data, 0o644)
}

// Progress is sent to the UI as "setup:progress".
type Progress struct {
	Step   int     `json:"step"`
	Label  string  `json:"label"`
	Detail string  `json:"detail,omitempty"`
	Pct    float64 `json:"pct"` // 0..100 across the whole run
}

// Done is sent as "setup:done" when a run ends.
type Done struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	AppPath string `json:"app_path,omitempty"`
}

// InstallSteps are the stages shown while installing (the UI lists them).
var InstallSteps = []string{"Checking the folder", "Copying ASG", "Creating shortcuts", "Setting up storage", "Saving providers", "Finishing up"}

// UninstallSteps are the stages shown while uninstalling.
var UninstallSteps = []string{"Removing shortcuts", "Removing the app", "Cleaning up"}

func (s *Setup) Steps() map[string][]string {
	return map[string][]string{"install": InstallSteps, "uninstall": UninstallSteps}
}

func (s *Setup) claim() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return errors.New("setup is already running")
	}
	s.busy = true
	return nil
}

func (s *Setup) release() {
	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

func (s *Setup) emit(name string, v any) {
	if s.ctx != nil {
		wruntime.EventsEmit(s.ctx, name, v)
	}
}

// Install starts installing in the background. Progress arrives as
// "setup:progress" events and the result as "setup:done".
func (s *Setup) Install(o Options) error {
	if bad := checkPaths(o.Paths); len(bad) > 0 {
		for _, msg := range bad {
			return errors.New(msg)
		}
	}
	if err := s.claim(); err != nil {
		return err
	}
	go func() {
		defer s.release()
		var app string
		var err error
		if s.archive == nil {
			err = s.preview(InstallSteps)
		} else {
			app, err = s.install(o)
		}
		if err != nil {
			s.emit("setup:done", Done{Error: err.Error()})
			return
		}
		s.mu.Lock()
		s.appPath = app
		s.mu.Unlock()
		s.emit("setup:done", Done{OK: true, AppPath: app})
	}()
	return nil
}

// preview walks through the steps without touching anything (a build
// without a payload, used while working on the wizard itself).
func (s *Setup) preview(steps []string) error {
	for i, l := range steps {
		for k := 0; k <= 10; k++ {
			s.emit("setup:progress", Progress{Step: i, Label: l, Detail: "preview — nothing is changed",
				Pct: (float64(i) + float64(k)/10) / float64(len(steps)) * 100})
			time.Sleep(55 * time.Millisecond)
		}
	}
	return nil
}

func (s *Setup) install(o Options) (string, error) {
	n := float64(len(InstallSteps))
	step := func(i int, detail string, frac float64) {
		s.emit("setup:progress", Progress{Step: i, Label: InstallSteps[i], Detail: detail, Pct: (float64(i) + frac) / n * 100})
	}
	dir := filepath.Clean(o.InstallDir)

	// 0 — folder
	step(0, dir, 0)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	size, top, err := archiveInfo(s.archive)
	if err != nil {
		return "", err
	}

	// 1 — files
	step(1, "", 0)
	last := time.Time{}
	err = extract(s.archive, dir, func(done int64) {
		if time.Since(last) < 40*time.Millisecond && done < size {
			return
		}
		last = time.Now()
		frac := 1.0
		if size > 0 {
			frac = float64(done) / float64(size)
		}
		step(1, humanBytes(done)+" of "+humanBytes(size), frac)
	})
	if err != nil {
		return "", err
	}
	rec := &Record{Version: version, InstallDir: dir, AppPath: appPath(dir), Installed: time.Now().UTC()}
	for _, t := range top {
		rec.Files = append(rec.Files, filepath.Join(dir, t))
	}
	if un, err := installUninstaller(dir); err != nil {
		return "", err
	} else if un != "" {
		rec.Files = append(rec.Files, un)
	}
	postInstall(rec.AppPath)

	// 2 — shortcuts (a previous install's are replaced)
	step(2, "", 0)
	if old := loadRecord(); old != nil {
		for _, sc := range old.Shortcuts {
			_ = os.Remove(sc)
		}
	}
	rec.Shortcuts, err = createShortcuts(rec.AppPath, o)
	if err != nil {
		return "", err
	}

	// 3 — storage
	step(3, o.DataDir, 0)
	for _, d := range []string{o.DataDir, o.MemoryDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	theme := o.Theme
	if theme != "light" && theme != "dark" {
		theme = "system"
	}
	if err := writeDesktopSettings(map[string]any{"data_dir": filepath.Clean(o.DataDir), "theme": theme}); err != nil {
		return "", err
	}

	// 4 — providers (and the memory folder, which lives in the shared config)
	step(4, "", 0)
	store, err := config.Load()
	if err != nil {
		return "", err
	}
	err = store.Update(func(c *config.Config) error {
		mem := filepath.Clean(o.MemoryDir)
		if samePath(mem, filepath.Join(config.Dir(), "memory")) {
			mem = ""
		}
		c.MemoryDir = mem
		return applyProviders(c, o.Providers, o.Active)
	})
	if err != nil {
		return "", err
	}

	// 5 — autostart, uninstall entry, record
	step(5, "", 0)
	if old := loadRecord(); old != nil && old.Autostart != "" {
		removeAutostart(old.Autostart)
	}
	if o.LaunchAtLogin {
		if rec.Autostart, err = setAutostart(rec.AppPath); err != nil {
			return "", err
		}
	}
	if err := register(rec, size); err != nil {
		return "", err
	}
	if err := saveRecord(rec); err != nil {
		return "", err
	}
	step(5, "", 1)
	return rec.AppPath, nil
}

// Launch starts the installed app and closes the wizard.
func (s *Setup) Launch() error {
	s.mu.Lock()
	app := s.appPath
	s.mu.Unlock()
	if app == "" {
		if rec := loadRecord(); rec != nil {
			app = rec.AppPath
		}
	}
	if app == "" {
		return errors.New("ASG isn't installed yet")
	}
	if err := launch(app); err != nil {
		return err
	}
	s.Quit()
	return nil
}

func (s *Setup) Quit() {
	if s.ctx != nil {
		wruntime.Quit(s.ctx)
	}
}

// Uninstall removes what the install record lists. With removeData set it
// also deletes sessions, artifacts, memory notes and the desktop settings;
// config.json (providers, shared with the asg terminal app) is kept.
func (s *Setup) Uninstall(removeData bool) error {
	rec := loadRecord()
	if rec == nil {
		return errors.New("no ASG install was found for this user")
	}
	if err := s.claim(); err != nil {
		return err
	}
	go func() {
		defer s.release()
		var err error
		if s.archive == nil && !s.uninstall {
			err = s.preview(UninstallSteps)
		} else {
			err = s.remove(rec, removeData)
		}
		if err != nil {
			s.emit("setup:done", Done{Error: err.Error()})
			return
		}
		s.emit("setup:done", Done{OK: true})
	}()
	return nil
}

func (s *Setup) remove(rec *Record, removeData bool) error {
	n := float64(len(UninstallSteps))
	step := func(i int, detail string) {
		s.emit("setup:progress", Progress{Step: i, Label: UninstallSteps[i], Detail: detail, Pct: float64(i) / n * 100})
	}
	step(0, "")
	for _, sc := range rec.Shortcuts {
		_ = os.Remove(sc)
	}
	if rec.Autostart != "" {
		removeAutostart(rec.Autostart)
	}
	unregister()

	step(1, rec.InstallDir)
	self, _ := os.Executable()
	var firstErr error
	for _, f := range rec.Files {
		if self != "" && samePath(f, self) {
			continue // the running uninstaller; removed after it exits
		}
		if !within(rec.InstallDir, f) {
			continue // never delete anything outside the install folder
		}
		if err := os.RemoveAll(f); err != nil && firstErr == nil {
			firstErr = inUse(f, err)
		}
	}
	if firstErr != nil {
		return firstErr
	}
	if self != "" && within(rec.InstallDir, self) {
		s.mu.Lock()
		s.selfDelete = rec.InstallDir
		s.mu.Unlock()
	} else {
		_ = os.Remove(rec.InstallDir) // only if empty
	}

	step(2, "")
	if removeData {
		cfg, _ := readConfig()
		desk := readDesktopSettings()
		data, _ := desk["data_dir"].(string)
		if data == "" {
			data = filepath.Join(config.Dir(), "desktop")
		}
		mem := cfg.MemoryDir
		if mem == "" {
			mem = filepath.Join(config.Dir(), "memory")
			_ = os.RemoveAll(filepath.Join(config.Dir(), "projects")) // project notes
		} else {
			_ = os.RemoveAll(filepath.Join(mem, "projects"))
		}
		for _, d := range []string{data, mem} {
			if safeToDelete(d) {
				_ = os.RemoveAll(d)
			}
		}
		_ = os.Remove(filepath.Join(config.Dir(), "desktop.json"))
	}
	return os.Remove(recordPath())
}

// safeToDelete refuses obviously wrong targets (a drive root, the home folder).
func safeToDelete(dir string) bool {
	d := filepath.Clean(dir)
	if d == "" || d == filepath.Dir(d) || !filepath.IsAbs(d) {
		return false
	}
	home, _ := os.UserHomeDir()
	return home == "" || !samePath(d, home)
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// within reports whether p is dir or inside it.
func within(dir, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(p))
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
