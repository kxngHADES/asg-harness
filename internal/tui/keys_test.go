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
	if !app.Yolo || !strings.Contains(r.prompt(), "yolo") || !strings.Contains(r.prompt(), "loop") {
		t.Errorf("ctrl+y should turn YOLO on and both should show in the prompt: yolo=%v prompt=%q", app.Yolo, r.prompt())
	}
	r.toggleYolo()
	if r.yolo() {
		t.Error("second toggle should turn YOLO off")
	}
}
