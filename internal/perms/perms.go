// Package perms remembers "always allow" answers to tool permission gates
// across sessions (~/.asg/permissions.json).
//
// A rule is a tool name ("edit_file", "web_fetch") or, for the bash tool,
// "bash:<program>" ("bash:git"), so allowing git once covers every later
// git command without also allowing rm. Rules are saved per project
// (keyed by working directory) or globally.
package perms

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
)

type file struct {
	Global   []string            `json:"global"`
	Projects map[string][]string `json:"projects"`
}

// Store is safe for concurrent use; every change is written to disk.
type Store struct {
	mu   sync.Mutex
	path string
	data file
}

// Load reads dir/permissions.json; a missing or unreadable file starts empty.
func Load(dir string) *Store {
	s := &Store{path: filepath.Join(dir, "permissions.json")}
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	if s.data.Projects == nil {
		s.data.Projects = map[string][]string{}
	}
	return s
}

func (s *Store) Path() string { return s.path }

// ProjectKey normalises a directory so the same project always maps to
// the same entry (case-insensitive on Windows).
func ProjectKey(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dir = filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		dir = strings.ToLower(dir)
	}
	return dir
}

// Allowed reports whether every rule is granted for the project.
func (s *Store) Allowed(project string, rules []string) bool {
	if len(rules) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	local := s.data.Projects[ProjectKey(project)]
	for _, r := range rules {
		if !contains(s.data.Global, r) && !contains(local, r) {
			return false
		}
	}
	return true
}

// Add grants rules for the project, or everywhere when global is set.
func (s *Store) Add(project string, rules []string, global bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if global {
		s.data.Global = merge(s.data.Global, rules)
	} else {
		k := ProjectKey(project)
		s.data.Projects[k] = merge(s.data.Projects[k], rules)
	}
	return s.save()
}

// Remove revokes a rule from the project and global lists.
func (s *Store) Remove(project, rule string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := ProjectKey(project)
	s.data.Global = without(s.data.Global, rule)
	if l := without(s.data.Projects[k], rule); len(l) > 0 {
		s.data.Projects[k] = l
	} else {
		delete(s.data.Projects, k)
	}
	return s.save()
}

// List returns the rules that apply to a project.
func (s *Store) List(project string) (global, local []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.data.Global...), append([]string(nil), s.data.Projects[ProjectKey(project)]...)
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

var (
	reSplit  = regexp.MustCompile(`&&|\|\||[;|&\n]`)
	reEnvVar = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
)

// ErrNotRememberable means a call is too dynamic to approve by rule
// (for example a shell command with $(…) substitution).
var ErrNotRememberable = errors.New("not rememberable")

// RulesFor returns the rules a tool call needs. Bash commands are split
// on && || ; | & and newlines, and each part needs its program allowed.
func RulesFor(tool string, args json.RawMessage) ([]string, error) {
	if tool != "bash" {
		return []string{tool}, nil
	}
	var a struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(args, &a)
	cmd := a.Command
	if strings.Contains(cmd, "$(") || strings.Contains(cmd, "`") || strings.TrimSpace(cmd) == "" {
		return nil, ErrNotRememberable
	}
	var rules []string
	for _, part := range reSplit.Split(cmd, -1) {
		prog := program(part)
		if prog == "" {
			continue
		}
		rules = merge(rules, []string{"bash:" + prog})
	}
	if len(rules) == 0 {
		return nil, ErrNotRememberable
	}
	return rules, nil
}

// program returns the command name of one simple shell command.
func program(part string) string {
	for _, f := range strings.Fields(part) {
		if reEnvVar.MatchString(f) {
			continue
		}
		f = strings.Trim(f, `"'()`)
		if runtime.GOOS == "windows" {
			f = strings.TrimSuffix(strings.ToLower(f), ".exe")
		}
		return f
	}
	return ""
}

// Describe renders rules for people: "bash:git" → "git", joined with ", ".
func Describe(rules []string) string {
	out := make([]string, len(rules))
	for i, r := range rules {
		out[i] = strings.TrimPrefix(r, "bash:")
	}
	return strings.Join(out, ", ")
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func merge(l, add []string) []string {
	for _, r := range add {
		if !contains(l, r) {
			l = append(l, r)
		}
	}
	sort.Strings(l)
	return l
}

func without(l []string, s string) []string {
	var out []string
	for _, x := range l {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}
