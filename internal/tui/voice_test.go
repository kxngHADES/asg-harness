package tui

import "testing"

func TestParseSpec(t *testing.T) {
	s, err := parseSpec([]string{"ollama", "phonon", "Captain", "Silas", "Blackwater"})
	if err != nil || s.Model != "phonon" || s.Voice != "Captain Silas Blackwater" {
		t.Fatalf("%+v %v", s, err)
	}
	if _, err := parseSpec([]string{"ollama"}); err == nil {
		t.Error("ollama without model should fail")
	}
	if _, err := parseSpec([]string{"banana"}); err == nil {
		t.Error("unknown engine should fail")
	}
	if s, _ := parseSpec([]string{"system", "Samantha"}); s.Voice != "Samantha" {
		t.Errorf("%+v", s)
	}
}

func TestNormalizeCommand(t *testing.T) {
	for in, want := range map[string]string{
		"//model llama3": "/model llama3",
		"clear":          "/clear",
		"EXIT":           "/exit",
		"clear the logs": "clear the logs",
		"/help":          "/help",
		"///etc/hosts":   "///etc/hosts",
	} {
		if got := normalizeCommand(in); got != want {
			t.Errorf("normalizeCommand(%q) = %q, want %q", in, got, want)
		}
	}
}
