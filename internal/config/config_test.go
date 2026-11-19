package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestOldToolRoundCapIsDropped(t *testing.T) {
	for _, tc := range []struct{ saved, want int }{{25, 0}, {-1, 0}, {0, 0}, {40, 40}} {
		home := t.TempDir()
		t.Setenv("ASG_HOME", home)
		body := []byte(`{"active_provider":"ollama","providers":{"ollama":{"type":"ollama"}},"max_tool_rounds":` + fmt.Sprint(tc.saved) + `}`)
		if err := os.WriteFile(filepath.Join(home, "config.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
		s, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Get().MaxToolRounds; got != tc.want {
			t.Errorf("saved %d: want %d, got %d", tc.saved, tc.want, got)
		}
	}
}
