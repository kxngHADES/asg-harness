package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadManifestRuntimes(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name, manifest string
		files          map[string]string
		lang, entry    string
		args           []string
		wantErr        string
	}{
		{name: "explicit", manifest: `{"name":"a","runtime":"python","entry":"tool.py"}`, files: map[string]string{"tool.py": ""}, lang: "python", entry: "tool.py"},
		{name: "default entry", manifest: `{"name":"b","runtime":"python"}`, files: map[string]string{"run.py": ""}, lang: "python", entry: "run.py"},
		{name: "detect go", manifest: `{"name":"c"}`, files: map[string]string{"go.mod": "module c", "main.go": ""}, lang: "go", entry: "."},
		{name: "detect py", manifest: `{"name":"d"}`, files: map[string]string{"requirements.txt": "", "main.py": ""}, lang: "python", entry: "main.py"},
		{name: "legacy python command", manifest: `{"name":"e","command":["python","{dir}/run.py","--x"],"args":["y"]}`, lang: "python", entry: "{dir}/run.py", args: []string{"--x", "y"}},
		{name: "plain command", manifest: `{"name":"f","command":["node","{dir}/i.js"]}`},
		{name: "unknown runtime", manifest: `{"name":"g","runtime":"ruby"}`, wantErr: "not supported"},
		{name: "nothing to run", manifest: `{"name":"h"}`, wantErr: "set \"runtime\""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(root, c.name)
			write(t, filepath.Join(dir, "tool.json"), c.manifest)
			for f, b := range c.files {
				write(t, filepath.Join(dir, f), b)
			}
			_, proj, err := readManifest(dir)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("want error %q, got %v", c.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.lang == "" {
				if proj != nil {
					t.Fatalf("command tool should have no project, got %+v", proj)
				}
				return
			}
			abs, _ := filepath.Abs(dir)
			entry := strings.ReplaceAll(c.entry, "{dir}", abs)
			if proj.Lang != c.lang || proj.Entry != entry || strings.Join(proj.Args, " ") != strings.Join(c.args, " ") {
				t.Errorf("got lang=%s entry=%s args=%v", proj.Lang, proj.Entry, proj.Args)
			}
		})
	}
}

func TestScaffoldGoTool(t *testing.T) {
	dir := t.TempDir()
	d, err := ScaffoldTool(dir, "gotool", "--go")
	if err == nil {
		t.Fatal("flag-style language should be cleaned by the caller, not accepted here")
	}
	if d, err = ScaffoldTool(dir, "gotool", "go"); err != nil {
		t.Fatal(err)
	}
	if ScriptToolLang(d) != LangGo {
		t.Errorf("scaffolded Go tool reports %q", ScriptToolLang(d))
	}
	for _, f := range []string{"go.mod", "main.go", "tool.json"} {
		if !exists(filepath.Join(d, f)) {
			t.Errorf("missing %s", f)
		}
	}
	p, _ := ScaffoldTool(dir, "pytool", "")
	if ScriptToolLang(p) != LangPython || !exists(filepath.Join(p, "pyproject.toml")) {
		t.Error("default scaffold should be a Python project")
	}
}

func TestImportToolsLocal(t *testing.T) {
	src, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(src, "pack", "one", "tool.json"), `{"name":"one","runtime":"python"}`)
	write(t, filepath.Join(src, "pack", "one", "main.py"), "print(1)")
	write(t, filepath.Join(src, "pack", "one", ".venv", "junk"), "x")
	write(t, filepath.Join(src, "pack", "two", "tool.json"), `{"name":"two","command":["echo"]}`)
	write(t, filepath.Join(src, "pack", "bad", "tool.json"), `{"name":"bad name"}`)

	got, err := ImportTools(context.Background(), filepath.Join(src, "pack"), dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "one" || got[0].Lang != LangPython || got[1].Lang != "" {
		t.Fatalf("imported %+v", got)
	}
	if exists(filepath.Join(dest, "one", ".venv")) {
		t.Error("virtual environments must not be copied")
	}
	if !exists(filepath.Join(dest, "one", ".source")) {
		t.Error("import should record its source")
	}
	if _, err := ImportTools(context.Background(), t.TempDir(), dest); err == nil {
		t.Error("a folder without tools should fail")
	}
}

func TestScriptProject(t *testing.T) {
	skill := t.TempDir()
	write(t, filepath.Join(skill, "requirements.txt"), "six\n")
	write(t, filepath.Join(skill, "scripts", "do.py"), "")
	write(t, filepath.Join(skill, "scripts", "x.txt"), "")
	write(t, filepath.Join(skill, "gomod", "go.mod"), "module x")
	write(t, filepath.Join(skill, "gomod", "cmd", "main.go"), "")

	p, err := scriptProject(filepath.Join(skill, "scripts", "do.py"))
	if err != nil || p.Lang != LangPython || p.Dir != skill {
		t.Errorf("python script should use the skill's requirements.txt: %+v %v", p, err)
	}
	p, err = scriptProject(filepath.Join(skill, "gomod", "cmd", "main.go"))
	if err != nil || p.Lang != LangGo || p.Dir != filepath.Join(skill, "gomod") {
		t.Errorf("go file should build inside its module: %+v %v", p, err)
	}
	if _, err := scriptProject(filepath.Join(skill, "scripts", "x.txt")); err == nil {
		t.Error("other files should be rejected")
	}
	if _, err := scriptProject(filepath.Join(skill, "scripts")); err == nil {
		t.Error("a folder without go.mod should be rejected")
	}
}

func TestSourceFingerprintChanges(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "main.go"), "package main")
	a := sourceFingerprint(dir, "go")
	if a != sourceFingerprint(dir, "go") {
		t.Fatal("fingerprint should be stable")
	}
	write(t, filepath.Join(dir, "main.go"), "package main // edited")
	if a == sourceFingerprint(dir, "go") {
		t.Error("fingerprint should change after an edit")
	}
}

// TestRuntimeEndToEnd really installs uv, Python 3.12 and libraries, and
// builds Go tools. It needs the network, so it only runs with
// ASG_RUNTIME_E2E=1 (set ASG_E2E_HOME to reuse a runtime between runs).
func TestRuntimeEndToEnd(t *testing.T) {
	if os.Getenv("ASG_RUNTIME_E2E") != "1" {
		t.Skip("set ASG_RUNTIME_E2E=1 to install the runtime and run real tools")
	}
	home := os.Getenv("ASG_E2E_HOME")
	if home == "" {
		home = t.TempDir()
	}
	t.Setenv("ASG_HOME", home)
	var notes []string
	ctx := WithProgress(context.Background(), func(m string) { notes = append(notes, m) })
	tools := t.TempDir()
	run := func(t *testing.T, dir, args string) string {
		t.Helper()
		if err := PrepareScriptTool(ctx, dir); err != nil {
			t.Fatal(err)
		}
		tool, err := LoadScriptTool(dir)
		if err != nil {
			t.Fatal(err)
		}
		out, err := tool.Run(ctx, json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	t.Run("python pyproject deps", func(t *testing.T) {
		d, _ := ScaffoldTool(tools, "py_deps", "python")
		write(t, filepath.Join(d, "pyproject.toml"), "[project]\nname = \"py-deps\"\nversion = \"0.1.0\"\nrequires-python = \">=3.12\"\ndependencies = [\"six\"]\n")
		write(t, filepath.Join(d, "main.py"), "import json, sys, six\nargs = json.load(sys.stdin)\nprint(args['text'], six.__name__, sys.version_info[:2])\n")
		if out := run(t, d, `{"text":"hi"}`); out != "hi six (3, 12)" {
			t.Errorf("got %q", out)
		}
	})
	t.Run("python requirements legacy command", func(t *testing.T) {
		d := filepath.Join(tools, "py_reqs")
		write(t, filepath.Join(d, "tool.json"), `{"name":"py_reqs","command":["python","{dir}/run.py"]}`)
		write(t, filepath.Join(d, "requirements.txt"), "six\n")
		write(t, filepath.Join(d, "run.py"), "import six, sys\nprint(six.__name__, sys.version_info[:2])\n")
		if out := run(t, d, `{}`); out != "six (3, 12)" {
			t.Errorf("got %q", out)
		}
	})
	t.Run("skill script with inline metadata", func(t *testing.T) {
		script := filepath.Join(t.TempDir(), "skill", "scripts", "hello.py")
		write(t, script, "# /// script\n# requires-python = \">=3.12\"\n# dependencies = [\"six\"]\n# ///\nimport six, sys\nprint(six.__name__, sys.argv[1])\n")
		args, _ := json.Marshal(map[string]any{"path": script, "args": []string{"there"}})
		out, err := runScriptTool().Run(ctx, args)
		if err != nil || out != "six there" {
			t.Errorf("got %q %v", out, err)
		}
	})
	t.Run("bare skill script imports", func(t *testing.T) {
		// Shaped like anthropics/skills: scripts import third-party
		// packages plus the skill's own helpers, and declare nothing.
		skill := filepath.Join(t.TempDir(), "pdf")
		write(t, filepath.Join(skill, "scripts", "helpers.py"), "import yaml\n")
		write(t, filepath.Join(skill, "scripts", "office", "__init__.py"), "from . import util\n")
		write(t, filepath.Join(skill, "scripts", "office", "util.py"), "import six\n")
		write(t, filepath.Join(skill, "scripts", "fill.py"), `import json, sys
import helpers
from office import util
from PIL import Image
try:
    import surely_not_a_real_pkg_xyz
except ImportError:
    pass
import yaml, six
print(Image.__name__, yaml.__name__, six.__name__)
`)
		args, _ := json.Marshal(map[string]any{"path": filepath.Join(skill, "scripts", "fill.py")})
		out, err := runScriptTool().Run(ctx, args)
		if err != nil || out != "PIL.Image yaml six" {
			t.Errorf("got %q %v", out, err)
		}
	})
	t.Run("bare tool installs a module the scan missed", func(t *testing.T) {
		d := filepath.Join(tools, "dyn")
		write(t, filepath.Join(d, "tool.json"), `{"name":"dyn","runtime":"python"}`)
		write(t, filepath.Join(d, "main.py"), `import importlib
m = importlib.import_module('six')
print(m.__name__)
`)
		if out := run(t, d, `{}`); out != "six" {
			t.Errorf("got %q", out)
		}
	})
	t.Run("go tool", func(t *testing.T) {
		d, _ := ScaffoldTool(tools, "go_tool", "go")
		if out := run(t, d, `{"text":"hello"}`); out != "received 5 characters" {
			t.Errorf("got %q", out)
		}
	})
	t.Logf("progress: %v", notes)
}

func TestPypiName(t *testing.T) {
	for in, want := range map[string]string{
		"PIL.Image": "pillow", "yaml": "pyyaml", "docx": "python-docx", "pypdf": "pypdf",
		"lxml.etree": "lxml", "google.protobuf.json_format": "protobuf",
		"google.cloud.storage": "google-cloud-storage", "google": "",
	} {
		if got := pypiName(in); got != want {
			t.Errorf("pypiName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBareScriptDetection(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "s.py"), "import requests\n")
	p := &Project{Lang: LangPython, Dir: dir, Entry: "s.py"}
	if !p.bareScript() {
		t.Error("a lone script is bare")
	}
	write(t, filepath.Join(dir, "requirements.txt"), "requests\n")
	if p.bareScript() {
		t.Error("a requirements.txt declares the dependencies")
	}
}
