package tui

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"golang.org/x/term"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

func TestShiftTabIsRewritten(t *testing.T) {
	q := &inputQueue{r: bytes.NewReader([]byte("ab\x1b[Zc\x1b[A"))}
	buf := make([]byte, 64)
	n, _ := q.Read(buf)
	want := "ab" + string(keyShiftTab) + "c\x1b[A"
	if got := string(buf[:n]); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if k, _ := parseKey([]byte(string(keyShiftTab))); k.kind != kOther {
		t.Errorf("shift+tab must not type into menu filters, got %+v", k)
	}
}

// The line editor must hand both shortcuts to the key callback instead of
// swallowing them, and must not insert anything into the line.
func TestShortcutsReachCallback(t *testing.T) {
	in := &inputQueue{r: bytes.NewReader([]byte("hi\x1b[Z\x19!\r"))}
	tm := term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{in, io.Discard}, "")
	var got []rune
	tm.AutoCompleteCallback = func(line string, pos int, key rune) (string, int, bool) {
		if key == keyShiftTab || key == keyCtrlY {
			got = append(got, key)
			return line, pos, true
		}
		return "", 0, false
	}
	line, err := tm.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	if line != "hi!" {
		t.Errorf("line = %q, want %q", line, "hi!")
	}
	if len(got) != 2 || got[0] != keyShiftTab || got[1] != keyCtrlY {
		t.Errorf("callback saw %q", got)
	}
}

func TestShiftTabCyclesModeAndCtrlYTogglesYolo(t *testing.T) {
	t.Setenv("ASG_HOME", t.TempDir())
	store, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	app := agent.NewApp(store)
	r := &REPL{app: app, sess: app.NewSession(), in: &inputQueue{r: &keystrokes{keys: "\x1b[Z\x1b[Z\x19go\r"}}, out: io.Discard}
	r.t = term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{r.in, io.Discard}, "")
	r.t.AutoCompleteCallback = r.onKey
	line, err := r.t.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	if line != "go" {
		t.Errorf("shortcuts leaked into the line: %q", line)
	}
	if r.mode() != agent.ModeLoop {
		t.Errorf("two shift+tabs from build should reach loop, got %s", r.mode())
	}
	if p := strings.ToLower(r.prompt()); !app.Yolo || !strings.Contains(p, "yolo") || !strings.Contains(p, "loop") {
		t.Errorf("ctrl+y should turn YOLO on and both should show in the prompt: yolo=%v prompt=%q", app.Yolo, r.prompt())
	}
	r.toggleYolo()
	if r.yolo() {
		t.Error("second toggle should turn YOLO off")
	}
}

// Ctrl+T cycles the work domain, saves it as the default, and repeated
// mode/domain changes redraw one status strip in place instead of
// printing a new line per key press.
func TestCtrlTCyclesDomainAndRedrawsStripInPlace(t *testing.T) {
	t.Setenv("ASG_HOME", t.TempDir())
	store, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	app := agent.NewApp(store)
	r := &REPL{app: app, sess: app.NewSession(), in: &inputQueue{r: &keystrokes{keys: "\x14\x14\x1b[Zok\r"}}, out: io.Discard}
	var screen strings.Builder
	r.t = term.NewTerminal(struct {
		io.Reader
		io.Writer
	}{r.in, &screen}, "")
	r.t.AutoCompleteCallback = r.onKey
	line, err := r.t.ReadLine()
	if err != nil {
		t.Fatal(err)
	}
	if line != "ok" {
		t.Errorf("ctrl+t leaked into the line: %q", line)
	}
	if got := r.track(); got.Domain != agent.DomainCyber || got.Flavor != "red" {
		t.Errorf("two ctrl+t from software should reach cyber/red, got %s", got)
	}
	if c := store.Get(); c.Domain != "cyber" || c.Flavor != "red" {
		t.Errorf("track not saved as default: %q/%q", c.Domain, c.Flavor)
	}
	if !strings.Contains(r.prompt(), "cyber/red") {
		t.Errorf("prompt should show the track: %q", r.prompt())
	}
	// First change prints the strip; the next two move up and overwrite it.
	if n := strings.Count(screen.String(), "\x1b[1A\r\x1b[2K"); n != 2 {
		t.Errorf("want 2 in-place strip redraws, got %d in %q", n, screen.String())
	}
	if s := r.statusStrip(); !strings.Contains(s, "cyber/red") || !strings.Contains(s, "plan") {
		t.Errorf("strip should show the modes and the track: %q", s)
	}
}
