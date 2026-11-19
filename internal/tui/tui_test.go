package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/sprite"
)

func items(labels ...string) []menuItem {
	var out []menuItem
	for _, l := range labels {
		out = append(out, menuItem{Label: l, Value: l})
	}
	return out
}

func labels(its []menuItem) string {
	var out []string
	for _, it := range its {
		out = append(out, it.Label)
	}
	return strings.Join(out, " ")
}

func TestFilterItems(t *testing.T) {
	cmds := items("/help", "/model", "/models", "/provider", "/memory")
	cmds[3].Detail = "pick a model provider"
	if got := labels(filterItems(cmds, "/mem")); got != "/memory" {
		t.Errorf("prefix filter: %q", got)
	}
	if got := labels(filterItems(cmds, "/model")); got != "/model /models /provider" {
		t.Errorf("prefix matches first, then detail matches: %q", got)
	}
	if got := labels(filterItems(items("/model", "/mode", "/models"), "/mode")); got != "/mode /model /models" {
		t.Errorf("an exact match must come first, got %q", got)
	}
	if got := len(filterItems(cmds, "/")); got != len(cmds) {
		t.Errorf("empty filter keeps everything, got %d", got)
	}
}

func TestSlashFilter(t *testing.T) {
	for in, open := range map[string]bool{
		"/":             true,
		"/mod":          true,
		"/model llama3": false, // typing arguments closes the menu
		"//etc":         false,
		"hello":         false,
		"/a\nb":         false,
	} {
		if _, ok := slashFilter(in); ok != open {
			t.Errorf("slashFilter(%q) open=%v, want %v", in, ok, open)
		}
	}
}

func TestSplitBlock(t *testing.T) {
	cases := []struct{ in, block, rest string }{
		{"para one\n\npara two", "para one\n", "para two"},
		{"no blank line yet\nstill going", "", "no blank line yet\nstill going"},
		{"```go\nx := 1\n\ny := 2\n```\nafter", "```go\nx := 1\n\ny := 2\n```\n", "after"},
		{"\n\nlead\n\nnext", "\n\nlead\n", "next"},
		{"partial line", "", "partial line"},
	}
	for _, c := range cases {
		b, rest, ok := splitBlock(c.in)
		if ok != (c.block != "") || b != c.block || rest != c.rest {
			t.Errorf("splitBlock(%q) = %q, %q, %v; want %q, %q", c.in, b, rest, ok, c.block, c.rest)
		}
	}
}

func TestRenderMarkdownKeepsText(t *testing.T) {
	out := stripANSI(renderMarkdown("# Title\n\nSome **bold** and `code`.\n\n- one\n- two", 60))
	for _, want := range []string{"Title", "bold", "code", "one", "two"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered markdown lost %q:\n%s", want, out)
		}
	}
	if strings.HasPrefix(out, "\n") || strings.HasSuffix(out, "\n") {
		t.Errorf("rendered markdown should be trimmed: %q", out)
	}
}

func TestPetFrames(t *testing.T) {
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
	block := spriteBlock(renderSprite(sprite.Compose(sprite.Rest)), []string{"a", "b"}, 2, 3)
	if n := strings.Count(block, "\n") + 1; n != spriteRows {
		t.Errorf("sprite block has %d lines, want %d", n, spriteRows)
	}
	c := newCrab(float64(trackCells - crabCells))
	for i := 0; i < 200; i++ {
		c.step()
		if w := visibleLen(petTrack(i, c.cell(), c.right())); w != trackCells {
			t.Fatalf("crab track frame %d is %d wide, want %d", i, w, trackCells)
		}
		if p := c.cell(); p < 0 || p > trackCells-crabCells {
			t.Fatalf("crab left its track: %d", p)
		}
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

// --- the Bubble Tea model ----------------------------------------------------

func testModel(t *testing.T) *model {
	t.Helper()
	t.Setenv("ASG_HOME", t.TempDir())
	t.Setenv("ASG_NO_ANIMATION", "1")
	store, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(New(agent.NewApp(store)))
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func press(m *model, keys ...tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(k)
	}
	return cmd
}

func typed(s string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}

var (
	kEnter    = tea.KeyPressMsg{Code: tea.KeyEnter}
	kDown     = tea.KeyPressMsg{Code: tea.KeyDown}
	kUp       = tea.KeyPressMsg{Code: tea.KeyUp}
	kTab      = tea.KeyPressMsg{Code: tea.KeyTab}
	kShiftTab = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	kCtrlT    = tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
	kCtrlY    = tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	kCtrlC    = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
)

func TestShortcuts(t *testing.T) {
	m := testModel(t)
	if m.r.mode() != agent.ModeBuild {
		t.Fatalf("starts in %s", m.r.mode())
	}
	press(m, kShiftTab)
	if m.r.mode() != agent.ModeBuild.Next() {
		t.Errorf("shift+tab should cycle the mode, got %s", m.r.mode())
	}
	d := m.r.track().Domain
	press(m, kCtrlT)
	if m.r.track().Domain == d {
		t.Error("ctrl+t should cycle the domain")
	}
	if m.input.Value() != "" {
		t.Errorf("ctrl+t must not edit the input (textarea transpose), got %q", m.input.Value())
	}
	press(m, kCtrlY)
	if !m.r.yolo() {
		t.Error("ctrl+y should turn YOLO on")
	}
	if !strings.Contains(stripANSI(m.r.statusStrip(100)), "YOLO") {
		t.Error("the status strip should show YOLO")
	}
	press(m, kCtrlY)
	if m.r.yolo() {
		t.Error("ctrl+y again should turn YOLO off")
	}
}

func TestSlashMenu(t *testing.T) {
	m := testModel(t)
	press(m, typed("/dash")...)
	vis, open := m.menu()
	if !open || len(vis) == 0 || vis[0].Value != "/dashboard" {
		t.Fatalf("menu should be open on /dashboard, got open=%v %v", open, labels(vis))
	}
	if !strings.Contains(stripANSI(m.View().Content), "/dashboard") {
		t.Error("the dropdown should be drawn")
	}

	m = testModel(t)
	press(m, typed("/ima")...)
	press(m, kEnter)
	if m.input.Value() != "/image " || m.working {
		t.Errorf("commands needing args are inserted for editing, got %q working=%v", m.input.Value(), m.working)
	}

	m = testModel(t)
	press(m, typed("/")...)
	press(m, kDown, kTab)
	if m.input.Value() != commands[1].name {
		t.Errorf("down + tab should complete the second command, got %q", m.input.Value())
	}

	m = testModel(t)
	press(m, typed("/model llama3")...)
	if _, open := m.menu(); open {
		t.Error("typing arguments should close the menu")
	}
	if cmd := press(m, kEnter); cmd == nil || !m.working || m.input.Value() != "" {
		t.Errorf("enter should submit; working=%v input=%q", m.working, m.input.Value())
	}
}

func TestBusyAndHistory(t *testing.T) {
	m := testModel(t)
	press(m, typed("first")...)
	press(m, kEnter)
	if !m.working {
		t.Fatal("submitting should start the worker")
	}
	press(m, typed("second")...)
	if cmd := press(m, kEnter); cmd != nil {
		t.Error("enter while a turn runs must not start another")
	}
	press(m, kCtrlC)
	if m.note == "" {
		t.Error("ctrl+c during a turn should say it is stopping")
	}
	m.Update(workerDoneMsg{})
	if m.working {
		t.Fatal("worker done should clear working")
	}
	m.input.Reset()
	press(m, kUp)
	if m.input.Value() != "first" {
		t.Errorf("up should recall the last input, got %q", m.input.Value())
	}
}

func TestBusySpinnerIDs(t *testing.T) {
	m := testModel(t)
	m.Update(busyMsg{id: 1, label: "thinking"})
	m.Update(busyMsg{id: 2, label: "running bash"})
	m.Update(busyMsg{id: 1}) // a stale stop must not hide the newer spinner
	if m.busyLabel != "running bash" {
		t.Errorf("busy label = %q", m.busyLabel)
	}
	if !strings.Contains(stripANSI(m.View().Content), "running bash") {
		t.Error("the working animation should show its label")
	}
	m.Update(busyMsg{id: 2})
	if m.busyLabel != "" {
		t.Error("stopping the current spinner should clear it")
	}
}
