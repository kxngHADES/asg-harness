// Package skills manages SKILL.md based skills: loading them from disk and
// importing them from local folders, URLs and GitHub.
//
// A skill is a folder containing SKILL.md with YAML-ish front matter:
//
//	---
//	name: my-skill
//	description: What it does and when to use it
//	---
//	Instructions for the model...
package skills

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/fetch"
)

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Dir         string `json:"dir"`
	Body        string `json:"-"`
	Source      string `json:"source,omitempty"`
	Builtin     bool   `json:"builtin,omitempty"` // ships inside the binary; Dir is empty
}

type Manager struct {
	Dirs []string // searched in order; the first is where imports go
}

// NewManager uses ~/.asg/skills plus ./.asg/skills for project-local skills.
func NewManager(home string) *Manager {
	return &Manager{Dirs: []string{filepath.Join(home, "skills"), filepath.Join(".asg", "skills")}}
}

func (m *Manager) InstallDir() string { return m.Dirs[0] }

// List loads every skill found in the skill directories.
func (m *Manager) List() []*Skill {
	seen := map[string]bool{}
	out := []*Skill{} // never nil: the dashboard expects a JSON array
	for _, dir := range m.Dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			s, err := Load(filepath.Join(dir, e.Name()))
			if err != nil || seen[s.Name] {
				continue
			}
			seen[s.Name] = true
			out = append(out, s)
		}
	}
	// Built-ins come last, so a user skill with the same name replaces one.
	for _, b := range Builtins() {
		if !seen[b.Name] {
			seen[b.Name] = true
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) Get(name string) *Skill {
	for _, s := range m.List() {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// Load parses SKILL.md inside dir.
func Load(dir string) (*Skill, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	meta, body := parseFrontMatter(string(data))
	s := &Skill{Name: meta["name"], Description: meta["description"], Dir: dir, Body: strings.TrimSpace(body)}
	if s.Name == "" {
		s.Name = filepath.Base(dir)
	}
	if src, err := os.ReadFile(filepath.Join(dir, ".source")); err == nil {
		s.Source = strings.TrimSpace(string(src))
	}
	return s, nil
}

func parseFrontMatter(text string) (map[string]string, string) {
	meta := map[string]string{}
	text = strings.TrimPrefix(text, "\uFEFF")
	if !strings.HasPrefix(text, "---") {
		return meta, text
	}
	rest := strings.TrimPrefix(text[3:], "\r")
	rest = strings.TrimPrefix(rest, "\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return meta, text
	}
	var lastKey string
	for _, line := range strings.Split(rest[:end], "\n") {
		line = strings.TrimRight(line, "\r")
		if k, v, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			lastKey = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if v == "|" || v == ">" || v == ">-" || v == "|-" {
				v = ""
			}
			meta[lastKey] = strings.Trim(v, `"'`)
		} else if lastKey != "" && strings.TrimSpace(line) != "" {
			// folded multi-line value
			meta[lastKey] = strings.TrimSpace(meta[lastKey] + " " + strings.TrimSpace(line))
		}
	}
	body := rest[end+4:]
	return meta, strings.TrimPrefix(body, "\n")
}

// Remove deletes an installed skill.
func (m *Manager) Remove(name string) error {
	s := m.Get(name)
	if s == nil {
		return fmt.Errorf("skill %q not found", name)
	}
	if s.Builtin {
		return fmt.Errorf("%q is a built-in skill and can't be removed; create a skill with the same name to replace it", name)
	}
	return os.RemoveAll(s.Dir)
}

// Create writes a new skill from a name, description and body.
func (m *Manager) Create(name, description, body string) (*Skill, error) {
	name = slug(name)
	if name == "" {
		return nil, fmt.Errorf("skill name required")
	}
	dir := filepath.Join(m.InstallDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", name, strings.ReplaceAll(description, "\n", " "), body)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		return nil, err
	}
	return Load(dir)
}

// Import installs skills from src, which may be:
//   - a local folder containing SKILL.md (or many skill folders)
//   - a local SKILL.md / .md file
//   - github:owner/repo[/path][@ref] or owner/repo shorthand
//   - https://github.com/owner/repo[/tree/ref/path]
//   - an https URL to a raw SKILL.md or .md file
//
// It returns the skills that were installed.
func (m *Manager) Import(ctx context.Context, src string) ([]*Skill, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("source required")
	}
	if info, err := os.Stat(expand(src)); err == nil {
		return m.importLocal(expand(src), info)
	}
	if owner, repo, ref, sub, ok := parseGitHub(src); ok {
		return m.importGitHub(ctx, owner, repo, ref, sub, src)
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return m.importURL(ctx, src)
	}
	return nil, fmt.Errorf("don't know how to import %q", src)
}

var parseGitHub = fetch.ParseGitHub

func (m *Manager) importLocal(path string, info fs.FileInfo) ([]*Skill, error) {
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return m.installSingleFile(string(data), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), path)
	}
	return m.installTree(path, path)
}

func (m *Manager) importURL(ctx context.Context, url string) ([]*Skill, error) {
	data, err := fetch.Get(ctx, url, 2<<20)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(strings.SplitN(url, "?", 2)[0])
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if strings.EqualFold(name, "SKILL") {
		name = filepath.Base(filepath.Dir(strings.SplitN(url, "?", 2)[0]))
	}
	return m.installSingleFile(string(data), name, url)
}

func (m *Manager) installSingleFile(content, fallbackName, source string) ([]*Skill, error) {
	meta, _ := parseFrontMatter(content)
	name := meta["name"]
	if name == "" {
		name = fallbackName
	}
	name = slug(name)
	if !strings.HasPrefix(content, "---") {
		content = fmt.Sprintf("---\nname: %s\ndescription: Imported from %s\n---\n\n%s", name, source, content)
	}
	dir := filepath.Join(m.InstallDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		return nil, err
	}
	_ = os.WriteFile(filepath.Join(dir, ".source"), []byte(source), 0o644)
	s, err := Load(dir)
	if err != nil {
		return nil, err
	}
	return []*Skill{s}, nil
}

// installTree copies every folder under root that contains a SKILL.md.
func (m *Manager) installTree(root, source string) ([]*Skill, error) {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			dirs = append(dirs, filepath.Dir(path))
		}
		return nil
	})
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no SKILL.md found in %s", source)
	}
	var out []*Skill
	for _, d := range dirs {
		s, err := Load(d)
		if err != nil {
			continue
		}
		dest := filepath.Join(m.InstallDir(), slug(s.Name))
		if abs(dest) == abs(d) {
			out = append(out, s)
			continue
		}
		_ = os.RemoveAll(dest)
		if err := fetch.CopyDir(d, dest); err != nil {
			return out, err
		}
		_ = os.WriteFile(filepath.Join(dest, ".source"), []byte(source), 0o644)
		if s, err := Load(dest); err == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *Manager) importGitHub(ctx context.Context, owner, repo, ref, sub, source string) ([]*Skill, error) {
	root, tmp, err := fetch.GitHub(ctx, owner, repo, ref)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if sub != "" {
		root = filepath.Join(root, filepath.FromSlash(sub))
		if info, err := os.Stat(root); err == nil && !info.IsDir() {
			data, _ := os.ReadFile(root)
			name := strings.TrimSuffix(filepath.Base(root), filepath.Ext(root))
			if strings.EqualFold(name, "SKILL") {
				return m.installTree(filepath.Dir(root), source)
			}
			return m.installSingleFile(string(data), name, source)
		}
	}
	return m.installTree(root, source)
}

var reSlug = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(s string) string {
	return strings.Trim(reSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-.")
}

func abs(p string) string { return fetch.Abs(p) }

func expand(p string) string { return fetch.Expand(p) }
