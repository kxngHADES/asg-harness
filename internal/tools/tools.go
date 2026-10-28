// Package tools implements the built-in tools the model can call.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/llm"
)

const maxOutput = 30000

// Tool is a callable capability exposed to the model.
type Tool struct {
	Spec llm.ToolSpec
	// NeedsApproval marks tools that change state or reach the network.
	NeedsApproval bool
	Run           func(ctx context.Context, args json.RawMessage) (string, error)
	// Describe renders a one-line summary of a call for approval prompts.
	Describe func(args json.RawMessage) string
}

type Registry struct {
	tools map[string]*Tool
	order []string
}

func NewRegistry() *Registry { return &Registry{tools: map[string]*Tool{}} }

func (r *Registry) Add(t *Tool) {
	if _, ok := r.tools[t.Spec.Name]; !ok {
		r.order = append(r.order, t.Spec.Name)
	}
	r.tools[t.Spec.Name] = t
}

func (r *Registry) Get(name string) *Tool { return r.tools[name] }

func (r *Registry) All() []*Tool {
	out := make([]*Tool, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.tools[n])
	}
	return out
}

func (r *Registry) Specs() []llm.ToolSpec {
	var specs []llm.ToolSpec
	for _, t := range r.All() {
		specs = append(specs, t.Spec)
	}
	return specs
}

func schema(required []string, props map[string]any) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }

func truncate(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + fmt.Sprintf("\n... [truncated %d bytes]", len(s)-maxOutput)
}

// flexInt accepts 5, "5" or "" — small local models often quote numbers.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("expected a number, got %s", b)
	}
	*f = flexInt(n)
	return nil
}

// flexBool accepts true, "true", 1 or "1".
type flexBool bool

func (f *flexBool) UnmarshalJSON(b []byte) error {
	switch strings.ToLower(strings.Trim(string(b), `"`)) {
	case "true", "1", "yes":
		*f = true
	default:
		*f = false
	}
	return nil
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("invalid arguments: %w", err)
	}
	return v, nil
}

func field(raw json.RawMessage, key string) string {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if v, ok := m[key]; ok {
		return fmt.Sprint(v)
	}
	return ""
}

// Builtins returns the standard tool set.
func Builtins() *Registry {
	r := NewRegistry()
	r.Add(bashTool())
	r.Add(readTool())
	r.Add(writeTool())
	r.Add(editTool())
	r.Add(listTool())
	r.Add(globTool())
	r.Add(grepTool())
	r.Add(fetchTool())
	return r
}

func bashTool() *Tool {
	return &Tool{
		Spec: llm.ToolSpec{
			Name:        "bash",
			Description: "Run a shell command in the current working directory and return combined stdout/stderr. Use for git, builds, tests, and inspecting the system.",
			Parameters: schema([]string{"command"}, map[string]any{
				"command": str("The shell command to run"),
				"timeout": num("Timeout in seconds (default 120)"),
			}),
		},
		NeedsApproval: true,
		Describe:      func(a json.RawMessage) string { return field(a, "command") },
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args, err := decode[struct {
				Command string  `json:"command"`
				Timeout flexInt `json:"timeout"`
			}](raw)
			if err != nil {
				return "", err
			}
			if args.Timeout <= 0 {
				args.Timeout = 120
			}
			ctx, cancel := context.WithTimeout(ctx, time.Duration(int(args.Timeout))*time.Second)
			defer cancel()
			var cmd *exec.Cmd
			if runtime.GOOS == "windows" {
				cmd = exec.CommandContext(ctx, "cmd", "/C", args.Command)
			} else {
				shell := os.Getenv("SHELL")
				if shell == "" {
					shell = "/bin/sh"
				}
				cmd = exec.CommandContext(ctx, shell, "-c", args.Command)
			}
			var buf bytes.Buffer
			cmd.Stdout, cmd.Stderr = &buf, &buf
			err = cmd.Run()
			out := truncate(buf.String())
			if ctx.Err() == context.DeadlineExceeded {
				return out + "\n[timed out]", nil
			}
			if err != nil {
				return fmt.Sprintf("%s\n[exit: %v]", out, err), nil
			}
			if out == "" {
				out = "(no output)"
			}
			return out, nil
		},
	}
}

func readTool() *Tool {
	return &Tool{
		Spec: llm.ToolSpec{
			Name:        "read_file",
			Description: "Read a text file. Returns lines prefixed with line numbers.",
			Parameters: schema([]string{"path"}, map[string]any{
				"path":   str("File path"),
				"offset": num("1-based line to start from"),
				"limit":  num("Max lines to return (default 2000)"),
			}),
		},
		Describe: func(a json.RawMessage) string { return field(a, "path") },
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			args, err := decode[struct {
				Path   string  `json:"path"`
				Offset flexInt `json:"offset"`
				Limit  flexInt `json:"limit"`
			}](raw)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(expand(args.Path))
			if err != nil {
				return "", err
			}
			lines := strings.Split(string(data), "\n")
			start := max(int(args.Offset), 1) - 1
			limit := int(args.Limit)
			if limit <= 0 {
				limit = 2000
			}
			var b strings.Builder
			for i := start; i < len(lines) && i < start+limit; i++ {
				fmt.Fprintf(&b, "%6d\t%s\n", i+1, lines[i])
			}
			if b.Len() == 0 {
				return "(empty)", nil
			}
			return truncate(b.String()), nil
		},
	}
}

func writeTool() *Tool {
	return &Tool{
		Spec: llm.ToolSpec{
			Name:        "write_file",
			Description: "Create or overwrite a file with the given content. Parent directories are created.",
			Parameters: schema([]string{"path", "content"}, map[string]any{
				"path":    str("File path"),
				"content": str("Full file content"),
			}),
		},
		NeedsApproval: true,
		Describe: func(a json.RawMessage) string {
			return fmt.Sprintf("%s (%d bytes)", field(a, "path"), len(field(a, "content")))
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			args, err := decode[struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}](raw)
			if err != nil {
				return "", err
			}
			p := expand(args.Path)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(p, []byte(args.Content), 0o644); err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(args.Content), p), nil
		},
	}
}

func editTool() *Tool {
	return &Tool{
		Spec: llm.ToolSpec{
			Name:        "edit_file",
			Description: "Replace an exact string in a file. old_string must appear exactly once unless replace_all is true.",
			Parameters: schema([]string{"path", "old_string", "new_string"}, map[string]any{
				"path":        str("File path"),
				"old_string":  str("Exact text to replace"),
				"new_string":  str("Replacement text"),
				"replace_all": map[string]any{"type": "boolean", "description": "Replace every occurrence"},
			}),
		},
		NeedsApproval: true,
		Describe:      func(a json.RawMessage) string { return field(a, "path") },
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			args, err := decode[struct {
				Path       string   `json:"path"`
				Old        string   `json:"old_string"`
				New        string   `json:"new_string"`
				ReplaceAll flexBool `json:"replace_all"`
			}](raw)
			if err != nil {
				return "", err
			}
			p := expand(args.Path)
			data, err := os.ReadFile(p)
			if err != nil {
				return "", err
			}
			s := string(data)
			n := strings.Count(s, args.Old)
			switch {
			case args.Old == "":
				return "", fmt.Errorf("old_string is empty")
			case n == 0:
				return "", fmt.Errorf("old_string not found in %s", p)
			case n > 1 && !bool(args.ReplaceAll):
				return "", fmt.Errorf("old_string appears %d times; add context or set replace_all", n)
			}
			if bool(args.ReplaceAll) {
				s = strings.ReplaceAll(s, args.Old, args.New)
			} else {
				s = strings.Replace(s, args.Old, args.New, 1)
			}
			if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
				return "", err
			}
			return fmt.Sprintf("replaced %d occurrence(s) in %s", map[bool]int{true: n, false: 1}[bool(args.ReplaceAll)], p), nil
		},
	}
}

func listTool() *Tool {
	return &Tool{
		Spec: llm.ToolSpec{
			Name:        "list_dir",
			Description: "List the entries of a directory.",
			Parameters:  schema(nil, map[string]any{"path": str("Directory (default: current)")}),
		},
		Describe: func(a json.RawMessage) string { return field(a, "path") },
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			args, _ := decode[struct {
				Path string `json:"path"`
			}](raw)
			p := expand(args.Path)
			if p == "" {
				p = "."
			}
			entries, err := os.ReadDir(p)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() {
					name += "/"
				}
				b.WriteString(name + "\n")
			}
			return truncate(b.String()), nil
		},
	}
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, ".venv": true, "__pycache__": true}

func walk(root string, fn func(path string, d fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != root && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		return fn(path, d)
	})
}

func globTool() *Tool {
	return &Tool{
		Spec: llm.ToolSpec{
			Name:        "glob",
			Description: "Find files whose path matches a glob pattern. Supports ** for any depth, e.g. **/*.go.",
			Parameters: schema([]string{"pattern"}, map[string]any{
				"pattern": str("Glob pattern"),
				"path":    str("Root directory (default: current)"),
			}),
		},
		Describe: func(a json.RawMessage) string { return field(a, "pattern") },
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			args, err := decode[struct {
				Pattern string `json:"pattern"`
				Path    string `json:"path"`
			}](raw)
			if err != nil {
				return "", err
			}
			root := expand(args.Path)
			if root == "" {
				root = "."
			}
			re, err := globToRegexp(args.Pattern)
			if err != nil {
				return "", err
			}
			var matches []string
			_ = walk(root, func(path string, d fs.DirEntry) error {
				rel, _ := filepath.Rel(root, path)
				if !d.IsDir() && re.MatchString(filepath.ToSlash(rel)) {
					matches = append(matches, path)
				}
				if len(matches) >= 1000 {
					return fs.SkipAll
				}
				return nil
			})
			sort.Strings(matches)
			if len(matches) == 0 {
				return "no matches", nil
			}
			return truncate(strings.Join(matches, "\n")), nil
		},
	}
}

func globToRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '{':
			b.WriteString("(?:")
		case '}':
			b.WriteString(")")
		case ',':
			b.WriteString("|")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func grepTool() *Tool {
	return &Tool{
		Spec: llm.ToolSpec{
			Name:        "grep",
			Description: "Search file contents with a regular expression. Returns file:line: text matches.",
			Parameters: schema([]string{"pattern"}, map[string]any{
				"pattern": str("Regular expression (Go syntax)"),
				"path":    str("File or directory to search (default: current)"),
				"include": str("Optional glob to filter file names, e.g. *.go"),
			}),
		},
		Describe: func(a json.RawMessage) string { return field(a, "pattern") },
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			args, err := decode[struct {
				Pattern string `json:"pattern"`
				Path    string `json:"path"`
				Include string `json:"include"`
			}](raw)
			if err != nil {
				return "", err
			}
			re, err := regexp.Compile(args.Pattern)
			if err != nil {
				return "", err
			}
			root := expand(args.Path)
			if root == "" {
				root = "."
			}
			var b strings.Builder
			count := 0
			_ = walk(root, func(path string, d fs.DirEntry) error {
				if d.IsDir() {
					return nil
				}
				if args.Include != "" {
					if ok, _ := filepath.Match(args.Include, d.Name()); !ok {
						return nil
					}
				}
				info, err := d.Info()
				if err != nil || info.Size() > 2<<20 {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil || bytes.IndexByte(data, 0) >= 0 {
					return nil
				}
				for i, line := range strings.Split(string(data), "\n") {
					if re.MatchString(line) {
						fmt.Fprintf(&b, "%s:%d: %s\n", path, i+1, strings.TrimSpace(line))
						if count++; count >= 500 {
							return fs.SkipAll
						}
					}
				}
				return nil
			})
			if count == 0 {
				return "no matches", nil
			}
			return truncate(b.String()), nil
		},
	}
}

var (
	reScript = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	reTag    = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpace  = regexp.MustCompile(`\n\s*\n+`)
)

func fetchTool() *Tool {
	return &Tool{
		Spec: llm.ToolSpec{
			Name:        "web_fetch",
			Description: "Fetch a URL over HTTP(S) and return its text content (HTML is stripped to text).",
			Parameters:  schema([]string{"url"}, map[string]any{"url": str("The URL to fetch")}),
		},
		NeedsApproval: true,
		Describe:      func(a json.RawMessage) string { return field(a, "url") },
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			args, err := decode[struct {
				URL string `json:"url"`
			}](raw)
			if err != nil {
				return "", err
			}
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", args.URL, nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("User-Agent", "asg-harness/1.0")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
			if err != nil {
				return "", err
			}
			text := string(data)
			if strings.Contains(resp.Header.Get("Content-Type"), "html") {
				text = reScript.ReplaceAllString(text, "")
				text = reTag.ReplaceAllString(text, "")
				text = htmlUnescape(text)
				text = reSpace.ReplaceAllString(text, "\n\n")
			}
			return truncate(fmt.Sprintf("[%s]\n%s", resp.Status, strings.TrimSpace(text))), nil
		},
	}
}

var htmlEntities = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&nbsp;", " ")

func htmlUnescape(s string) string { return htmlEntities.Replace(s) }

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
