// Package runtimes owns the language runtimes that script tools run on, so a
// tool works the same on every machine without the user installing anything:
//
//   - uv, downloaded from its official GitHub release (never taken from PATH)
//   - Python 3.12, installed by uv into the runtime folder
//   - Go, used from PATH when present, otherwise downloaded from go.dev on
//     first use (lazily: most people never need it)
//
// Everything lives in <ASG home>/runtime:
//
//	bin/uv[.exe]   the uv binary
//	python/        Python installations managed by uv
//	go/            the Go toolchain, when ASG had to install one
//	envs/          one virtual environment per Python tool
//	gobin/         compiled Go tools, rebuilt when their source changes
//	state.json     the app version the runtime was last bootstrapped for
package runtimes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/fetch"
	"github.com/kxngHADES/asg-harness/internal/proc"
)

// PythonVersion is the Python every tool gets unless its pyproject.toml asks
// for another one.
const PythonVersion = "3.12"

// Progress receives short human-readable status lines while something slow
// (a download, a dependency install) is happening. It may be nil.
type Progress func(msg string)

// Say reports one formatted status line.
func (p Progress) Say(format string, a ...any) {
	if p != nil {
		p(fmt.Sprintf(format, a...))
	}
}

// Manager installs and locates the runtimes in one folder. It is safe for
// concurrent use; installs are serialised.
type Manager struct {
	Home string

	mu       sync.Mutex
	pythonOK bool
	goPath   string
}

var (
	defMu    sync.Mutex
	defaults = map[string]*Manager{}
)

// Default is the manager for the current ASG home (~/.asg/runtime).
func Default() *Manager {
	home := filepath.Join(config.Dir(), "runtime")
	defMu.Lock()
	defer defMu.Unlock()
	if m := defaults[home]; m != nil {
		return m
	}
	m := New(home)
	defaults[home] = m
	return m
}

// New returns a manager rooted at home.
func New(home string) *Manager { return &Manager{Home: home} }

func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// UVPath is where the managed uv binary lives. ASG_UV points at another uv
// binary instead (for offline machines or development).
func (m *Manager) UVPath() string {
	if p := os.Getenv("ASG_UV"); p != "" {
		return p
	}
	return filepath.Join(m.Home, "bin", exe("uv"))
}

// PythonDir is where uv keeps the Python installations it manages.
func (m *Manager) PythonDir() string { return filepath.Join(m.Home, "python") }

// EnvDir is the virtual environment for the tool called name in dir. It
// lives outside the tool's folder so tools stay clean and shareable, and is
// keyed by the folder so two tools never share (and break) one environment.
func (m *Manager) EnvDir(name, dir string) string {
	return filepath.Join(m.Home, "envs", name+"-"+shortHash(fetch.Abs(dir)))
}

// GoBinDir holds compiled Go tools.
func (m *Manager) GoBinDir() string { return filepath.Join(m.Home, "gobin") }

func (m *Manager) managedGo() string { return filepath.Join(m.Home, "go", "bin", exe("go")) }

// Env is the environment script tools and runtime commands run with: uv
// keeps its Pythons in the runtime folder and only uses those, and a Go
// toolchain ASG installed comes first on PATH.
func (m *Manager) Env() []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		// A virtualenv the user activated must not leak into tool runs.
		if strings.HasPrefix(strings.ToUpper(kv), "VIRTUAL_ENV=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"UV_PYTHON_INSTALL_DIR="+m.PythonDir(),
		"UV_PYTHON_PREFERENCE=only-managed",
		"UV_NO_PROGRESS=1",
	)
	if _, err := os.Stat(m.managedGo()); err == nil {
		env = prependPath(env, filepath.Dir(m.managedGo()))
	}
	return env
}

func prependPath(env []string, dir string) []string {
	for i, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PATH") {
			env[i] = k + "=" + dir + string(os.PathListSeparator) + v
			return env
		}
	}
	return append(env, "PATH="+dir)
}

// UV returns the uv binary, downloading it first if needed.
func (m *Manager) UV(ctx context.Context, p Progress) (string, error) {
	path := m.UVPath()
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if os.Getenv("ASG_UV") != "" {
		return "", fmt.Errorf("ASG_UV points at %s, which does not exist", path)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	p.Say("downloading uv (one time)…")
	if err := installUV(ctx, m.Home, path); err != nil {
		return "", fmt.Errorf("install uv: %w", err)
	}
	return path, nil
}

// EnsurePython makes sure the managed Python 3.12 is installed.
func (m *Manager) EnsurePython(ctx context.Context, p Progress) error {
	uv, err := m.UV(ctx, p)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pythonOK {
		return nil
	}
	if _, err := m.Run(ctx, "", uv, "python", "find", PythonVersion); err != nil {
		p.Say("installing Python %s (one time)…", PythonVersion)
		if _, err := m.Run(ctx, "", uv, "python", "install", PythonVersion); err != nil {
			return fmt.Errorf("install Python %s: %w", PythonVersion, err)
		}
	}
	m.pythonOK = true
	return nil
}

// Go returns a working go binary: the one ASG installed, else the one on
// PATH, else it downloads the latest stable toolchain from go.dev.
func (m *Manager) Go(ctx context.Context, p Progress) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.goPath != "" {
		return m.goPath, nil
	}
	candidates := []string{m.managedGo()}
	if os.Getenv("ASG_MANAGED_GO_ONLY") == "" {
		if lp, err := exec.LookPath("go"); err == nil {
			candidates = append(candidates, lp)
		}
	}
	for _, c := range candidates {
		if goWorks(ctx, c) {
			m.goPath = c
			return c, nil
		}
	}
	p.Say("downloading the Go toolchain (one time, ~70 MB)…")
	if err := installGo(ctx, m.Home); err != nil {
		return "", fmt.Errorf("install Go: %w", err)
	}
	if !goWorks(ctx, m.managedGo()) {
		return "", errors.New("installed Go toolchain does not run")
	}
	m.goPath = m.managedGo()
	return m.goPath, nil
}

func goWorks(ctx context.Context, path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "version")
	proc.Hide(cmd)
	return cmd.Run() == nil
}

// Run runs a runtime command (uv, go) in dir with the runtime environment
// and returns its combined output; a failure carries that output.
func (m *Manager) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	return m.RunEnv(ctx, dir, nil, name, args...)
}

// RunEnv is Run with extra environment variables.
func (m *Manager) RunEnv(ctx context.Context, dir string, extra []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	proc.Hide(cmd)
	cmd.Dir = dir
	cmd.Env = append(m.Env(), extra...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	s := strings.TrimSpace(out.String())
	if err != nil {
		if ctx.Err() != nil {
			return s, ctx.Err()
		}
		if s == "" {
			return "", err
		}
		return s, fmt.Errorf("%s %s: %s", filepath.Base(name), strings.Join(args, " "), lastLines(s, 20))
	}
	return s, nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ---------- bootstrap ----------

type state struct {
	Version string    `json:"bootstrapped_version"`
	At      time.Time `json:"bootstrapped_at"`
}

func (m *Manager) statePath() string { return filepath.Join(m.Home, "state.json") }

func (m *Manager) readState() state {
	var s state
	if data, err := os.ReadFile(m.statePath()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

// NeedsBootstrap reports whether Bootstrap should run for appVersion: on a
// new install, after an update, or when uv or Python went missing.
func (m *Manager) NeedsBootstrap(appVersion string) bool {
	if m.readState().Version != appVersion {
		return true
	}
	if _, err := os.Stat(m.UVPath()); err != nil {
		return true
	}
	entries, _ := os.ReadDir(m.PythonDir())
	return len(entries) == 0
}

// Bootstrap installs uv and Python 3.12 (idempotent) and records appVersion
// so it runs again after the next update. Go is not installed here: it is
// fetched the first time a Go tool needs it.
func (m *Manager) Bootstrap(ctx context.Context, appVersion string, p Progress) error {
	if err := m.EnsurePython(ctx, p); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(state{Version: appVersion, At: time.Now().UTC()}, "", "  ")
	if err := os.MkdirAll(m.Home, 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.statePath(), data, 0o644)
}

// Status describes what is installed, for settings screens and `asg runtime`.
type Status struct {
	Home         string `json:"home"`
	UV           string `json:"uv"`              // version line, empty if missing
	Python       string `json:"python"`          // path of the managed 3.12, empty if missing
	Go           string `json:"go"`              // version line, empty if none found
	GoManaged    bool   `json:"go_managed"`      // ASG installed that Go
	Bootstrapped string `json:"bootstrapped"`    // app version of the last bootstrap
	Error        string `json:"error,omitempty"` // why a check failed
}

// Status inspects the runtime without installing anything.
func (m *Manager) Status(ctx context.Context) Status {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	st := Status{Home: m.Home, Bootstrapped: m.readState().Version}
	if uv := m.UVPath(); fileExists(uv) {
		if out, err := m.Run(ctx, "", uv, "--version"); err == nil {
			st.UV = out
		} else {
			st.Error = err.Error()
		}
		if out, err := m.Run(ctx, "", uv, "python", "find", PythonVersion); err == nil {
			st.Python = out
		}
	}
	gobin := m.managedGo()
	st.GoManaged = goWorks(ctx, gobin)
	if !st.GoManaged {
		gobin, _ = exec.LookPath("go")
	}
	if gobin != "" {
		if out, err := m.Run(ctx, "", gobin, "version"); err == nil {
			st.Go = out
		}
	}
	return st
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
