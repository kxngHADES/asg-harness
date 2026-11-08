// Package memory keeps notes the agent decides are worth remembering
// between sessions, one Markdown file per note.
//
//	~/.asg/memory/*.md                       user scope: applies everywhere
//	~/.asg/projects/<project>/memory/*.md    project scope: this directory only
//
// Notes are plain Markdown so people can read and edit them by hand.
package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	User    = "user"
	Project = "project"
)

type Note struct {
	Name    string
	Scope   string
	Path    string
	Summary string // first line, for listings
	Body    string
	ModTime time.Time
}

type Manager struct{ home string }

func New(home string) *Manager { return &Manager{home: home} }

// Dir returns the folder for a scope, relative to the current directory.
func (m *Manager) Dir(scope string) string {
	if scope == Project {
		cwd, _ := os.Getwd()
		return filepath.Join(m.home, "projects", projectSlug(cwd), "memory")
	}
	return filepath.Join(m.home, "memory")
}

func projectSlug(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if runtime.GOOS == "windows" {
		dir = strings.ToLower(dir)
	}
	s := strings.Trim(reNonSlug.ReplaceAllString(dir, "-"), "-")
	if s == "" {
		s = "root"
	}
	return s
}

var (
	reNonSlug = regexp.MustCompile(`[^A-Za-z0-9]+`)
	reName    = regexp.MustCompile(`[^a-z0-9]+`)
)

// Slug turns a note name into a safe file name ("Testing Framework" → "testing-framework").
func Slug(name string) string {
	return strings.Trim(reName.ReplaceAllString(strings.ToLower(strings.TrimSuffix(name, ".md")), "-"), "-")
}

// List returns user notes then project notes, each sorted by name.
func (m *Manager) List() []Note {
	var out []Note
	for _, scope := range []string{User, Project} {
		dir := m.Dir(scope)
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			n := Note{Name: strings.TrimSuffix(e.Name(), ".md"), Scope: scope, Path: p, Body: strings.TrimSpace(string(data))}
			if info, err := e.Info(); err == nil {
				n.ModTime = info.ModTime()
			}
			n.Summary = summary(n.Body)
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Scope != out[j].Scope {
			return out[i].Scope == User
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func summary(body string) string {
	for _, l := range strings.Split(body, "\n") {
		if l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#")); l != "" {
			return l
		}
	}
	return ""
}

// Save writes (or overwrites) a note and returns its path.
func (m *Manager) Save(scope, name, content string) (string, error) {
	if scope != Project {
		scope = User
	}
	slug := Slug(name)
	if slug == "" {
		return "", errors.New("memory name is empty")
	}
	if strings.TrimSpace(content) == "" {
		return "", errors.New("memory content is empty")
	}
	dir := m.Dir(scope)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, slug+".md")
	return p, os.WriteFile(p, []byte(strings.TrimSpace(content)+"\n"), 0o600)
}

// Delete removes a note. An empty scope removes it from whichever scope has it.
func (m *Manager) Delete(scope, name string) error {
	slug := Slug(name)
	scopes := []string{User, Project}
	if scope == User || scope == Project {
		scopes = []string{scope}
	}
	for _, sc := range scopes {
		err := os.Remove(filepath.Join(m.Dir(sc), slug+".md"))
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return fmt.Errorf("no memory named %q", slug)
}

// Prompt renders saved notes for the system prompt. Notes beyond budget
// characters are listed by path so the model can read them on demand.
func (m *Manager) Prompt(budget int) string {
	notes := m.List()
	var b strings.Builder
	b.WriteString(`
Persistent memory: you keep Markdown notes that survive between sessions.
- Call memory_save when the user states a lasting preference, convention or fact (e.g. "always write tests with pytest", "I deploy with fly.io"), or when you learn something non-obvious that will matter next time. Use scope "user" for things true everywhere and "project" for this directory only.
- Saving with an existing name replaces that note, so update notes instead of duplicating them. Call memory_delete when a note turns out to be wrong.
- Do not save secrets, one-off task details, or things already in the code or ASG.md.
- Follow saved notes unless the user says otherwise.
`)
	if len(notes) == 0 {
		b.WriteString("No notes saved yet.\n")
		return b.String()
	}
	b.WriteString("Saved notes:\n")
	used := 0
	for _, n := range notes {
		if used+len(n.Body) > budget {
			fmt.Fprintf(&b, "\n## [%s] %s — %s (not loaded; read %s)\n", n.Scope, n.Name, n.Summary, n.Path)
			continue
		}
		used += len(n.Body)
		fmt.Fprintf(&b, "\n## [%s] %s\n%s\n", n.Scope, n.Name, n.Body)
	}
	return b.String()
}
