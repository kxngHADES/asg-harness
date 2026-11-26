package tools

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/fetch"
)

// ScaffoldScriptTool creates a starter Python tool in dir/name.
func ScaffoldScriptTool(dir, name string) (string, error) {
	return ScaffoldTool(dir, name, LangPython)
}

// ScaffoldTool creates a starter tool in dir/name written in lang
// ("python" or "go"): tool.json plus a ready-to-run project.
func ScaffoldTool(dir, name, lang string) (string, error) {
	if !reToolName.MatchString(name) {
		return "", fmt.Errorf("tool name %q must be letters, digits, _ or -", name)
	}
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" || lang == "py" {
		lang = LangPython
	}
	if lang == "golang" {
		lang = LangGo
	}
	var files map[string]string
	switch lang {
	case LangPython:
		files = pythonTemplate(name)
	case LangGo:
		files = goTemplate(name)
	default:
		return "", fmt.Errorf("language %q is not supported (python or go)", lang)
	}
	tdir := filepath.Join(dir, name)
	if _, err := os.Stat(tdir); err == nil {
		return "", fmt.Errorf("%s already exists", tdir)
	}
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		return "", err
	}
	for f, body := range files {
		if err := os.WriteFile(filepath.Join(tdir, f), []byte(body), 0o644); err != nil {
			return "", err
		}
	}
	return tdir, nil
}

func manifestTemplate(name, lang, entry string) string {
	return fmt.Sprintf(`{
  "name": %q,
  "description": "Describe what the tool does and when the model should use it.",
  "parameters": {
    "type": "object",
    "required": ["text"],
    "properties": {
      "text": {"type": "string", "description": "Example input"}
    }
  },
  "runtime": %q,
  "entry": %q,
  "needs_approval": true,
  "timeout": 60
}
`, name, lang, entry)
}

func pythonTemplate(name string) map[string]string {
	return map[string]string{
		"tool.json": manifestTemplate(name, LangPython, "main.py"),
		"pyproject.toml": fmt.Sprintf(`[project]
name = %q
version = "0.1.0"
description = "ASG tool"
requires-python = ">=3.12"
# Add libraries here (e.g. "requests>=2.32"); ASG installs them into this
# tool's own environment on its first run.
dependencies = []
`, strings.ReplaceAll(name, "_", "-")),
		"main.py": `"""ASG tool: the call's arguments arrive as JSON on stdin; print the result.

Runs on ASG's managed Python 3.12 via uv. Anything printed to stdout goes
back to the model; exit non-zero (or raise) to report an error.
"""
import json
import sys


def main() -> None:
    args = json.load(sys.stdin)
    text = args.get("text", "")
    print(f"received {len(text)} characters")


if __name__ == "__main__":
    main()
`,
	}
}

func goTemplate(name string) map[string]string {
	return map[string]string{
		"tool.json": manifestTemplate(name, LangGo, "."),
		"go.mod":    fmt.Sprintf("module asgtool/%s\n\ngo 1.22\n", name),
		"main.go": `// ASG tool: the call's arguments arrive as JSON on stdin; print the result.
//
// ASG builds this with its managed Go toolchain on the first run (and again
// whenever the source changes). Add modules with "go get" in this folder.
// Anything printed to stdout goes back to the model; exit non-zero to report
// an error.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type args struct {
	Text string ` + "`json:\"text\"`" + `
}

func main() {
	var a args
	if err := json.NewDecoder(os.Stdin).Decode(&a); err != nil {
		fmt.Fprintln(os.Stderr, "bad arguments:", err)
		os.Exit(1)
	}
	fmt.Printf("received %d characters\n", len(a.Text))
}
`,
	}
}

// Imported is one tool installed by ImportTools.
type Imported struct {
	Name string
	Dir  string
	Lang string // python, go or "" for a command tool
}

// ImportTools installs every tool (a folder with tool.json) found in src
// into dest. src may be a local folder or tool.json, github:owner/repo[/path]
// [@ref], owner/repo, or https://github.com/owner/repo[/tree/ref/path].
// Dependencies are not installed here; see PrepareScriptTool.
func ImportTools(ctx context.Context, src, dest string) ([]Imported, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("source required")
	}
	local := fetch.Expand(src)
	if info, err := os.Stat(local); err == nil {
		if !info.IsDir() {
			local = filepath.Dir(local)
		}
		return installTools(local, src, dest)
	}
	if owner, repo, ref, sub, ok := fetch.ParseGitHub(src); ok {
		root, tmp, err := fetch.GitHub(ctx, owner, repo, ref)
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		if sub != "" {
			root = filepath.Join(root, filepath.FromSlash(sub))
			if info, err := os.Stat(root); err == nil && !info.IsDir() {
				root = filepath.Dir(root)
			}
		}
		return installTools(root, src, dest)
	}
	return nil, fmt.Errorf("don't know how to import %q (a folder, github:owner/repo or a GitHub URL)", src)
}

func installTools(root, source, dest string) ([]Imported, error) {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "__pycache__") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "tool.json" {
			dirs = append(dirs, filepath.Dir(path))
			return filepath.SkipDir // a tool's own subfolders are part of it
		}
		return nil
	})
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no tool.json found in %s", source)
	}
	var out []Imported
	var errs []string
	for _, d := range dirs {
		m, _, err := readManifest(d)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		to := filepath.Join(dest, m.Name)
		if fetch.Abs(to) != fetch.Abs(d) {
			_ = os.RemoveAll(to)
			if err := fetch.CopyDir(d, to); err != nil {
				return out, err
			}
			_ = os.WriteFile(filepath.Join(to, ".source"), []byte(source), 0o644)
		}
		if _, err := LoadScriptTool(to); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		out = append(out, Imported{Name: m.Name, Dir: fetch.Abs(to), Lang: ScriptToolLang(to)})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid tools in %s: %s", source, strings.Join(errs, "; "))
	}
	return out, nil
}
