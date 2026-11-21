package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxngHADES/asg-harness/internal/llm"
)

func TestNextAfter(t *testing.T) {
	loc := time.Local
	// Thursday 2026-11-19 10:30
	now := time.Date(2026, 11, 19, 10, 30, 0, 0, loc)
	tests := []struct {
		name string
		s    Schedule
		want time.Time
	}{
		{"daily later today", Schedule{Every: "daily", At: "18:00"}, time.Date(2026, 11, 19, 18, 0, 0, 0, loc)},
		{"daily passed", Schedule{Every: "daily", At: "09:00"}, time.Date(2026, 11, 20, 9, 0, 0, 0, loc)},
		{"daily exactly now", Schedule{Every: "daily", At: "10:30"}, time.Date(2026, 11, 20, 10, 30, 0, 0, loc)},
		{"hourly", Schedule{Every: "hourly"}, time.Date(2026, 11, 19, 11, 0, 0, 0, loc)},
		{"minutes", Schedule{Every: "minutes", Interval: 15}, time.Date(2026, 11, 19, 10, 45, 0, 0, loc)},
		{"minutes floor", Schedule{Every: "minutes", Interval: 1}, time.Date(2026, 11, 19, 10, 35, 0, 0, loc)},
		{"weekly monday", Schedule{Every: "weekly", Weekday: 1, At: "08:00"}, time.Date(2026, 11, 23, 8, 0, 0, 0, loc)},
		{"weekly today later", Schedule{Every: "weekly", Weekday: 4, At: "12:00"}, time.Date(2026, 11, 19, 12, 0, 0, 0, loc)},
		{"weekdays skips weekend", Schedule{Every: "weekdays", At: "09:00"}, time.Date(2026, 11, 20, 9, 0, 0, 0, loc)},
		{"bad clock is 09:00", Schedule{Every: "daily", At: "25:99"}, time.Date(2026, 11, 20, 9, 0, 0, 0, loc)},
	}
	for _, tt := range tests {
		if got := tt.s.NextAfter(now); !got.Equal(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
	// Friday evening → Monday for weekdays.
	fri := time.Date(2026, 11, 20, 20, 0, 0, 0, loc)
	if got := (Schedule{Every: "weekdays", At: "09:00"}).NextAfter(fri); got.Weekday() != time.Monday {
		t.Errorf("weekdays after Friday: got %v", got.Weekday())
	}
}

func TestDescribe(t *testing.T) {
	cases := map[string]Schedule{
		"Daily at 07:05":    {Every: "daily", At: "7:05"},
		"Every hour":        {Every: "hourly"},
		"Every 30 minutes":  {Every: "minutes", Interval: 30},
		"Mondays at 09:00":  {Every: "weekly", Weekday: 1, At: "09:00"},
		"Weekdays at 08:30": {Every: "weekdays", At: "08:30"},
	}
	for want, s := range cases {
		if got := s.Describe(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestArtifactKind(t *testing.T) {
	cases := map[string]string{".html": "html", "MD": "markdown", ".svg": "svg", ".PNG": "image", ".pdf": "pdf",
		".docx": "document", ".csv": "data", ".go": "code", ".txt": "text", "": "text"}
	for ext, want := range cases {
		if got := ArtifactKind(ext); got != want {
			t.Errorf("ArtifactKind(%q) = %q, want %q", ext, got, want)
		}
	}
}

func TestValidIDAndWithin(t *testing.T) {
	for _, id := range []string{"s1", "abc-def_9"} {
		if !validID(id) {
			t.Errorf("%q should be valid", id)
		}
	}
	for _, id := range []string{"", "../x", "a/b", `a\b`, "A"} {
		if validID(id) {
			t.Errorf("%q should be invalid", id)
		}
	}
	dir := t.TempDir()
	if !within(dir, filepath.Join(dir, "a", "b.txt")) {
		t.Error("child should be within")
	}
	if within(dir, filepath.Join(dir, "..", "x.txt")) {
		t.Error("parent escape should not be within")
	}
	if within(dir, dir+"-other/x") {
		t.Error("sibling with shared prefix should not be within")
	}
}

func TestAutoTitle(t *testing.T) {
	if got := autoTitle("  fix the   login bug  "); got != "fix the login bug" {
		t.Errorf("got %q", got)
	}
	long := autoTitle("please write a very long description of how the scheduler in this application decides when to run")
	if len([]rune(long)) > 54 || long[len(long)-3:] != "…" {
		t.Errorf("long title not clipped: %q", long)
	}
}

func TestStoreSessionsAndArtifacts(t *testing.T) {
	s := &Store{dir: t.TempDir()}
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	old := &SessionFile{SessionMeta: SessionMeta{ID: "s1", Title: "old", Kind: KindChat, Updated: time.Now().Add(-time.Hour)}}
	pinned := &SessionFile{SessionMeta: SessionMeta{ID: "s2", Title: "pinned", Kind: KindCode, Pinned: true, Updated: time.Now().Add(-2 * time.Hour)}}
	fresh := &SessionFile{SessionMeta: SessionMeta{ID: "s3", Title: "new", Kind: KindArtifact, Updated: time.Now()},
		History: []llm.Message{{Role: llm.RoleUser, Content: "hi"}, {Role: llm.RoleAssistant, Content: "hello"},
			{Role: llm.RoleTool, Content: "x"}, {Role: llm.RoleUser, Content: "[1 image(s) returned by the tool calls above — look at them]"}}}
	for _, f := range []*SessionFile{old, pinned, fresh} {
		if err := s.SaveSession(f); err != nil {
			t.Fatal(err)
		}
	}
	list := s.ListSessions()
	if len(list) != 3 || list[0].ID != "s2" || list[1].ID != "s3" || list[2].ID != "s1" {
		t.Fatalf("order wrong: %+v", list)
	}
	got, err := s.LoadSession("s3")
	if err != nil || len(got.History) != 4 || got.Messages != 2 {
		t.Fatalf("load: %v %+v", err, got)
	}

	src := filepath.Join(t.TempDir(), "page.html")
	os.WriteFile(src, []byte("<h1>v1</h1>"), 0o644)
	a1, err := s.Capture(fresh.SessionMeta, src)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(src, []byte("<h1>v2</h1>"), 0o644)
	a2, err := s.Capture(fresh.SessionMeta, src)
	if err != nil {
		t.Fatal(err)
	}
	if a1.ID != a2.ID || a2.Versions != 2 || a2.Kind != "html" {
		t.Fatalf("second write should add a version: %+v", a2)
	}
	data, _ := os.ReadFile(s.ArtifactFile(a2))
	if string(data) != "<h1>v2</h1>" {
		t.Errorf("snapshot not updated: %s", data)
	}
	os.Remove(src)
	if _, err := os.Stat(s.ArtifactFile(a2)); err != nil {
		t.Error("snapshot should survive the original being deleted")
	}

	if err := s.DeleteSession("s3"); err != nil {
		t.Fatal(err)
	}
	if len(s.ListSessions()) != 2 {
		t.Error("delete did not remove the session")
	}
	if len(s.Artifacts()) != 1 {
		t.Error("artifacts should outlive their session")
	}
}

func TestMoveData(t *testing.T) {
	root := t.TempDir()
	s := &Store{dir: filepath.Join(root, "a")}
	s.Init()
	s.SaveSession(&SessionFile{SessionMeta: SessionMeta{ID: "s1", Kind: KindChat}})
	if err := s.MoveData(filepath.Join(root, "a", "inner")); err == nil {
		t.Error("moving into itself should fail")
	}
	busy := filepath.Join(root, "busy")
	os.MkdirAll(busy, 0o755)
	os.WriteFile(filepath.Join(busy, "x"), nil, 0o644)
	if err := s.MoveData(busy); err == nil {
		t.Error("moving into a non-empty folder should fail")
	}
	dst := filepath.Join(root, "b")
	if err := s.MoveData(dst); err != nil {
		t.Fatal(err)
	}
	if s.Dir() != dst || len(s.ListSessions()) != 1 {
		t.Error("data not available at the new place")
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Error("old folder should be gone")
	}
}

func TestItemsAndUserText(t *testing.T) {
	if got := userText("# Skill: notes\n(Skill files…)\n\nbody\n\n---\nUser request: do it"); got != "/notes do it" {
		t.Errorf("userText: %q", got)
	}
	if !toolFailed("error: boom") || toolFailed("ok") {
		t.Error("toolFailed")
	}
}

func TestRenderMarkdownIsSafe(t *testing.T) {
	out := RenderMarkdown("hi <script>alert(1)</script>\n\n```go\nfunc main() {}\n```")
	if strings.Contains(out, "<script>") {
		t.Errorf("raw HTML leaked: %s", out)
	}
	if !strings.Contains(out, `class="codeblock"`) || !strings.Contains(out, "data-copy") {
		t.Errorf("code block not highlighted: %s", out)
	}
}
