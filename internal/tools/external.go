package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxngHADES/asg-harness/internal/proc"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/llm"
)

// Script tools let anyone add a tool without recompiling ASG: a folder
// holding tool.json plus the program it runs. Python and Go tools run on
// ASG's managed runtime (uv + Python 3.12, and Go), so they work without the
// user installing anything:
//
//	{
//	  "name": "word_count",
//	  "description": "Count words in a file",
//	  "parameters": {"type": "object", "required": ["path"],
//	                 "properties": {"path": {"type": "string"}}},
//	  "runtime": "python",
//	  "entry": "main.py",
//	  "needs_approval": false,
//	  "timeout": 60
//	}
//
// A Python tool may declare dependencies in pyproject.toml, requirements.txt
// or PEP 723 inline metadata, or just import them (ASG reads the imports of
// a bare script); they are installed on first run into the tool's own
// virtual environment. A Go tool is a module (go.mod + main.go),
// built on first run. Any other program can run through "command".
//
// The tool's arguments arrive as JSON on stdin (and in $ASG_TOOL_ARGS);
// whatever the program prints is the result. A non-zero exit is an error.
// {dir} in command, entry and args is replaced with the tool's folder.

// Manifest is the content of a tool.json file.
type Manifest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	// Runtime is "python" or "go". Empty means: use Command if given, else
	// guess from the files in the folder.
	Runtime string `json:"runtime,omitempty"`
	// Entry is the Python script (default main.py) or Go package (default
	// ".") relative to the tool folder.
	Entry string `json:"entry,omitempty"`
	// Args are extra command-line arguments for the entry.
	Args []string `json:"args,omitempty"`
	// Command runs any other program, e.g. ["node", "{dir}/index.js"].
	// ["python", "x.py"] is run on the managed Python.
	Command []string `json:"command,omitempty"`
	// NeedsApproval defaults to true: a script can do anything.
	NeedsApproval *bool `json:"needs_approval,omitempty"`
	Timeout       int   `json:"timeout,omitempty"` // seconds, default 120
}

var reToolName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

// LoadScriptTools reads every <dir>/<name>/tool.json under the given
// folders. Earlier folders win when two tools share a name. Broken
// manifests are reported, not fatal.
func LoadScriptTools(dirs ...string) ([]*Tool, []error) {
	var out []*Tool
	var errs []error
	seen := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			tdir := filepath.Join(dir, e.Name())
			if _, err := os.Stat(filepath.Join(tdir, "tool.json")); err != nil {
				continue
			}
			t, err := LoadScriptTool(tdir)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if seen[t.Spec.Name] {
				continue
			}
			seen[t.Spec.Name] = true
			out = append(out, t)
		}
	}
	return out, errs
}

// readManifest reads dir/tool.json and resolves how the tool runs: a
// Project for Python and Go tools, or a plain command.
func readManifest(dir string) (Manifest, *Project, error) {
	path := filepath.Join(dir, "tool.json")
	var m Manifest
	data, err := os.ReadFile(path)
	if err != nil {
		return m, nil, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, nil, fmt.Errorf("%s: %w", path, err)
	}
	if !reToolName.MatchString(m.Name) {
		return m, nil, fmt.Errorf("%s: name %q must be letters, digits, _ or -", path, m.Name)
	}
	abs, _ := filepath.Abs(dir)
	sub := func(s string) string { return strings.ReplaceAll(s, "{dir}", abs) }
	lang := strings.ToLower(strings.TrimSpace(m.Runtime))
	entry := sub(m.Entry)
	args := subAll(m.Args, sub)
	if lang == "" && len(m.Command) >= 2 && isPythonCommand(m.Command[0]) && strings.HasSuffix(m.Command[1], ".py") {
		// ["python", "{dir}/run.py", …]: run it on the managed Python
		// instead of whatever "python" happens to be on PATH.
		lang, entry = LangPython, sub(m.Command[1])
		args = append(subAll(m.Command[2:], sub), args...)
	}
	if lang == "" && len(m.Command) == 0 {
		lang = detectLang(abs)
		if lang == "" {
			return m, nil, fmt.Errorf(`%s: set "runtime" (python or go) or "command"`, path)
		}
	}
	if lang == "" {
		return m, nil, nil
	}
	if lang != LangPython && lang != LangGo {
		return m, nil, fmt.Errorf("%s: runtime %q is not supported (python or go)", path, m.Runtime)
	}
	if entry == "" {
		entry = defaultEntry(abs, m.Name, lang)
	}
	return m, &Project{Name: m.Name, Dir: abs, Lang: lang, Entry: entry, Args: args}, nil
}

func isPythonCommand(c string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(c)), ".exe")
	return base == "python" || base == "python3" || base == "py"
}

func subAll(in []string, sub func(string) string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = sub(s)
	}
	return out
}

// LoadScriptTool loads the tool in dir.
func LoadScriptTool(dir string) (*Tool, error) {
	m, proj, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	if m.Parameters == nil {
		m.Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	timeout := time.Duration(m.Timeout) * time.Second
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	abs, _ := filepath.Abs(dir)
	needs := m.NeedsApproval == nil || *m.NeedsApproval
	return &Tool{
		Spec:          llm.ToolSpec{Name: m.Name, Description: m.Description, Parameters: m.Parameters},
		NeedsApproval: needs,
		Source:        abs,
		Describe: func(a json.RawMessage) string {
			s := strings.Join(strings.Fields(string(a)), " ")
			if len(s) > 160 {
				s = s[:159] + "…"
			}
			return s
		},
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			if len(raw) == 0 {
				raw = json.RawMessage("{}")
			}
			if proj == nil {
				return runScript(ctx, abs, subAll(m.Command, func(s string) string { return strings.ReplaceAll(s, "{dir}", abs) }), nil, raw, timeout)
			}
			// Setup (runtime, dependencies, build) has its own, longer
			// limit; the tool's timeout covers only the run itself.
			return proj.Exec(ctx, abs, raw, timeout)
		},
	}, nil
}

// PrepareScriptTool installs what the tool in dir needs (runtime,
// dependencies, Go build) so its first call is fast. Command tools need
// nothing and return nil.
func PrepareScriptTool(ctx context.Context, dir string) error {
	_, proj, err := readManifest(dir)
	if err != nil || proj == nil {
		return err
	}
	_, _, err = proj.Prepare(ctx)
	return err
}

// ScriptToolLang reports "python", "go" or "" (a plain command) for the
// tool in dir.
func ScriptToolLang(dir string) string {
	if _, proj, err := readManifest(dir); err == nil && proj != nil {
		return proj.Lang
	}
	return ""
}

func runScript(ctx context.Context, dir string, argv, env []string, args []byte, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	proc.Hide(cmd)
	cmd.Stdin = bytes.NewReader(args)
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = append(env, "ASG_TOOL_ARGS="+string(args), "ASG_TOOL_DIR="+dir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String() + "\n" + out)
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(truncate(msg))
	}
	if out == "" {
		out = strings.TrimSpace(stderr.String())
	}
	if out == "" {
		out = "(no output)"
	}
	return truncate(out), nil
}
