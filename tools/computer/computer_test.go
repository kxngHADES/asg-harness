package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

func TestParseKeys(t *testing.T) {
	tests := []struct {
		in   string
		want string // chords joined by spaces
		err  bool
	}{
		{"ctrl+c", "ctrl+c", false},
		{"Control+Shift+T", "ctrl+shift+t", false},
		{"shift+ctrl+t", "ctrl+shift+t", false},
		{"cmd+space", "super+space", false},
		{"Return", "enter", false},
		{"alt+F4", "alt+f4", false},
		{"ctrl+a ctrl+c", "ctrl+a ctrl+c", false},
		{"ctrl++", "ctrl++", false},
		{"+", "+", false},
		{"page_down", "pagedown", false},
		{"ctrl+a+b", "", true},
		{"hyper", "", true},
		{"f25", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		got, err := ParseKeys(tt.in)
		if (err != nil) != tt.err {
			t.Errorf("ParseKeys(%q) error = %v, want error %v", tt.in, err, tt.err)
			continue
		}
		var s []string
		for _, c := range got {
			s = append(s, c.String())
		}
		if strings.Join(s, " ") != tt.want {
			t.Errorf("ParseKeys(%q) = %q, want %q", tt.in, strings.Join(s, " "), tt.want)
		}
	}
}

func TestSensitiveKeys(t *testing.T) {
	for in, want := range map[string]bool{"alt+f4": true, "ctrl+w": true, "shift+ctrl+delete": true, "cmd+q": true, "ctrl+c": false, "enter": false} {
		if got := sensitiveKeys(in); got != want {
			t.Errorf("sensitiveKeys(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestViewMapping(t *testing.T) {
	v := newView(1, Rect{X: 2560, Y: 0, W: 2560, H: 1440}, 1280)
	if v.ImgW != 1280 || v.ImgH != 720 {
		t.Fatalf("image size %dx%d", v.ImgW, v.ImgH)
	}
	for _, tt := range []struct{ ix, iy, sx, sy int }{{0, 0, 2560, 0}, {640, 360, 3840, 720}, {1279, 719, 5118, 1438}} {
		sx, sy := v.toScreen(tt.ix, tt.iy)
		if sx != tt.sx || sy != tt.sy {
			t.Errorf("toScreen(%d,%d) = (%d,%d), want (%d,%d)", tt.ix, tt.iy, sx, sy, tt.sx, tt.sy)
		}
		if ix, iy, ok := v.toImage(sx, sy); !ok || ix != tt.ix || iy != tt.iy {
			t.Errorf("toImage(%d,%d) = (%d,%d,%v), want (%d,%d)", sx, sy, ix, iy, ok, tt.ix, tt.iy)
		}
	}
	if _, _, ok := v.toImage(100, 100); ok {
		t.Error("a point on another monitor mapped into this view")
	}
	small := newView(0, Rect{0, 0, 1024, 768}, 1280)
	if small.ImgW != 1024 || small.ImgH != 768 {
		t.Errorf("small screens must not be upscaled: %dx%d", small.ImgW, small.ImgH)
	}
	if r := v.rectToImage(Rect{2560, 0, 256, 144}); r != image.Rect(0, 0, 128, 72) {
		t.Errorf("rectToImage = %v", r)
	}
}

func TestChanged(t *testing.T) {
	a := make([]uint8, 100)
	b := make([]uint8, 100)
	if changed(a, b) != 0 {
		t.Error("identical thumbs changed")
	}
	for i := 0; i < 25; i++ {
		b[i] = 200
	}
	if got := changed(a, b); got != 0.25 {
		t.Errorf("changed = %v, want 0.25", got)
	}
	if changed(nil, b) != -1 {
		t.Error("no previous screenshot should report -1")
	}
}

func TestMatchesApp(t *testing.T) {
	w := Window{App: "KeePassXC.exe", Title: "Passwords.kdbx - KeePassXC"}
	if !isASG(Window{App: "ASG-Desktop.exe"}) || isASG(Window{App: "code.exe"}) {
		t.Error("isASG")
	}
	if !matchesApp(w, []string{"keepassxc"}) || !matchesApp(w, []string{"passwords.kdbx"}) || matchesApp(w, []string{"chrome", ""}) {
		t.Error("matchesApp")
	}
}

// fakeDriver records input on a two-monitor desktop.
type fakeDriver struct {
	mu      sync.Mutex
	x, y    int
	log     []string
	windows []Window
}

func (f *fakeDriver) Monitors() ([]Monitor, error) {
	return []Monitor{{Name: "right", Rect: Rect{1920, 0, 1920, 1080}}, {Name: "main", Rect: Rect{0, 0, 1920, 1080}, Primary: true}}, nil
}
func (f *fakeDriver) Capture(r Rect) (image.Image, error) {
	img := image.NewRGBA(image.Rect(0, 0, r.W, r.H))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.Black)
	return img, nil
}
func (f *fakeDriver) Cursor() (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.x, f.y, nil
}
func (f *fakeDriver) Move(x, y int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.x, f.y = x, y
	return nil
}
func (f *fakeDriver) add(s string) error {
	f.mu.Lock()
	f.log = append(f.log, s)
	f.mu.Unlock()
	return nil
}
func (f *fakeDriver) Button(b Button, down bool) error {
	f.mu.Lock()
	x, y := f.x, f.y
	f.mu.Unlock()
	return f.add(fmt.Sprintf("%s %v @%d,%d", b, down, x, y))
}
func (f *fakeDriver) Scroll(dx, dy int) error       { return f.add(fmt.Sprintf("scroll %d %d", dx, dy)) }
func (f *fakeDriver) Type(s string) error           { return f.add("type " + s) }
func (f *fakeDriver) Key(n string, down bool) error { return f.add(fmt.Sprintf("key %s %v", n, down)) }
func (f *fakeDriver) Windows() ([]Window, error)    { return f.windows, nil }
func (f *fakeDriver) Focus(id string) error         { return f.add("focus " + id) }
func (f *fakeDriver) Open(t string) error           { return f.add("open " + t) }
func (f *fakeDriver) entries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}
func (f *fakeDriver) setWindows(ws []Window) { f.mu.Lock(); f.windows = ws; f.mu.Unlock() }
func (f *fakeDriver) clear()                 { f.mu.Lock(); f.log = nil; f.mu.Unlock() }
func (f *fakeDriver) String() string         { return strings.Join(f.entries(), "\n") }
func (f *fakeDriver) has(s string) bool      { return strings.Contains(f.String(), s) }
func (f *fakeDriver) count(prefix string) (n int) {
	for _, e := range f.entries() {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

func setup(t *testing.T, cc config.ComputerConfig) (*fakeDriver, *tools.Tool, context.Context) {
	t.Helper()

	fd := &fakeDriver{windows: []Window{{ID: "w1", Title: "Editor", App: "code.exe", Rect: Rect{0, 0, 1920, 1080}, Focused: true}}}
	old := sys
	sys = fd
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		ctl.mu.Lock()
		r := ctl.run
		ctl.mu.Unlock()
		if r != nil {
			ctl.end(r)
		}
		// Wait for any watcher still ending a run before swapping the driver.
		for i := 0; i < 200 && Current().Active; i++ {
			time.Sleep(5 * time.Millisecond)
		}
		sys = old
	})
	cc.Enabled = true
	cc.MoveMillis = -1
	cc.NoAutoScreenshot = true
	c := config.Config{Computer: cc}
	tool := Tool(Host{Config: func() config.ComputerConfig { cfg := c; return normalized(cfg.Computer) }})
	return fd, tool, ctx
}

// normalized applies config defaults the way config.Store does.
func normalized(cc config.ComputerConfig) config.ComputerConfig {
	if cc.MaxImageSide == 0 {
		cc.MaxImageSide = 1280
	}
	if cc.MaxActions == 0 {
		cc.MaxActions = 200
	}
	if cc.SessionMinutes == 0 {
		cc.SessionMinutes = 30
	}
	if cc.Approval == "" {
		cc.Approval = config.ComputerApproveActions
	}
	return cc
}

func call(t *testing.T, tool *tools.Tool, ctx context.Context, args string) (string, error) {
	t.Helper()
	text, _, err := tool.RunMedia(ctx, json.RawMessage(args))
	return text, err
}

func TestClickMapsToPrimaryMonitor(t *testing.T) {
	fd, tool, ctx := setup(t, config.ComputerConfig{})
	out, err := call(t, tool, ctx, `{"action":"screenshot"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "1280×720 image") {
		t.Errorf("screenshot text: %s", out)
	}
	if _, err := call(t, tool, ctx, `{"action":"left_click","x":"640","y":360}`); err != nil {
		t.Fatal(err)
	}
	if !fd.has("left true @960,540") || !fd.has("left false @960,540") {
		t.Errorf("click went to the wrong place:\n%s", fd)
	}
	if _, err := call(t, tool, ctx, `{"action":"double_click","coordinate":[0,0]}`); err != nil {
		t.Fatal(err)
	}
	if fd.count("left true @0,0") != 2 {
		t.Errorf("double click:\n%s", fd)
	}
	if _, err := call(t, tool, ctx, `{"action":"left_click","x":5000,"y":1}`); err == nil {
		t.Error("a point outside the screenshot was accepted")
	}
}

func TestSecondMonitorAndExclusions(t *testing.T) {
	fd, tool, ctx := setup(t, config.ComputerConfig{Monitor: 1, ExcludedApps: []string{"keepass"}, ExcludedMonitors: []int{0}})
	if _, err := call(t, tool, ctx, `{"action":"screenshot","monitor":0}`); err == nil {
		t.Error("an excluded monitor was captured")
	}
	if _, err := call(t, tool, ctx, `{"action":"screenshot"}`); err != nil {
		t.Fatal(err)
	}
	fd.setWindows([]Window{
		{ID: "k", Title: "Vault", App: "KeePass.exe", Rect: Rect{1920, 0, 500, 500}, Focused: true},
		{ID: "a", Title: "ASG", App: "asg-desktop.exe", Rect: Rect{3000, 0, 800, 800}, Own: true},
	})
	if _, err := call(t, tool, ctx, `{"action":"left_click","x":10,"y":10}`); err == nil || !strings.Contains(err.Error(), "excluded") {
		t.Errorf("click into an excluded app: %v", err)
	}
	if _, err := call(t, tool, ctx, `{"action":"left_click","x":1200,"y":100}`); err == nil || !strings.Contains(err.Error(), "ASG's own window") {
		t.Errorf("click into ASG itself: %v", err)
	}
	if _, err := call(t, tool, ctx, `{"action":"type","text":"hunter2"}`); err == nil {
		t.Error("typed into an excluded app that has focus")
	}
	if fd.count("left") != 0 || fd.count("type") != 0 {
		t.Errorf("input was sent:\n%s", fd)
	}
	if _, err := call(t, tool, ctx, `{"action":"left_click","x":900,"y":600}`); err != nil {
		t.Fatalf("click on the free part of monitor 1: %v", err)
	}
	if !fd.has("left true @3270,900") {
		t.Errorf("monitor 1 click:\n%s", fd)
	}
}

func TestKeysAndTyping(t *testing.T) {
	fd, tool, ctx := setup(t, config.ComputerConfig{})
	if _, err := call(t, tool, ctx, `{"action":"key","keys":"ctrl+shift+t"}`); err != nil {
		t.Fatal(err)
	}
	want := []string{"key ctrl true", "key shift true", "key t true", "key t false", "key shift false", "key ctrl false"}
	if got := fd.entries(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("combo order:\n got %v\nwant %v", got, want)
	}
	fd.clear()
	if _, err := call(t, tool, ctx, `{"action":"type","text":"hello world"}`); err != nil {
		t.Fatal(err)
	}
	if !fd.has("type hello world") {
		t.Errorf("typing:\n%s", fd)
	}
	fd.clear()
	call(t, tool, ctx, `{"action":"key_down","keys":"shift"}`)
	releaseAll()
	if !fd.has("key shift false") {
		t.Errorf("releaseAll did not let go of a held key:\n%s", fd)
	}
}

func TestPauseStopAndLimits(t *testing.T) {
	fd, tool, ctx := setup(t, config.ComputerConfig{MaxActions: 2})
	if _, err := call(t, tool, ctx, `{"action":"screenshot"}`); err != nil {
		t.Fatal(err)
	}
	var states []State
	var mu sync.Mutex
	unsub := Subscribe(func(s State) { mu.Lock(); states = append(states, s); mu.Unlock() })
	defer unsub()

	Pause("")
	if _, err := call(t, tool, ctx, `{"action":"left_click","x":1,"y":1}`); err == nil || !strings.Contains(err.Error(), "paused") {
		t.Errorf("click while paused: %v", err)
	}
	ResumeAfterAnswer()
	if !Current().Active || Current().Paused {
		t.Errorf("state after resume: %+v", Current())
	}
	for i := 0; i < 2; i++ {
		if _, err := call(t, tool, ctx, `{"action":"left_click","x":1,"y":1}`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := call(t, tool, ctx, `{"action":"left_click","x":1,"y":1}`); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("action limit: %v", err)
	}
	if _, err := call(t, tool, ctx, `{"action":"screenshot"}`); err != nil {
		t.Errorf("looking must not count against the action limit: %v", err)
	}

	stopped := make(chan struct{})
	cancelStop := OnEmergencyStop(func() { close(stopped) })
	defer cancelStop()
	Stop("")
	<-stopped
	if _, err := call(t, tool, ctx, `{"action":"screenshot"}`); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Errorf("after emergency stop: %v", err)
	}
	if fd.count("left") != 4 {
		t.Errorf("clicks:\n%s", fd)
	}
	mu.Lock()
	defer mu.Unlock()
	var sawPause, sawStop bool
	for _, s := range states {
		sawPause = sawPause || s.Paused
		sawStop = sawStop || s.Stopped
	}
	if !sawPause || !sawStop {
		t.Errorf("subscribers missed states: %+v", states)
	}
}

func TestTakeoverInterruptsAction(t *testing.T) {
	_, tool, ctx := setup(t, config.ComputerConfig{})
	call(t, tool, ctx, `{"action":"screenshot"}`)
	ctl.takeover.Store(true)
	if err := ctl.interrupted(ctx); err == nil || !Current().Paused {
		t.Errorf("takeover: err=%v state=%+v", err, Current())
	}
	Resume()
}

func TestNewTurnResetsStop(t *testing.T) {
	_, tool, ctx := setup(t, config.ComputerConfig{})
	call(t, tool, ctx, `{"action":"screenshot"}`)
	Stop("")
	ctx2, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := call(t, tool, ctx2, `{"action":"screenshot"}`); err != nil {
		t.Errorf("a new turn should start fresh: %v", err)
	}
}

func TestApprovalLevels(t *testing.T) {
	lvl := config.ComputerApproveActions
	tool := Tool(Host{Config: func() config.ComputerConfig { return config.ComputerConfig{Enabled: true, Approval: lvl} }})
	check := func(args string, want tools.Approval) {
		t.Helper()
		if got := tool.Approval(json.RawMessage(args)); got != want {
			t.Errorf("[%s] %s: approval %v, want %v", lvl, args, got, want)
		}
	}
	check(`{"action":"screenshot"}`, tools.ApprovalNone)
	check(`{"action":"left_click","x":1,"y":1}`, tools.ApprovalAsk)
	check(`{"action":"key","keys":"alt+f4"}`, tools.ApprovalAlways)
	check(`{"action":"left_click","x":1,"y":1,"sensitive":"true"}`, tools.ApprovalAlways)
	lvl = config.ComputerApproveAuto
	check(`{"action":"type","text":"x"}`, tools.ApprovalNone)
	check(`{"action":"key","keys":"ctrl+w"}`, tools.ApprovalAlways)
	lvl = config.ComputerApproveEvery
	check(`{"action":"screenshot"}`, tools.ApprovalAsk)
	if r := tool.Rules(json.RawMessage(`{"action":"double_click"}`)); len(r) != 1 || r[0] != "computer:mouse" {
		t.Errorf("rules = %v", r)
	}
	if !tool.Enabled() {
		t.Error("enabled")
	}
}

func TestDisabledRefuses(t *testing.T) {
	tool := Tool(Host{Config: func() config.ComputerConfig { return config.ComputerConfig{} }})
	if tool.Enabled() {
		t.Error("tool should be hidden while computer use is off")
	}
	if _, _, err := tool.RunMedia(context.Background(), json.RawMessage(`{"action":"screenshot"}`)); err == nil {
		t.Error("ran while off")
	}
}

func init() { headless = true } // no on-screen frame or input hooks in tests
