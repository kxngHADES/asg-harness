package sprite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The README shows docs/asg-crab*.svg. They are generated from the sprite,
// so this test fails when they go stale; refresh them with
// UPDATE_SPRITES=1 go test ./internal/sprite/
func TestDocsSVGUpToDate(t *testing.T) {
	files := map[string]string{
		"asg-crab.svg":         SVG(Idle, 140, 8),
		"asg-crab-working.svg": SVG(Work, 110, 8),
	}
	for name, want := range files {
		p := filepath.Join("..", "..", "docs", name)
		if os.Getenv("UPDATE_SPRITES") != "" {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(want+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(p)
		if err != nil || strings.TrimSpace(strings.ReplaceAll(string(got), "\r\n", "\n")) != want {
			t.Errorf("docs/%s is stale; run UPDATE_SPRITES=1 go test ./internal/sprite/", name)
		}
	}
}

func TestSVGFrames(t *testing.T) {
	s := SVG(Work, 110, 8)
	if n := strings.Count(s, "<g "); n != len(Work) {
		t.Errorf("got %d frame groups, want %d", n, len(Work))
	}
	if !strings.Contains(s, "prefers-reduced-motion") {
		t.Error("animation should respect reduced motion")
	}
	if strings.Contains(SVG([]Pose{Rest}, 0, 8), "@keyframes") {
		t.Error("a single frame needs no animation")
	}
}
