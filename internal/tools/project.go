package tools

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/fetch"
	"github.com/kxngHADES/asg-harness/internal/runtimes"
)

// Languages a tool can be written in and run through ASG's managed runtime.
const (
	LangPython = "python"
	LangGo     = "go"
)

// setupTimeout bounds the first-run work (downloading Python or Go,
// installing dependencies, compiling) separately from the tool's own timeout.
const setupTimeout = 20 * time.Minute

type progressKey struct{}

// WithProgress lets slow first-run work (runtime downloads, dependency
// installs, Go builds) report what it is doing, e.g. as UI notices.
func WithProgress(ctx context.Context, fn func(string)) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

func progressFrom(ctx context.Context) runtimes.Progress {
	if fn, ok := ctx.Value(progressKey{}).(func(string)); ok {
		return fn
	}
	return nil
}

// Project is a Python or Go program run with the managed runtime:
//
//   - Python: uv run, with dependencies from pyproject.toml (synced into a
//     per-tool virtual environment), requirements.txt (pip-installed into
//     one), PEP 723 inline script metadata, or — for a bare script — the
//     imports ASG finds in it. Python 3.12 by default.
//   - Go: built once with go build (modules downloaded on the way) and
//     rebuilt only when the source changes.
type Project struct {
	Name    string   // names the tool's environment and binary
	Dir     string   // folder holding pyproject.toml / requirements.txt / go.mod
	Lang    string   // LangPython or LangGo
	Entry   string   // Python script, or Go package / file; relative to Dir
	Args    []string // extra arguments after the entry
	Runtime *runtimes.Manager
}

func (p *Project) rt() *runtimes.Manager {
	if p.Runtime != nil {
		return p.Runtime
	}
	return runtimes.Default()
}

// Prepare installs whatever the project needs and returns the command that
// runs it plus extra environment variables. It is cheap once done.
func (p *Project) Prepare(ctx context.Context) (argv, env []string, err error) {
	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	switch p.Lang {
	case LangPython:
		return p.preparePython(ctx)
	case LangGo:
		return p.prepareGo(ctx)
	}
	return nil, nil, fmt.Errorf("unsupported language %q (python or go)", p.Lang)
}

func (p *Project) entryPath() string {
	if filepath.IsAbs(p.Entry) {
		return p.Entry
	}
	return filepath.Join(p.Dir, p.Entry)
}

func (p *Project) preparePython(ctx context.Context) ([]string, []string, error) {
	rt, say := p.rt(), progressFrom(ctx)
	uv, err := rt.UV(ctx, say)
	if err != nil {
		return nil, nil, err
	}
	if err := rt.EnsurePython(ctx, say); err != nil {
		return nil, nil, err
	}
	entry := p.entryPath()
	if _, err := os.Stat(entry); err != nil {
		return nil, nil, fmt.Errorf("entry script: %w", err)
	}
	envDir := rt.EnvDir(p.Name, p.Dir)
	stampPath := envDir + ".stamp"
	switch {
	case exists(filepath.Join(p.Dir, "pyproject.toml")):
		extra := []string{"UV_PROJECT_ENVIRONMENT=" + envDir}
		if readStamp(stampPath) != hashFiles(p.Dir, "pyproject.toml", "uv.lock") || !exists(envDir) {
			say.Say("installing dependencies for %s (first run)…", p.Name)
			args := []string{"sync", "-q", "--project", p.Dir}
			if !declaresPython(filepath.Join(p.Dir, "pyproject.toml")) {
				args = append(args, "--python", runtimes.PythonVersion)
			}
			if _, err := rt.RunEnv(ctx, p.Dir, extra, uv, args...); err != nil {
				return nil, nil, err
			}
			// uv sync may have just written uv.lock: stamp after it.
			writeStamp(stampPath, hashFiles(p.Dir, "pyproject.toml", "uv.lock"))
		}
		argv := append([]string{uv, "run", "-q", "--no-sync", "--project", p.Dir, entry}, p.Args...)
		return argv, extra, nil
	case exists(filepath.Join(p.Dir, "requirements.txt")):
		if readStamp(stampPath) != hashFiles(p.Dir, "requirements.txt") || !exists(envDir) {
			say.Say("installing requirements for %s (first run)…", p.Name)
			_ = os.RemoveAll(envDir)
			if _, err := rt.Run(ctx, p.Dir, uv, "venv", "-q", "--python", runtimes.PythonVersion, envDir); err != nil {
				return nil, nil, err
			}
			if _, err := rt.Run(ctx, p.Dir, uv, "pip", "install", "-q", "--python", envDir, "-r", filepath.Join(p.Dir, "requirements.txt")); err != nil {
				return nil, nil, err
			}
			writeStamp(stampPath, hashFiles(p.Dir, "requirements.txt"))
		}
		return append([]string{uv, "run", "-q", "--no-project", "--python", envDir, entry}, p.Args...), nil, nil
	case hasInlineMetadata(entry):
		// PEP 723: uv reads the dependencies from the script itself and
		// caches the environment.
		return append([]string{uv, "run", "-q", "--script", entry}, p.Args...), nil, nil
	default:
		// A bare script: install what it imports into its own environment.
		if err := p.prepareBare(ctx, rt, uv, envDir); err != nil {
			return nil, nil, err
		}
		return append([]string{uv, "run", "-q", "--no-project", "--python", envDir, entry}, p.Args...), nil, nil
	}
}

func (p *Project) prepareGo(ctx context.Context) ([]string, []string, error) {
	rt, say := p.rt(), progressFrom(ctx)
	gobin, err := rt.Go(ctx, say)
	if err != nil {
		return nil, nil, err
	}
	pkg := p.Entry
	if pkg == "" {
		pkg = "."
	}
	if !strings.HasPrefix(pkg, ".") && !filepath.IsAbs(pkg) && !strings.HasSuffix(pkg, ".go") {
		pkg = "./" + pkg
	}
	binDir := rt.GoBinDir()
	exeExt := ""
	if runtime.GOOS == "windows" {
		exeExt = ".exe"
	}
	bin := filepath.Join(binDir, p.Name+"-"+sourceFingerprint(p.Dir, gobin+"|"+pkg)+exeExt)
	if !exists(bin) {
		say.Say("building %s (first run)…", p.Name)
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return nil, nil, err
		}
		tmp := bin + ".tmp" + exeExt
		args := []string{"build", "-o", tmp}
		if exists(filepath.Join(p.Dir, "go.mod")) {
			// -mod=mod lets a tool shipped without go.sum fetch its modules.
			args = []string{"build", "-mod=mod", "-o", tmp}
		}
		if _, err := rt.Run(ctx, p.Dir, gobin, append(args, pkg)...); err != nil {
			os.Remove(tmp)
			return nil, nil, err
		}
		if err := os.Rename(tmp, bin); err != nil {
			return nil, nil, err
		}
		// Drop builds of older source.
		old, _ := filepath.Glob(filepath.Join(binDir, p.Name+"-*"))
		for _, o := range old {
			if o != bin && !strings.HasSuffix(o, ".tmp"+exeExt) {
				_ = os.Remove(o)
			}
		}
	}
	return append([]string{bin}, p.Args...), nil, nil
}

// detectLang guesses a tool folder's language from its files.
func detectLang(dir string) string {
	switch {
	case exists(filepath.Join(dir, "go.mod")):
		return LangGo
	case exists(filepath.Join(dir, "pyproject.toml")), exists(filepath.Join(dir, "requirements.txt")):
		return LangPython
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.py")); len(m) > 0 {
		return LangPython
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.go")); len(m) > 0 {
		return LangGo
	}
	return ""
}

// defaultEntry picks the script a Python tool runs: main.py, run.py,
// <name>.py or the only .py file.
func defaultEntry(dir, name, lang string) string {
	if lang == LangGo {
		return "."
	}
	for _, c := range []string{"main.py", "run.py", name + ".py"} {
		if exists(filepath.Join(dir, c)) {
			return c
		}
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.py")); len(m) == 1 {
		return filepath.Base(m[0])
	}
	return "main.py"
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func declaresPython(pyproject string) bool {
	data, _ := os.ReadFile(pyproject)
	return strings.Contains(string(data), "requires-python")
}

// hasInlineMetadata reports a PEP 723 "# /// script" block.
func hasInlineMetadata(script string) bool {
	f, err := os.Open(script)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for i := 0; sc.Scan() && i < 200; i++ {
		if strings.TrimSpace(sc.Text()) == "# /// script" {
			return true
		}
	}
	return false
}

func hashFiles(dir string, names ...string) string {
	h := sha256.New()
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			fmt.Fprintf(h, "%s:-\n", n)
			continue
		}
		fmt.Fprintf(h, "%s:%d\n", n, len(data))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func readStamp(p string) string {
	data, _ := os.ReadFile(p)
	return strings.TrimSpace(string(data))
}

func writeStamp(p, v string) {
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(v+"\n"), 0o644)
}

// sourceFingerprint changes whenever a file under dir changes, so a Go tool
// is rebuilt after an edit and reused otherwise.
func sourceFingerprint(dir, salt string) string {
	type entry struct {
		rel  string
		size int64
		mod  int64
	}
	var files []entry
	root := fetch.Abs(dir)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(files) > 5000 {
			return nil
		}
		if d.IsDir() {
			if path != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			rel, _ := filepath.Rel(root, path)
			files = append(files, entry{rel, info.Size(), info.ModTime().UnixNano()})
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s\n", root, salt)
	for _, f := range files {
		fmt.Fprintf(h, "%s|%d|%d\n", f.rel, f.size, f.mod)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}
