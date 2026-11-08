package tui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/kxngHADES/asg-harness/internal/sprite"
)

func TestParseKey(t *testing.T) {
	cases := []struct {
		in   string
		kind keyKind
		used int
	}{
		{"\x1b[A", kUp, 3},
		{"\x1bOB", kDown, 3},
		{"\x1b", kEsc, 1},
		{"\r\n", kEnter, 2},
		{"\x7f", kBack, 1},
		{"\x1b[1;5C", kOther, 6},
		{"ém", kRune, 2},
	}
	for _, c := range cases {
		k, n := parseKey([]byte(c.in))
		if k.kind != c.kind || n != c.used {
			t.Errorf("%q: got kind %d used %d, want %d %d", c.in, k.kind, n, c.kind, c.used)
		}
	}
}

func runPicker(p *picker, keys string) (*menuItem, int) {
	q := &inputQueue{r: strings.NewReader(keys)}
	return p.run(q, &bytes.Buffer{})
}

func items(labels ...string) []menuItem {
	var out []menuItem
	for _, l := range labels {
		out = append(out, menuItem{Label: l, Value: l})
	}
	return out
}

func TestPickerSlashMenu(t *testing.T) {
	cmds := items("/help", "/model", "/models", "/provider", "/memory")

	it, how := runPicker(&picker{items: cmds, slash: true, rows: 8}, "mod\r")
	if how != pickChose || it.Value != "/model" {
		t.Errorf("filter+enter chose %v %d", it, how)
	}
	it, _ = runPicker(&picker{items: cmds, slash: true, rows: 8}, "\x1b[B\x1b[B\r")
	if it.Value != "/models" {
		t.Errorf("arrow down twice chose %v", it)
	}
	it, _ = runPicker(&picker{items: cmds, slash: true, rows: 8}, "\x1b[A\r")
	if it.Value != "/memory" {
		t.Errorf("arrow up should wrap to last, got %v", it)
	}
	p := &picker{items: cmds, slash: true, rows: 8}
	if _, how := runPicker(p, "model "); how != pickTyped || p.filter != "model " {
		t.Errorf("space should hand back typed text, got %d %q", how, p.filter)
	}
	if _, how := runPicker(&picker{items: cmds, slash: true, rows: 8}, "\x7f"); how != pickCancel {
		t.Errorf("backspace on empty filter should close the menu")
	}
	if _, how := runPicker(&picker{items: cmds, slash: true, rows: 8}, "zzz\r"); how != pickTyped {
		t.Errorf("enter with no matches should submit typed text")
	}
}

func TestPickerKeepsBytesAfterKey(t *testing.T) {
	q := &inputQueue{r: strings.NewReader("\x1b[Bxy")}
	k, _ := q.readKey()
	if k.kind != kDown {
		t.Fatalf("got %v", k)
	}
	k, _ = q.readKey()
	if k.kind != kRune || k.r != 'x' {
		t.Errorf("bytes after a key must not be lost, got %v", k)
	}
}

func TestPetFrames(t *testing.T) {
	// Every frame must draw as exactly spriteRows rows of spriteW cells, or
	// the redraw-in-place animation would drift.
	for _, anim := range [][]sprite.Pose{sprite.Enter, sprite.Idle, sprite.Work, {sprite.Rest}} {
		for _, p := range anim {
			rows := renderSprite(sprite.Compose(p))
			if len(rows) != spriteRows {
				t.Fatalf("want %d rows, got %d", spriteRows, len(rows))
			}
			for _, r := range rows {
				if w := visibleLen(r); w != spriteW {
					t.Errorf("row is %d cells wide, want %d", w, spriteW)
				}
			}
		}
	}
	frame := workFrame(5, "thinking", 0)
	if n := strings.Count(frame, "\n"); n != spriteRows {
		t.Errorf("work frame has %d lines, want %d", n, spriteRows)
	}
	if !strings.HasSuffix(frame, fmt.Sprintf("\x1b[%dA\r", spriteRows)) {
		t.Error("work frame must return the cursor to its first row")
	}
	for i := 0; i < 60; i++ {
		if w := visibleLen(petTrack(i)); w != 12 {
			t.Fatalf("spinner frame %d is %d wide, want 12", i, w)
		}
	}
}
