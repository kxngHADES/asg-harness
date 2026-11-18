package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
// holding tool.json plus whatever script it runs, in any language.
//
//	{
//	  "name": "word_count",
//	  "description": "Count words in a file",
//	  "parameters": {"type": "object", "required": ["path"],
//	                 "properties": {"path": {"type": "string"}}},
//	  "command": ["python", "{dir}/run.py"],
//	  "needs_approval": false,
//	  "timeout": 60
//	}
//
// The tool's arguments arrive as JSON on stdin (and in $ASG_TOOL_ARGS);
// whatever the command prints is the result. A non-zero exit is an error.
// {dir} in the command is replaced with the tool's folder.

// Manifest is the content of a tool.json file.
type Manifest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Command     []string       `json:"command"`
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

// LoadScriptTool loads the tool in dir.
func LoadScriptTool(dir string) (*Tool, error) {
	path := filepath.Join(dir, "tool.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !reToolName.MatchString(m.Name) {
		return nil, fmt.Errorf("%s: name %q must be letters, digits, _ or -", path, m.Name)
	}
	if len(m.Command) == 0 {
		return nil, fmt.Errorf("%s: command is empty", path)
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
			return runScript(ctx, abs, m.Command, raw, timeout)
		},
	}, nil
}

func runScript(ctx context.Context, dir string, command []string, args json.RawMessage, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := make([]string, len(command))
	for i, a := range command {
		argv[i] = strings.ReplaceAll(a, "{dir}", dir)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(args)
	cmd.Env = append(os.Environ(), "ASG_TOOL_ARGS="+string(args), "ASG_TOOL_DIR="+dir)
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

// ScaffoldScriptTool creates a starter script tool in dir/name.
func ScaffoldScriptTool(dir, name string) (string, error) {
	if !reToolName.MatchString(name) {
		return "", fmt.Errorf("tool name %q must be letters, digits, _ or -", name)
	}
	tdir := filepath.Join(dir, name)
	if _, err := os.Stat(tdir); err == nil {
		return "", fmt.Errorf("%s already exists", tdir)
	}
	if err := os.MkdirAll(tdir, 0o755); err != nil {
		return "", err
	}
	manifest := fmt.Sprintf(`{
  "name": %q,
  "description": "Describe what the tool does and when the model should use it.",
  "parameters": {
    "type": "object",
    "required": ["text"],
    "properties": {
      "text": {"type": "string", "description": "Example input"}
    }
  },
  "command": ["python", "{dir}/run.py"],
  "needs_approval": true,
  "timeout": 60
}
`, name)
	script := `"""ASG script tool: arguments arrive as JSON on stdin; print the result."""
import json
import sys

args = json.load(sys.stdin)
text = args.get("text", "")
print(f"received {len(text)} characters")
`
	if err := os.WriteFile(filepath.Join(tdir, "tool.json"), []byte(manifest), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tdir, "run.py"), []byte(script), 0o644); err != nil {
		return "", err
	}
	return tdir, nil
}
