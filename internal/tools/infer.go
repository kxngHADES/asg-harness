package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/runtimes"
)

// Most scripts that ship with skills and shared tools are a bare .py file:
// no pyproject.toml, no requirements.txt, no inline metadata — they just
// import what they need (pypdf, lxml, PIL, …). For those ASG works the
// dependencies out itself: it reads the imports (following the script's own
// local modules), drops the standard library and the script's own modules,
// maps import names to PyPI names and installs them into the tool's own
// environment. If a run still hits ModuleNotFoundError, that module is
// installed and the run retried.

//go:embed infer_imports.py
var inferImportsPy []byte

// pypiNames maps import names to the PyPI distribution that provides them,
// where the two differ. Everything else installs under its import name.
var pypiNames = map[string]string{
	"PIL": "pillow", "cv2": "opencv-python", "yaml": "pyyaml", "bs4": "beautifulsoup4",
	"sklearn": "scikit-learn", "skimage": "scikit-image", "docx": "python-docx",
	"pptx": "python-pptx", "fitz": "pymupdf", "dateutil": "python-dateutil",
	"dotenv": "python-dotenv", "jwt": "pyjwt", "Crypto": "pycryptodome", "Cryptodome": "pycryptodomex",
	"OpenSSL": "pyopenssl", "magic": "python-magic", "serial": "pyserial", "usb": "pyusb",
	"gi": "pygobject", "attr": "attrs", "git": "gitpython", "github": "pygithub",
	"telegram": "python-telegram-bot", "discord": "discord.py", "slugify": "python-slugify",
	"Levenshtein": "python-Levenshtein", "mpl_toolkits": "matplotlib", "pkg_resources": "setuptools",
	"odf": "odfpy", "zmq": "pyzmq", "psycopg2": "psycopg2-binary", "MySQLdb": "mysqlclient",
	"mysql": "mysql-connector-python", "ldap": "python-ldap", "socks": "pysocks", "Xlib": "python-xlib",
	"wx": "wxPython", "speech_recognition": "SpeechRecognition", "pyaudio": "PyAudio",
	"ruamel": "ruamel.yaml", "jose": "python-jose", "multipart": "python-multipart",
	"websocket": "websocket-client", "yt_dlp": "yt-dlp", "win32api": "pywin32", "win32con": "pywin32",
	"win32com": "pywin32", "win32gui": "pywin32", "win32clipboard": "pywin32", "pywintypes": "pywin32",
	"pythoncom": "pywin32", "docx2txt": "docx2txt", "markdown_it": "markdown-it-py",
	"pdfminer": "pdfminer.six", "pymupdf": "pymupdf", "Bio": "biopython", "kafka": "kafka-python",
	"lz4": "lz4", "snappy": "python-snappy", "nacl": "pynacl", "jinja2": "Jinja2", "tree_sitter": "tree-sitter",
}

// pypiName is the distribution to install for an imported module.
func pypiName(module string) string {
	parts := strings.Split(module, ".")
	if parts[0] == "google" {
		// A namespace package: google.protobuf, google.generativeai, google.cloud.storage …
		switch {
		case len(parts) >= 3 && parts[1] == "cloud":
			return "google-cloud-" + parts[2]
		case len(parts) >= 2 && parts[1] == "protobuf":
			return "protobuf"
		case len(parts) >= 2:
			return "google-" + strings.ReplaceAll(parts[1], "_", "-")
		}
		return ""
	}
	if n, ok := pypiNames[parts[0]]; ok {
		return n
	}
	return parts[0]
}

type inferred struct {
	Required []string `json:"required"`
	Optional []string `json:"optional"` // imported inside try/except ImportError
}

// bareScript reports a Python project with nothing that declares its
// dependencies, whose imports ASG therefore installs itself.
func (p *Project) bareScript() bool {
	return p.Lang == LangPython &&
		!exists(filepath.Join(p.Dir, "pyproject.toml")) &&
		!exists(filepath.Join(p.Dir, "requirements.txt")) &&
		!hasInlineMetadata(p.entryPath())
}

// searchRoots are where a script's own modules may live: its folder, the
// tool / skill folder, and the folder above (skills run as
// `python -m scripts.x` or import `scripts.helpers` from their root).
func (p *Project) searchRoots() []string {
	entryDir := filepath.Dir(p.entryPath())
	roots := []string{entryDir}
	for _, d := range []string{p.Dir, filepath.Dir(entryDir)} {
		if d != entryDir {
			roots = append(roots, d)
		}
	}
	return roots
}

// prepareBare installs the imports of a bare script into envDir. It re-reads
// the imports only when the script's folders change.
func (p *Project) prepareBare(ctx context.Context, rt *runtimes.Manager, uv, envDir string) error {
	stampPath := envDir + ".stamp"
	roots := p.searchRoots()
	stamp := "imports:" + sourceFingerprint(roots[0], strings.Join(roots[1:], "|")+"|"+p.Entry)
	if readStamp(stampPath) == stamp && exists(envDir) {
		return nil
	}
	deps, err := inferImports(ctx, rt, uv, p.entryPath(), roots)
	if err != nil {
		return err
	}
	if !exists(envDir) {
		if _, err := rt.Run(ctx, p.Dir, uv, "venv", "-q", "--python", runtimes.PythonVersion, envDir); err != nil {
			return err
		}
	}
	if len(deps.Required)+len(deps.Optional) > 0 {
		say := progressFrom(ctx)
		what := strings.Join(deps.Required, ", ")
		if len(deps.Optional) > 0 {
			if what != "" {
				what += " "
			}
			what += "(optional: " + strings.Join(deps.Optional, ", ") + ")"
		}
		say.Say("installing %s for %s (first run)…", what, p.Name)
		if failed := pipInstall(ctx, rt, uv, envDir, deps.Required); len(failed) > 0 {
			say.Say("could not install %s for %s; the script may still fail on those imports", strings.Join(failed, ", "), p.Name)
		}
		pipInstall(ctx, rt, uv, envDir, deps.Optional) // optional: failures are fine
	}
	writeStamp(stampPath, stamp)
	return nil
}

// inferImports lists the PyPI packages a script imports.
func inferImports(ctx context.Context, rt *runtimes.Manager, uv, entry string, roots []string) (inferred, error) {
	helper := filepath.Join(rt.Home, "infer_imports.py")
	if old, err := os.ReadFile(helper); err != nil || string(old) != string(inferImportsPy) {
		if err := os.MkdirAll(rt.Home, 0o755); err != nil {
			return inferred{}, err
		}
		if err := os.WriteFile(helper, inferImportsPy, 0o644); err != nil {
			return inferred{}, err
		}
	}
	args := append([]string{"run", "-q", "--no-project", "--python", runtimes.PythonVersion, helper, entry}, roots...)
	out, err := rt.Run(ctx, "", uv, args...)
	if err != nil {
		return inferred{}, fmt.Errorf("read the imports of %s: %w", filepath.Base(entry), err)
	}
	var mods inferred
	if err := json.Unmarshal([]byte(lastLine(out)), &mods); err != nil {
		return inferred{}, fmt.Errorf("read the imports of %s: %w", filepath.Base(entry), err)
	}
	seen := map[string]bool{}
	toPkgs := func(in []string) []string {
		var pkgs []string
		for _, m := range in {
			if n := pypiName(m); n != "" && !seen[strings.ToLower(n)] {
				seen[strings.ToLower(n)] = true
				pkgs = append(pkgs, n)
			}
		}
		sort.Strings(pkgs)
		return pkgs
	}
	return inferred{Required: toPkgs(mods.Required), Optional: toPkgs(mods.Optional)}, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

// pipInstall installs pkgs into envDir: all at once, and if that fails one
// by one so a single bad name doesn't block the rest. It returns the ones
// that could not be installed.
func pipInstall(ctx context.Context, rt *runtimes.Manager, uv, envDir string, pkgs []string) []string {
	if len(pkgs) == 0 {
		return nil
	}
	base := []string{"pip", "install", "-q", "--python", envDir}
	if _, err := rt.Run(ctx, "", uv, append(base, pkgs...)...); err == nil {
		return nil
	}
	var failed []string
	for _, p := range pkgs {
		if _, err := rt.Run(ctx, "", uv, append(base, p)...); err != nil {
			failed = append(failed, p)
		}
	}
	return failed
}

var reMissingModule = regexp.MustCompile(`No module named '([A-Za-z_][A-Za-z0-9_.]*)'`)

// installMissing handles a ModuleNotFoundError from a bare script the
// import scan missed (a dynamic import, say): it installs that module and
// reports whether a retry is worth it.
func (p *Project) installMissing(ctx context.Context, errText string) bool {
	m := reMissingModule.FindStringSubmatch(errText)
	if m == nil || !p.bareScript() {
		return false
	}
	pkg := pypiName(m[1])
	if pkg == "" {
		return false
	}
	rt := p.rt()
	uv, err := rt.UV(ctx, progressFrom(ctx))
	if err != nil {
		return false
	}
	progressFrom(ctx).Say("installing %s for %s…", pkg, p.Name)
	return len(pipInstall(ctx, rt, uv, rt.EnvDir(p.Name, p.Dir), []string{pkg})) == 0
}

// Exec prepares the project and runs it with stdin. A bare Python script
// that fails on a missing module gets it installed and is run again.
func (p *Project) Exec(ctx context.Context, workDir string, stdin []byte, timeout time.Duration) (string, error) {
	argv, env, err := p.Prepare(ctx)
	if err != nil {
		return "", fmt.Errorf("prepare %s: %w", p.Name, err)
	}
	for attempt := 0; ; attempt++ {
		out, err := runScript(ctx, workDir, argv, append(p.rt().Env(), env...), stdin, timeout)
		if err == nil || attempt >= 3 || ctx.Err() != nil || !p.installMissing(ctx, err.Error()) {
			return out, err
		}
	}
}
