package memory

import (
	"os"
	"strings"
	"testing"
)

func TestSaveListDelete(t *testing.T) {
	home := t.TempDir()
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	m := New(home)

	if _, err := m.Save(User, "Testing Framework", "# Tests\nAlways write tests with pytest."); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Save(Project, "deploy", "Deploys go through fly.io."); err != nil {
		t.Fatal(err)
	}
	// Saving the same name replaces the note.
	if _, err := m.Save(User, "testing-framework", "Use unittest, not pytest."); err != nil {
		t.Fatal(err)
	}

	notes := New(home).List()
	if len(notes) != 2 {
		t.Fatalf("got %d notes, want 2: %+v", len(notes), notes)
	}
	if notes[0].Name != "testing-framework" || notes[0].Scope != User || notes[0].Body != "Use unittest, not pytest." {
		t.Errorf("user note = %+v", notes[0])
	}
	if notes[1].Name != "deploy" || notes[1].Scope != Project {
		t.Errorf("project note = %+v", notes[1])
	}

	p := m.Prompt(10000)
	if !strings.Contains(p, "Use unittest, not pytest.") || !strings.Contains(p, "fly.io") {
		t.Errorf("prompt missing notes:\n%s", p)
	}
	if p := m.Prompt(5); !strings.Contains(p, "not loaded; read") {
		t.Errorf("over-budget notes should be listed by path:\n%s", p)
	}

	if err := m.Delete("", "deploy"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete("", "deploy"); err == nil {
		t.Error("deleting a missing note should fail")
	}
	if _, err := m.Save(User, "../../evil", "x"); err != nil {
		t.Fatal(err)
	}
	for _, n := range m.List() {
		if strings.Contains(n.Name, ".") {
			t.Errorf("unsafe name kept: %q", n.Name)
		}
	}
}
