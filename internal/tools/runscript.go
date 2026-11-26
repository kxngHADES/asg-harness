package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/llm"
)

// runScriptTool runs a Python or Go program, such as a script a skill
// ships, on the managed runtime, installing its dependencies first. Skills
// therefore don't need Python or Go on the user's machine, nor a project
// file of their own: a bare script runs on Python 3.12 (or the Go toolchain).
func runScriptTool() *Tool {
	return &Tool{
		Spec: spec("run_script",
			"Run a Python (.py) or Go (.go file, or a folder with go.mod) program on ASG's managed runtime: Python 3.12 via uv, or Go. "+
				"Dependencies are installed first from pyproject.toml or requirements.txt (next to the script or one folder up), PEP 723 inline metadata, or — for a plain script — from its imports; "+
				"Go modules are fetched and the program built. Use this to run scripts that skills ship, instead of calling python or go through bash. "+
				"Prints the program's output.",
			schema([]string{"path"}, map[string]any{
				"path":    str("The .py or .go file, or Go module folder, to run"),
				"args":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Command-line arguments"},
				"stdin":   str("Text passed on standard input (optional)"),
				"timeout": num("Seconds the run may take, not counting first-time setup (default 300)"),
			})),
		NeedsApproval: true,
		Describe: func(raw json.RawMessage) string {
			var a struct {
				Path string   `json:"path"`
				Args []string `json:"args"`
			}
			_ = json.Unmarshal(raw, &a)
			return strings.TrimSpace(a.Path + " " + strings.Join(a.Args, " "))
		},
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var a struct {
				Path    string   `json:"path"`
				Args    []string `json:"args"`
				Stdin   string   `json:"stdin"`
				Timeout int      `json:"timeout"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			proj, err := scriptProject(a.Path)
			if err != nil {
				return "", err
			}
			proj.Args = a.Args
			timeout := time.Duration(a.Timeout) * time.Second
			if timeout <= 0 {
				timeout = 300 * time.Second
			}
			return proj.Exec(ctx, proj.Dir, []byte(a.Stdin), timeout)
		},
	}
}

var reNotName = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// scriptProject works out how to run path: its language, and the folder
// whose pyproject.toml / requirements.txt / go.mod applies to it.
func scriptProject(path string) (*Project, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	name := func(dir string) string {
		return "script-" + strings.Trim(reNotName.ReplaceAllString(filepath.Base(dir), "-"), "-")
	}
	if info.IsDir() {
		if !exists(filepath.Join(abs, "go.mod")) {
			return nil, fmt.Errorf("%s is a folder without go.mod; pass a .py or .go file", path)
		}
		return &Project{Name: name(abs), Dir: abs, Lang: LangGo, Entry: "."}, nil
	}
	dir := filepath.Dir(abs)
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".py":
		projDir := dir
		for _, d := range []string{dir, filepath.Dir(dir)} {
			if exists(filepath.Join(d, "pyproject.toml")) || exists(filepath.Join(d, "requirements.txt")) {
				projDir = d
				break
			}
		}
		return &Project{Name: name(projDir), Dir: projDir, Lang: LangPython, Entry: abs}, nil
	case ".go":
		projDir := dir
		for _, d := range []string{dir, filepath.Dir(dir)} {
			if exists(filepath.Join(d, "go.mod")) {
				projDir = d
				break
			}
		}
		return &Project{Name: name(projDir), Dir: projDir, Lang: LangGo, Entry: abs}, nil
	}
	return nil, fmt.Errorf("%s: only Python (.py) and Go (.go) programs are supported", path)
}

func spec(name, desc string, params map[string]any) llm.ToolSpec {
	return llm.ToolSpec{Name: name, Description: desc, Parameters: params}
}
