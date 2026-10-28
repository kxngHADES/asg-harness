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
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Dir         string `json:"dir"`
	Body        string `json:"-"`
	Source      string `json:"source,omitempty"`
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
	var out []*Skill
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

var reGitHub = regexp.MustCompile(`^(?:https?://)?github\.com/([^/]+)/([^/#?]+?)(?:\.git)?(?:/(?:tree|blob)/([^/]+)(?:/(.*))?)?/?$`)

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

func parseGitHub(src string) (owner, repo, ref, sub string, ok bool) {
	if strings.HasPrefix(src, "github:") || (!strings.Contains(src, "://") && !strings.HasPrefix(src, "github.com") && strings.Count(src, "/") >= 1 && !strings.HasPrefix(src, ".") && !strings.HasPrefix(src, "/")) {
		s := strings.TrimPrefix(src, "github:")
		if i := strings.LastIndex(s, "@"); i > 0 {
			ref = s[i+1:]
			s = s[:i]
		}
		parts := strings.SplitN(s, "/", 3)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return "", "", "", "", false
		}
		owner, repo = parts[0], strings.TrimSuffix(parts[1], ".git")
		if len(parts) == 3 {
			sub = parts[2]
		}
		return owner, repo, ref, sub, true
	}
	if mm := reGitHub.FindStringSubmatch(src); mm != nil {
		return mm[1], mm[2], mm[3], mm[4], true
	}
	return "", "", "", "", false
}

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
	data, err := httpGet(ctx, url, 2<<20)
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
		if err := copyDir(d, dest); err != nil {
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
	tmp, err := os.MkdirTemp("", "asg-skill-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)

	refs := []string{ref}
	if ref == "" {
		refs = []string{"HEAD", "main", "master"}
	}
	var lastErr error
	for _, r := range refs {
		url := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, repo, r)
		if lastErr = downloadTarGz(ctx, url, tmp); lastErr == nil {
			break
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("download github.com/%s/%s: %w", owner, repo, lastErr)
	}
	// The archive contains a single top-level folder: repo-ref/
	entries, _ := os.ReadDir(tmp)
	root := tmp
	if len(entries) == 1 && entries[0].IsDir() {
		root = filepath.Join(tmp, entries[0].Name())
	}
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

func downloadTarGz(ctx context.Context, url, dest string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s", resp.Status)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	destAbs := abs(dest)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(hdr.Name))
		// guard against path traversal in archive entries
		if !strings.HasPrefix(abs(target), destAbs+string(os.PathSeparator)) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			_ = os.MkdirAll(target, 0o755)
		case tar.TypeReg:
			if hdr.Size > 50<<20 {
				continue
			}
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			f.Close()
			if err != nil {
				return err
			}
		}
	}
}

func httpGet(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

var reSlug = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(s string) string {
	return strings.Trim(reSlug.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-.")
}

func abs(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
