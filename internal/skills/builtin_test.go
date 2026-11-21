package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinsParse(t *testing.T) {
	want := map[string]bool{"design-first-dev": false, "frontend-design": false}
	for _, s := range Builtins() {
		if _, ok := want[s.Name]; ok {
			want[s.Name] = true
		}
		if !s.Builtin || s.Dir != "" || s.Source != "built-in" {
			t.Errorf("%s: builtin=%v dir=%q source=%q", s.Name, s.Builtin, s.Dir, s.Source)
		}
		if s.Description == "" || strings.HasPrefix(s.Body, "---") || len(s.Body) < 200 {
			t.Errorf("%s: front matter not parsed (desc %q)", s.Name, s.Description)
		}
	}
	for n, ok := range want {
		if !ok {
			t.Errorf("built-in %q missing", n)
		}
	}
}

func TestBuiltinsListedOverriddenAndProtected(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{Dirs: []string{dir}}

	if s := m.Get("design-first-dev"); s == nil || !s.Builtin {
		t.Fatalf("built-in not listed by an empty manager: %+v", s)
	}
	if err := m.Remove("design-first-dev"); err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatalf("removing a built-in: err = %v", err)
	}

	// A user skill with the same name wins, and only appears once.
	own := filepath.Join(dir, "frontend-design")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, "SKILL.md"), []byte("---\nname: frontend-design\ndescription: mine\n---\nmy rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range m.List() {
		if s.Name == "frontend-design" {
			n++
			if s.Builtin || s.Body != "my rules" {
				t.Errorf("user skill should replace the built-in: %+v", s)
			}
		}
	}
	if n != 1 {
		t.Errorf("frontend-design listed %d times", n)
	}
	// Removing the user copy brings the built-in back.
	if err := m.Remove("frontend-design"); err != nil {
		t.Fatal(err)
	}
	if s := m.Get("frontend-design"); s == nil || !s.Builtin {
		t.Errorf("built-in should be back after removing the override: %+v", s)
	}
}
