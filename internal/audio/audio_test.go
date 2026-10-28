package audio

import (
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	got := Clean("## Title\nSee **this** [link](http://x) and `code`.\n```go\nx := 1\n```")
	if strings.ContainsAny(got, "#*`[") || !strings.Contains(got, "code omitted") || !strings.Contains(got, "link") {
		t.Fatalf("Clean = %q", got)
	}
}

func TestChunks(t *testing.T) {
	text := strings.Repeat("This is a sentence. ", 60)
	chunks := Chunks(text, 400)
	if len(chunks) < 3 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	if len(chunks[0]) > 200 {
		t.Errorf("first chunk should be short for fast start, got %d chars", len(chunks[0]))
	}
	for _, c := range chunks {
		if len(c) > 400 {
			t.Errorf("chunk too long: %d", len(c))
		}
	}
	if strings.Join(chunks, " ") != strings.TrimSpace(text) {
		t.Error("chunks lost text")
	}
	if got := Chunks(strings.Repeat("word ", 300), 100); len(got) < 10 {
		t.Errorf("long sentence not split: %d", len(got))
	}
}
