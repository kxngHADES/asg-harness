package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseGitHub(t *testing.T) {
	cases := []struct{ in, owner, repo, ref, sub string }{
		{"github:anthropics/skills", "anthropics", "skills", "", ""},
		{"github:anthropics/skills/skills/pdf@main", "anthropics", "skills", "main", "skills/pdf"},
		{"anthropics/skills/skills/pdf", "anthropics", "skills", "", "skills/pdf"},
		{"https://github.com/anthropics/skills/tree/main/skills/docx", "anthropics", "skills", "main", "skills/docx"},
		{"https://github.com/anthropics/skills.git", "anthropics", "skills", "", ""},
	}
	for _, c := range cases {
		o, r, ref, sub, ok := parseGitHub(c.in)
		if !ok || o != c.owner || r != c.repo || ref != c.ref || sub != c.sub {
			t.Errorf("parseGitHub(%q) = %q %q %q %q %v", c.in, o, r, ref, sub, ok)
		}
	}
	if _, _, _, _, ok := parseGitHub("https://example.com/SKILL.md"); ok {
		t.Error("plain URL parsed as GitHub")
	}
}

func TestFrontMatter(t *testing.T) {
	meta, body := parseFrontMatter("---\nname: demo\ndescription: >\n  folded\n  text\n---\nHello")
	if meta["name"] != "demo" || meta["description"] != "folded text" || body != "Hello" {
		t.Fatalf("got %v %q", meta, body)
	}
}

func TestImportLocalTree(t *testing.T) {
	src := t.TempDir()
	for _, n := range []string{"a", "nested/b"} {
		dir := filepath.Join(src, n)
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+filepath.Base(n)+"\ndescription: d\n---\nbody"), 0o644)
	}
	m := &Manager{Dirs: []string{t.TempDir()}}
	got, err := m.Import(context.Background(), src)
	if err != nil || len(got) != 2 {
		t.Fatalf("import: %v %v", got, err)
	}
	if len(m.List()) != 2 || m.Get("b") == nil {
		t.Fatalf("list: %v", m.List())
	}
	if err := m.Remove("a"); err != nil || m.Get("a") != nil {
		t.Fatalf("remove: %v", err)
	}
}
