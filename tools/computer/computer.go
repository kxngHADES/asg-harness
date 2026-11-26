// Package computer is live computer use: ASG looks at the screen and moves
// the real mouse and keyboard, in a perceive → act → verify loop.
//
// It is one tool, "computer", whose action argument picks what to do
// (screenshot, left_click, type, key, drag, scroll, focus_window, …). The
// model gives coordinates in the last screenshot's pixels and they are
// scaled onto the screen. After every mouse or keyboard action a fresh
// screenshot (with how much of the screen changed) comes back, so the
// model can check that the action worked before the next one.
//
// Safety: the tool is off until the user enables it; every input action
// passes ASG's permission gate (sensitive ones even in YOLO mode); the
// user can pause, resume or stop at any time (Esc on Windows, the desktop
// banner, ctrl+c); grabbing the mouse or keyboard mid-action pauses
// control; excluded apps and monitors are blacked out and never touched;
// and input into ASG's own windows is refused, so the model can't approve
// its own permission gates.
package computer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/atotto/clipboard"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/internal/llm"
	"github.com/kxngHADES/asg-harness/internal/tools"
)

// Host connects the tool to the rest of ASG.
type Host struct {
	// Config returns the current computer-use settings.
	Config func() config.ComputerConfig
	// Perceive, when set and a vision model is configured, describes a
	// screenshot in text for a chat model that only plans actions.
	Perceive func(ctx context.Context, img llm.Image, w, h int, focus string) (string, error)
}

// Name is the tool's name.
const Name = "computer"

var heldMu sync.Mutex

// action kinds, for permission rules and approval levels.
const (
	kindLook      = "look"
	kindMouse     = "mouse"
	kindKeyboard  = "keyboard"
	kindWindows   = "windows"
	kindClipboard = "clipboard"
)

var actionKinds = map[string]string{
	"screenshot": kindLook, "zoom": kindLook, "list_monitors": kindLook, "cursor_position": kindLook,
	"list_windows": kindLook, "wait": kindLook,
	"mouse_move": kindMouse, "left_click": kindMouse, "right_click": kindMouse, "middle_click": kindMouse,
	"double_click": kindMouse, "triple_click": kindMouse, "click": kindMouse, "mouse_down": kindMouse,
	"mouse_up": kindMouse, "drag": kindMouse, "scroll": kindMouse,
	"type": kindKeyboard, "key": kindKeyboard, "key_down": kindKeyboard, "key_up": kindKeyboard,
	"focus_window": kindWindows, "open": kindWindows,
	"clipboard_get": kindClipboard, "clipboard_set": kindClipboard,
}

// Actions lists every action, for docs and the schema.
func Actions() []string {
	out := make([]string, 0, len(actionKinds))
	for a := range actionKinds {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// Args is the tool input. Coordinates are pixels in the last screenshot.
type Args struct {
	Action     string    `json:"action"`
	X          *fnum     `json:"x,omitempty"`
	Y          *fnum     `json:"y,omitempty"`
	ToX        *fnum     `json:"to_x,omitempty"`
	ToY        *fnum     `json:"to_y,omitempty"`
	Coordinate []float64 `json:"coordinate,omitempty"` // [x, y], as some models send it
	Button     string    `json:"button,omitempty"`
	Clicks     *fnum     `json:"clicks,omitempty"`
	Direction  string    `json:"direction,omitempty"`
	Amount     *fnum     `json:"amount,omitempty"`
	Text       string    `json:"text,omitempty"`
	Keys       string    `json:"keys,omitempty"`
	Monitor    *fnum     `json:"monitor,omitempty"`
	Window     string    `json:"window,omitempty"`
	Target     string    `json:"target,omitempty"`
	Seconds    *fnum     `json:"seconds,omitempty"`
	Focus      string    `json:"focus,omitempty"`
	Sensitive  fbool     `json:"sensitive,omitempty"`
}

// fnum accepts 5, 5.0 or "5".
type fnum float64

func (f *fnum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("expected a number, got %s", b)
	}
	*f = fnum(n)
	return nil
}

func (f *fnum) int() int { return int(math.Round(float64(*f))) }

// fbool accepts true, "true" or 1.
type fbool bool

func (f *fbool) UnmarshalJSON(b []byte) error {
	switch strings.ToLower(strings.Trim(string(b), `"`)) {
	case "true", "1", "yes":
		*f = true
	default:
		*f = false
	}
	return nil
}

func parseArgs(raw json.RawMessage) (Args, error) {
	var a Args
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return a, fmt.Errorf("invalid arguments: %w", err)
		}
	}
	a.Action = strings.ToLower(strings.TrimSpace(a.Action))
	if len(a.Coordinate) == 2 && a.X == nil && a.Y == nil {
		x, y := fnum(a.Coordinate[0]), fnum(a.Coordinate[1])
		a.X, a.Y = &x, &y
	}
	if a.Keys == "" && (a.Action == "key" || a.Action == "key_down" || a.Action == "key_up") {
		a.Keys = a.Text
	}
	if _, ok := actionKinds[a.Action]; !ok {
		return a, fmt.Errorf("unknown action %q — one of: %s", a.Action, strings.Join(Actions(), ", "))
	}
	return a, nil
}

// sensitive reports calls that must always be approved by a person.
func (a Args) sensitive() bool {
	if a.Sensitive {
		return true
	}
	switch a.Action {
	case "key", "key_down":
		return sensitiveKeys(a.Keys)
	}
	return false
}

// Tool returns the computer tool.
func Tool(h Host) *tools.Tool {
	cfg := func() config.ComputerConfig {
		if h.Config == nil {
			return config.ComputerConfig{}
		}
		return h.Config()
	}
	return &tools.Tool{
		Spec: llm.ToolSpec{
			Name: Name,
			Description: "Use the user's computer live: see the screen and move the real mouse and keyboard. Work in a loop: screenshot → " +
				"decide one action → do it → look at the screenshot that comes back and check it worked → next action. " +
				"x/y are pixels in the LAST screenshot (0,0 = top-left). Prefer keyboard shortcuts when they are reliable. " +
				"Set sensitive=true for anything that sends, submits, buys, deletes, closes unsaved work or can't be undone — the user must approve it. " +
				"Never type passwords or other credentials unless the user gave them to you for that purpose. " +
				"Actions: screenshot, zoom (x,y,to_x,to_y region to read small text), list_monitors, list_windows, cursor_position, wait (seconds), " +
				"mouse_move, left_click, right_click, middle_click, double_click, triple_click (x,y), mouse_down/mouse_up (button, optional x,y), " +
				"drag (x,y → to_x,to_y), scroll (x,y, direction up|down|left|right, amount), type (text), key (keys like \"ctrl+c\", \"alt+tab\", " +
				"\"enter\"; several chords separated by spaces), key_down/key_up (hold and release), focus_window (window: title words or id), " +
				"open (target: app name, file, folder or URL), clipboard_get, clipboard_set (text).",
			Parameters: map[string]any{"type": "object", "required": []string{"action"}, "properties": map[string]any{
				"action":    map[string]any{"type": "string", "enum": Actions(), "description": "What to do"},
				"x":         map[string]any{"type": "integer", "description": "X in the last screenshot's pixels"},
				"y":         map[string]any{"type": "integer", "description": "Y in the last screenshot's pixels"},
				"to_x":      map[string]any{"type": "integer", "description": "drag / zoom: end X"},
				"to_y":      map[string]any{"type": "integer", "description": "drag / zoom: end Y"},
				"button":    map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "description": "mouse_down / mouse_up / click button (default left)"},
				"clicks":    map[string]any{"type": "integer", "description": "click: how many clicks (1–3)"},
				"direction": map[string]any{"type": "string", "enum": []string{"up", "down", "left", "right"}, "description": "scroll direction"},
				"amount":    map[string]any{"type": "integer", "description": "scroll: wheel clicks (default 3)"},
				"text":      map[string]any{"type": "string", "description": "type / clipboard_set: the text"},
				"keys":      map[string]any{"type": "string", "description": `key / key_down / key_up: e.g. "ctrl+s", "alt+tab", "enter", "ctrl+a ctrl+c"`},
				"monitor":   map[string]any{"type": "integer", "description": "screenshot: monitor number from list_monitors (0 = primary, -1 = every monitor)"},
				"window":    map[string]any{"type": "string", "description": "focus_window: window id or words from its title / app name"},
				"target":    map[string]any{"type": "string", "description": "open: app name, file, folder or URL"},
				"seconds":   map[string]any{"type": "number", "description": "wait: seconds (default 1, max 30)"},
				"focus":     map[string]any{"type": "string", "description": "screenshot: what you are looking for (helps a separate vision model)"},
				"sensitive": map[string]any{"type": "boolean", "description": "true for irreversible actions (send, submit, buy, delete, close) — always asks the user"},
			}},
		},
		NeedsApproval: true,
		Source:        "bundled",
		Enabled:       func() bool { return cfg().Enabled },
		Approval: func(raw json.RawMessage) tools.Approval {
			a, err := parseArgs(raw)
			if err != nil {
				return tools.ApprovalNone // it will fail without doing anything
			}
			if a.sensitive() {
				return tools.ApprovalAlways
			}
			switch cfg().Approval {
			case config.ComputerApproveEvery:
				return tools.ApprovalAsk
			case config.ComputerApproveAuto:
				return tools.ApprovalNone
			}
			if actionKinds[a.Action] == kindLook {
				return tools.ApprovalNone
			}
			return tools.ApprovalAsk
		},
		Rules: func(raw json.RawMessage) []string {
			a, _ := parseArgs(raw)
			return []string{Name + ":" + actionKinds[a.Action]}
		},
		Describe: func(raw json.RawMessage) string { a, _ := parseArgs(raw); return describe(a) },
		Run: func(ctx context.Context, raw json.RawMessage) (string, error) {
			text, _, err := (&runner{h: h, cfg: cfg()}).do(ctx, raw)
			return text, err
		},
		RunMedia: func(ctx context.Context, raw json.RawMessage) (string, []llm.Image, error) {
			return (&runner{h: h, cfg: cfg()}).do(ctx, raw)
		},
	}
}

func describe(a Args) string {
	at := ""
	if a.X != nil && a.Y != nil {
		at = fmt.Sprintf(" (%d, %d)", a.X.int(), a.Y.int())
	}
	switch a.Action {
	case "type", "clipboard_set":
		t := a.Text
		if utf8.RuneCountInString(t) > 60 {
			t = string([]rune(t)[:60]) + "…"
		}
		return fmt.Sprintf("%s %q", a.Action, t)
	case "key", "key_down", "key_up":
		return a.Action + " " + a.Keys
	case "drag", "zoom":
		if a.ToX != nil && a.ToY != nil {
			return fmt.Sprintf("%s%s → (%d, %d)", a.Action, at, a.ToX.int(), a.ToY.int())
		}
	case "scroll":
		return fmt.Sprintf("scroll %s%s", a.Direction, at)
	case "focus_window":
		return "focus " + a.Window
	case "open":
		return "open " + a.Target
	}
	return a.Action + at
}

// runner carries one call.
type runner struct {
	h   Host
	cfg config.ComputerConfig
	r   *run
}

func (x *runner) do(ctx context.Context, raw json.RawMessage) (string, []llm.Image, error) {
	a, err := parseArgs(raw)
	if err != nil {
		return "", nil, err
	}
	if !x.cfg.Enabled {
		return "", nil, errors.New("computer use is turned off — the user can turn it on (TUI: /computer on, desktop: Customise → Computer use)")
	}
	if sys == nil {
		return "", nil, errors.New(unsupported)
	}
	x.r = ctl.begin(ctx)
	kind := actionKinds[a.Action]
	input := kind == kindMouse || kind == kindKeyboard || a.Action == "focus_window" || a.Action == "open"
	if err := ctl.check(x.r, x.cfg.MaxActions, time.Duration(x.cfg.SessionMinutes)*time.Minute, input); err != nil {
		return "", nil, err
	}
	if input {
		if err := x.settle(ctx); err != nil {
			return "", nil, err
		}
		ctl.acting.Store(true)
		ctl.takeover.Store(false)
	}
	msg, err := x.act(ctx, a)
	ctl.acting.Store(false)
	ctl.note(x.r, describe(a), input)
	if err != nil {
		return "", nil, err
	}
	if !input || x.cfg.NoAutoScreenshot || a.Action == "key_down" || a.Action == "mouse_down" {
		if a.Action == "screenshot" || a.Action == "zoom" || a.Action == "wait" {
			return msg.text, msg.images, nil
		}
		return msg.text, nil, nil
	}
	// Let the UI react, then look again so the model can verify.
	pause := 400 * time.Millisecond
	if a.Action == "open" || a.Action == "focus_window" {
		pause = 1200 * time.Millisecond
	}
	if err := sleep(ctx, pause); err != nil {
		return msg.text, nil, nil
	}
	v, verr := x.view(nil)
	if verr != nil {
		return msg.text, nil, nil
	}
	shot, err := x.shoot(ctx, v, "")
	if err != nil {
		return msg.text + "\n(could not take the follow-up screenshot: " + err.Error() + ")", nil, nil
	}
	return msg.text + "\n\nAfter the action — " + shot.text, shot.images, nil
}

type result struct {
	text   string
	images []llm.Image
}

func (x *runner) act(ctx context.Context, a Args) (result, error) {
	switch a.Action {
	case "screenshot":
		v, err := x.view(a.Monitor)
		if err != nil {
			return result{}, err
		}
		return x.shoot(ctx, v, a.Focus)
	case "zoom":
		return x.zoom(a)
	case "list_monitors":
		return x.listMonitors()
	case "list_windows":
		return x.listWindows()
	case "cursor_position":
		cx, cy, err := sys.Cursor()
		if err != nil {
			return result{}, err
		}
		v, err := x.view(nil)
		if err != nil {
			return result{}, err
		}
		if ix, iy, ok := v.toImage(cx, cy); ok {
			return result{text: fmt.Sprintf("The mouse is at (%d, %d) in the screenshot.", ix, iy)}, nil
		}
		return result{text: "The mouse is outside the current monitor."}, nil
	case "wait":
		secs := 1.0
		if a.Seconds != nil {
			secs = math.Min(math.Max(float64(*a.Seconds), 0.1), 30)
		}
		if err := sleep(ctx, time.Duration(secs*float64(time.Second))); err != nil {
			return result{}, err
		}
		v, err := x.view(nil)
		if err != nil {
			return result{}, err
		}
		shot, err := x.shoot(ctx, v, "")
		shot.text = fmt.Sprintf("Waited %.1fs. ", secs) + shot.text
		return shot, err
	case "mouse_move":
		sx, sy, err := x.point(a)
		if err != nil {
			return result{}, err
		}
		if err := x.moveTo(ctx, sx, sy); err != nil {
			return result{}, err
		}
		return result{text: "Moved the mouse to " + at(a) + "."}, nil
	case "left_click", "right_click", "middle_click", "double_click", "triple_click", "click":
		return x.click(ctx, a)
	case "mouse_down", "mouse_up":
		return x.mouseButton(ctx, a)
	case "drag":
		return x.drag(ctx, a)
	case "scroll":
		return x.scroll(ctx, a)
	case "type":
		return x.typeText(ctx, a.Text)
	case "key":
		return x.keys(ctx, a.Keys)
	case "key_down", "key_up":
		return x.holdKeys(a)
	case "focus_window":
		return x.focus(a.Window)
	case "open":
		t := strings.TrimSpace(a.Target)
		if t == "" {
			return result{}, errors.New("open needs a target: an app name, file, folder or URL")
		}
		if err := sys.Open(t); err != nil {
			return result{}, err
		}
		return result{text: "Opened " + t + "."}, nil
	case "clipboard_get":
		s, err := clipboard.ReadAll()
		if err != nil {
			return result{}, fmt.Errorf("read the clipboard: %w", err)
		}
		if len(s) > 20000 {
			s = s[:20000] + "\n…[truncated]"
		}
		return result{text: "Clipboard:\n" + s}, nil
	case "clipboard_set":
		if err := clipboard.WriteAll(a.Text); err != nil {
			return result{}, fmt.Errorf("write the clipboard: %w", err)
		}
		return result{text: fmt.Sprintf("Put %d characters on the clipboard.", utf8.RuneCountInString(a.Text))}, nil
	}
	return result{}, fmt.Errorf("unknown action %q", a.Action)
}

func at(a Args) string {
	if a.X == nil || a.Y == nil {
		return "the current position"
	}
	return fmt.Sprintf("(%d, %d)", a.X.int(), a.Y.int())
}

// --- monitors and views -----------------------------------------------------

// monitors returns the displays, primary first.
func monitors() ([]Monitor, error) {
	ms, err := sys.Monitors()
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, errors.New("no monitors found")
	}
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].Primary != ms[j].Primary {
			return ms[i].Primary
		}
		if ms[i].Rect.X != ms[j].Rect.X {
			return ms[i].Rect.X < ms[j].Rect.X
		}
		return ms[i].Rect.Y < ms[j].Rect.Y
	})
	return ms, nil
}

func excludedMonitor(cfg config.ComputerConfig, i int) bool {
	for _, e := range cfg.ExcludedMonitors {
		if e == i {
			return true
		}
	}
	return false
}

// view picks the coordinate space: the given monitor, else the last
// screenshot's, else the configured one.
func (x *runner) view(mon *fnum) (view, error) {
	if mon == nil {
		ctl.mu.Lock()
		last := x.r.view
		ctl.mu.Unlock()
		if last != nil {
			return *last, nil
		}
	}
	idx := x.cfg.Monitor
	if mon != nil {
		idx = mon.int()
	}
	ms, err := monitors()
	if err != nil {
		return view{}, err
	}
	if idx == -1 {
		var rs []Rect
		for _, m := range ms {
			rs = append(rs, m.Rect)
		}
		return newView(-1, union(rs), x.cfg.MaxImageSide), nil
	}
	if idx < 0 || idx >= len(ms) {
		return view{}, fmt.Errorf("there is no monitor %d — list_monitors shows %d (0–%d, or -1 for all)", idx, len(ms), len(ms)-1)
	}
	if excludedMonitor(x.cfg, idx) {
		return view{}, fmt.Errorf("monitor %d is excluded from computer use by the user", idx)
	}
	return newView(idx, ms[idx].Rect, x.cfg.MaxImageSide), nil
}

// point maps the call's x/y to the screen and checks ASG may touch it.
func (x *runner) point(a Args) (int, int, error) {
	if a.X == nil || a.Y == nil {
		return 0, 0, fmt.Errorf("%s needs x and y (pixels in the last screenshot)", a.Action)
	}
	return x.mapPoint(a.X.int(), a.Y.int())
}

func (x *runner) mapPoint(ix, iy int) (int, int, error) {
	v, err := x.view(nil)
	if err != nil {
		return 0, 0, err
	}
	if ix < 0 || iy < 0 || ix >= v.ImgW || iy >= v.ImgH {
		return 0, 0, fmt.Errorf("(%d, %d) is outside the screenshot, which is %d×%d", ix, iy, v.ImgW, v.ImgH)
	}
	sx, sy := v.toScreen(ix, iy)
	if err := x.allowedAt(sx, sy); err != nil {
		return 0, 0, err
	}
	return sx, sy, nil
}

// allowedAt refuses points on excluded monitors, excluded apps, or ASG itself.
func (x *runner) allowedAt(sx, sy int) error {
	if ms, err := monitors(); err == nil {
		for i, m := range ms {
			if m.Rect.contains(sx, sy) && excludedMonitor(x.cfg, i) {
				return fmt.Errorf("that point is on monitor %d, which the user excluded from computer use", i)
			}
		}
	}
	wins, err := sys.Windows()
	if err != nil {
		return nil // can't tell; the gate already asked
	}
	for _, w := range wins { // front-most first: the first hit is what gets the click
		if w.Minimized || !w.Rect.contains(sx, sy) {
			continue
		}
		return windowAllowed(w, x.cfg)
	}
	return nil
}

// asgApps are ASG's own programs: another ASG (the desktop app while the
// terminal works, or the reverse) is protected like this process.
var asgApps = map[string]bool{"asg": true, "asg-desktop": true}

func isASG(w Window) bool {
	return w.Own || asgApps[strings.TrimSuffix(strings.ToLower(w.App), ".exe")]
}

func windowAllowed(w Window, cfg config.ComputerConfig) error {
	if isASG(w) {
		return errors.New("that is ASG's own window — computer use can't click or type into ASG itself (it could approve its own actions). Ask the user to do it")
	}
	if matchesApp(w, cfg.ExcludedApps) {
		return fmt.Errorf("that window (%s) belongs to an app the user excluded from computer use", w.App)
	}
	return nil
}

// foregroundAllowed checks the window that will receive keystrokes.
func (x *runner) foregroundAllowed() error {
	wins, err := sys.Windows()
	if err != nil {
		return nil
	}
	for _, w := range wins {
		if w.Focused {
			if err := windowAllowed(w, x.cfg); err != nil {
				return fmt.Errorf("the focused window can't receive input: %w", err)
			}
			return nil
		}
	}
	return nil
}

// --- looking ----------------------------------------------------------------

func (x *runner) shoot(ctx context.Context, v view, focus string) (result, error) {
	raw, err := sys.Capture(v.Rect)
	if err != nil {
		return result{}, fmt.Errorf("screen capture failed: %w", err)
	}
	img := fitImage(raw, v)

	var hide []imageRect
	if ms, err := monitors(); err == nil && v.Monitor == -1 {
		for i, m := range ms {
			if excludedMonitor(x.cfg, i) {
				hide = append(hide, v.rectToImage(m.Rect))
			}
		}
	}
	wins, _ := sys.Windows()
	active := ""
	for _, w := range wins {
		if w.Focused {
			active = fmt.Sprintf("%q (%s)", w.Title, w.App)
		}
		if !w.Minimized && matchesApp(w, x.cfg.ExcludedApps) {
			hide = append(hide, v.rectToImage(w.Rect))
			if w.Focused {
				active = "an excluded app (hidden)"
			}
		}
	}
	blackout(img, hide)
	cursor := ""
	if cx, cy, err := sys.Cursor(); err == nil {
		if ix, iy, ok := v.toImage(cx, cy); ok {
			drawCursor(img, ix, iy)
			cursor = fmt.Sprintf(" The mouse is at (%d, %d), marked with a pink ring.", ix, iy)
		}
	}
	th := thumb(img)
	ctl.mu.Lock()
	prev := x.r.thumbs[v.Monitor]
	x.r.thumbs[v.Monitor] = th
	vv := v
	x.r.view = &vv
	ctl.mu.Unlock()

	where := fmt.Sprintf("monitor %d", v.Monitor)
	if v.Monitor == -1 {
		where = "all monitors"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Screenshot of %s (%d×%d on screen) as a %d×%d image. Give x/y in this image's pixels.%s", where, v.Rect.W, v.Rect.H, v.ImgW, v.ImgH, cursor)
	if active != "" {
		fmt.Fprintf(&b, " Active window: %s.", active)
	}
	if d := changed(prev, th); d >= 0 {
		switch {
		case d < 0.002:
			b.WriteString(" The screen did not visibly change since the last screenshot — if you expected a change, the action may have missed.")
		default:
			fmt.Fprintf(&b, " About %.0f%% of the screen changed since the last screenshot.", math.Max(d*100, 1))
		}
	}
	if len(hide) > 0 {
		b.WriteString(" Black areas are excluded by the user; don't try to use them.")
	}
	enc, err := encode(img)
	if err != nil {
		return result{}, err
	}
	if x.cfg.VisionModel != "" && x.h.Perceive != nil {
		desc, perr := x.h.Perceive(ctx, enc, v.ImgW, v.ImgH, focus)
		if perr == nil && strings.TrimSpace(desc) != "" {
			b.WriteString("\n\nWhat the vision model sees (coordinates in the same image):\n" + strings.TrimSpace(desc))
			return result{text: b.String()}, nil
		}
		if perr != nil {
			fmt.Fprintf(&b, "\n(The vision model failed: %v — the image is attached instead.)", perr)
		}
	}
	b.WriteString(" The image is attached.")
	return result{text: b.String(), images: []llm.Image{enc}}, nil
}

func (x *runner) zoom(a Args) (result, error) {
	if a.X == nil || a.Y == nil || a.ToX == nil || a.ToY == nil {
		return result{}, errors.New("zoom needs x, y, to_x and to_y: the region of the last screenshot to look at closely")
	}
	v, err := x.view(nil)
	if err != nil {
		return result{}, err
	}
	x0, y0 := v.toScreen(min(a.X.int(), a.ToX.int()), min(a.Y.int(), a.ToY.int()))
	x1, y1 := v.toScreen(max(a.X.int(), a.ToX.int()), max(a.Y.int(), a.ToY.int()))
	r := Rect{x0, y0, max(x1-x0, 8), max(y1-y0, 8)}.intersect(v.Rect)
	if r.empty() {
		return result{}, errors.New("that region is empty or outside the screenshot")
	}
	raw, err := sys.Capture(r)
	if err != nil {
		return result{}, err
	}
	zv := newView(v.Monitor, r, x.cfg.MaxImageSide)
	b := raw.Bounds()
	if b.Dx() > zv.ImgW { // HiDPI: keep the extra detail
		zv.ImgW, zv.ImgH = min(b.Dx(), x.cfg.MaxImageSide), min(b.Dy(), x.cfg.MaxImageSide)
	}
	img := fitImage(raw, zv)
	var hide []imageRect
	if wins, err := sys.Windows(); err == nil {
		for _, w := range wins {
			if !w.Minimized && matchesApp(w, x.cfg.ExcludedApps) {
				hide = append(hide, zv.rectToImage(w.Rect))
			}
		}
	}
	blackout(img, hide)
	enc, err := encode(img)
	if err != nil {
		return result{}, err
	}
	return result{text: fmt.Sprintf("Zoomed view of (%d, %d)–(%d, %d), %d×%d. Only for reading: x/y you give still refer to the full screenshot.",
		a.X.int(), a.Y.int(), a.ToX.int(), a.ToY.int(), img.Bounds().Dx(), img.Bounds().Dy()), images: []llm.Image{enc}}, nil
}

func (x *runner) listMonitors() (result, error) {
	ms, err := monitors()
	if err != nil {
		return result{}, err
	}
	var b strings.Builder
	b.WriteString("Monitors (use the number with screenshot's monitor; -1 is all of them):\n")
	for i, m := range ms {
		flags := ""
		if m.Primary {
			flags += " primary"
		}
		if excludedMonitor(x.cfg, i) {
			flags += " EXCLUDED"
		}
		fmt.Fprintf(&b, "%d: %s %d×%d at (%d, %d)%s\n", i, m.Name, m.Rect.W, m.Rect.H, m.Rect.X, m.Rect.Y, flags)
	}
	return result{text: b.String()}, nil
}

func (x *runner) listWindows() (result, error) {
	wins, err := sys.Windows()
	if err != nil {
		return result{}, err
	}
	v, _ := x.view(nil)
	var b strings.Builder
	b.WriteString("Open windows, front-most first (id · app · title · position in the screenshot):\n")
	for i, w := range wins {
		if i >= 60 {
			fmt.Fprintf(&b, "… and %d more\n", len(wins)-i)
			break
		}
		title := w.Title
		if matchesApp(w, x.cfg.ExcludedApps) {
			title = "(excluded by the user)"
		}
		var flags []string
		if w.Focused {
			flags = append(flags, "focused")
		}
		if w.Minimized {
			flags = append(flags, "minimized")
		}
		if isASG(w) {
			flags = append(flags, "ASG — no input")
		}
		pos := "off this monitor"
		if r := v.rectToImage(w.Rect); !r.Empty() {
			pos = fmt.Sprintf("(%d, %d)–(%d, %d)", r.Min.X, r.Min.Y, r.Max.X, r.Max.Y)
		}
		fl := ""
		if len(flags) > 0 {
			fl = " [" + strings.Join(flags, ", ") + "]"
		}
		fmt.Fprintf(&b, "%s · %s · %q · %s%s\n", w.ID, w.App, title, pos, fl)
	}
	return result{text: b.String()}, nil
}

// --- acting -----------------------------------------------------------------

// settle waits for the user to stop using the mouse and keyboard before ASG
// takes over again (e.g. after clicking Allow on a permission gate). If they
// keep going, they have taken control: pause.
func (x *runner) settle(ctx context.Context) error {
	const quiet, patience = 700 * time.Millisecond, 5 * time.Second
	start := time.Now()
	for {
		last := time.Unix(0, lastPhysical.Load())
		if time.Since(last) >= quiet {
			return nil
		}
		if time.Since(start) > patience {
			Pause("you are using the mouse or keyboard")
			return fmt.Errorf(errPaused, "the user is using the mouse or keyboard")
		}
		if err := sleep(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
}

// moveTo moves the pointer visibly, easing in and out.
func (x *runner) moveTo(ctx context.Context, sx, sy int) error {
	cx, cy, err := sys.Cursor()
	ms := x.cfg.MoveMillis
	if err != nil || ms < 0 {
		return sys.Move(sx, sy)
	}
	dist := math.Hypot(float64(sx-cx), float64(sy-cy))
	if dist < 2 {
		return sys.Move(sx, sy)
	}
	dur := time.Duration(math.Min(float64(ms), 120+dist/4)) * time.Millisecond
	steps := max(int(dur/(12*time.Millisecond)), 2)
	for i := 1; i <= steps; i++ {
		if err := ctl.interrupted(ctx); err != nil {
			return err
		}
		t := float64(i) / float64(steps)
		e := (1 - math.Cos(math.Pi*t)) / 2
		if err := sys.Move(cx+int(math.Round(float64(sx-cx)*e)), cy+int(math.Round(float64(sy-cy)*e))); err != nil {
			return err
		}
		time.Sleep(dur / time.Duration(steps))
	}
	return sys.Move(sx, sy)
}

func parseButton(s string) (Button, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "left", "l", "primary":
		return Left, nil
	case "right", "r", "secondary":
		return Right, nil
	case "middle", "m", "wheel":
		return Middle, nil
	}
	return Left, fmt.Errorf("unknown button %q — left, right or middle", s)
}

func (x *runner) click(ctx context.Context, a Args) (result, error) {
	btn, n := Left, 1
	switch a.Action {
	case "right_click":
		btn = Right
	case "middle_click":
		btn = Middle
	case "double_click":
		n = 2
	case "triple_click":
		n = 3
	case "click":
		b, err := parseButton(a.Button)
		if err != nil {
			return result{}, err
		}
		btn = b
		if a.Clicks != nil {
			n = min(max(a.Clicks.int(), 1), 3)
		}
	}
	if a.X != nil && a.Y != nil {
		sx, sy, err := x.point(a)
		if err != nil {
			return result{}, err
		}
		if err := x.moveTo(ctx, sx, sy); err != nil {
			return result{}, err
		}
	} else {
		cx, cy, err := sys.Cursor()
		if err == nil {
			if err := x.allowedAt(cx, cy); err != nil {
				return result{}, err
			}
		}
	}
	for i := 0; i < n; i++ {
		if err := ctl.interrupted(ctx); err != nil {
			return result{}, err
		}
		if err := sys.Button(btn, true); err != nil {
			return result{}, err
		}
		time.Sleep(25 * time.Millisecond)
		if err := sys.Button(btn, false); err != nil {
			return result{}, err
		}
		if i < n-1 {
			time.Sleep(60 * time.Millisecond)
		}
	}
	what := map[int]string{1: "Clicked", 2: "Double-clicked", 3: "Triple-clicked"}[n]
	if btn != Left {
		what = fmt.Sprintf("%s (%s button)", what, btn)
	}
	return result{text: fmt.Sprintf("%s at %s.", what, at(a))}, nil
}

func (x *runner) mouseButton(ctx context.Context, a Args) (result, error) {
	btn, err := parseButton(a.Button)
	if err != nil {
		return result{}, err
	}
	if a.X != nil && a.Y != nil {
		sx, sy, err := x.point(a)
		if err != nil {
			return result{}, err
		}
		if err := x.moveTo(ctx, sx, sy); err != nil {
			return result{}, err
		}
	}
	down := a.Action == "mouse_down"
	if err := sys.Button(btn, down); err != nil {
		return result{}, err
	}
	heldMu.Lock()
	if down {
		held.buttons[btn] = true
	} else {
		delete(held.buttons, btn)
	}
	heldMu.Unlock()
	if down {
		return result{text: fmt.Sprintf("Holding the %s button down at %s. Release it with mouse_up.", btn, at(a))}, nil
	}
	return result{text: fmt.Sprintf("Released the %s button at %s.", btn, at(a))}, nil
}

func (x *runner) drag(ctx context.Context, a Args) (result, error) {
	if a.X == nil || a.Y == nil || a.ToX == nil || a.ToY == nil {
		return result{}, errors.New("drag needs x, y (start) and to_x, to_y (end)")
	}
	sx, sy, err := x.point(a)
	if err != nil {
		return result{}, err
	}
	ex, ey, err := x.mapPoint(a.ToX.int(), a.ToY.int())
	if err != nil {
		return result{}, err
	}
	if err := x.moveTo(ctx, sx, sy); err != nil {
		return result{}, err
	}
	if err := sys.Button(Left, true); err != nil {
		return result{}, err
	}
	defer sys.Button(Left, false) //nolint:errcheck // always let go
	time.Sleep(80 * time.Millisecond)
	// Drags need a slower move with many steps or apps miss them.
	steps := 30
	for i := 1; i <= steps; i++ {
		if err := ctl.interrupted(ctx); err != nil {
			return result{}, err
		}
		t := float64(i) / float64(steps)
		if err := sys.Move(sx+int(math.Round(float64(ex-sx)*t)), sy+int(math.Round(float64(ey-sy)*t))); err != nil {
			return result{}, err
		}
		time.Sleep(15 * time.Millisecond)
	}
	time.Sleep(80 * time.Millisecond)
	if err := sys.Button(Left, false); err != nil {
		return result{}, err
	}
	return result{text: fmt.Sprintf("Dragged from %s to (%d, %d).", at(a), a.ToX.int(), a.ToY.int())}, nil
}

func (x *runner) scroll(ctx context.Context, a Args) (result, error) {
	n := 3
	if a.Amount != nil {
		n = min(max(a.Amount.int(), 1), 50)
	}
	dx, dy := 0, 0
	switch strings.ToLower(a.Direction) {
	case "", "down":
		dy = n
	case "up":
		dy = -n
	case "right":
		dx = n
	case "left":
		dx = -n
	default:
		return result{}, fmt.Errorf("unknown direction %q — up, down, left or right", a.Direction)
	}
	if a.X != nil && a.Y != nil {
		sx, sy, err := x.point(a)
		if err != nil {
			return result{}, err
		}
		if err := x.moveTo(ctx, sx, sy); err != nil {
			return result{}, err
		}
	} else if cx, cy, err := sys.Cursor(); err == nil {
		if err := x.allowedAt(cx, cy); err != nil {
			return result{}, err
		}
	}
	if err := sys.Scroll(dx, dy); err != nil {
		return result{}, err
	}
	dir := a.Direction
	if dir == "" {
		dir = "down"
	}
	return result{text: fmt.Sprintf("Scrolled %s %d click(s) at %s.", dir, n, at(a))}, nil
}

func (x *runner) typeText(ctx context.Context, text string) (result, error) {
	if text == "" {
		return result{}, errors.New("type needs text")
	}
	if utf8.RuneCountInString(text) > 5000 {
		return result{}, errors.New("text is too long to type (over 5000 characters) — use clipboard_set and ctrl+v instead")
	}
	if err := x.foregroundAllowed(); err != nil {
		return result{}, err
	}
	runes := []rune(text)
	for i := 0; i < len(runes); i += 16 {
		if err := ctl.interrupted(ctx); err != nil {
			return result{}, fmt.Errorf("typed %d of %d characters, then: %w", i, len(runes), err)
		}
		if err := sys.Type(string(runes[i:min(i+16, len(runes))])); err != nil {
			return result{}, err
		}
	}
	return result{text: fmt.Sprintf("Typed %d characters.", len(runes))}, nil
}

func (x *runner) keys(ctx context.Context, s string) (result, error) {
	combos, err := ParseKeys(s)
	if err != nil {
		return result{}, err
	}
	if err := x.foregroundAllowed(); err != nil {
		return result{}, err
	}
	for _, c := range combos {
		if err := ctl.interrupted(ctx); err != nil {
			return result{}, err
		}
		if err := pressCombo(c); err != nil {
			return result{}, err
		}
		time.Sleep(40 * time.Millisecond)
	}
	return result{text: "Pressed " + s + "."}, nil
}

// pressCombo holds the modifiers, taps the key, and releases in reverse.
func pressCombo(c Combo) error {
	var down []string
	defer func() {
		for i := len(down) - 1; i >= 0; i-- {
			_ = sys.Key(down[i], false)
		}
	}()
	for _, k := range c {
		if err := sys.Key(k, true); err != nil {
			return err
		}
		down = append(down, k)
		time.Sleep(10 * time.Millisecond)
	}
	return nil
}

func (x *runner) holdKeys(a Args) (result, error) {
	combos, err := ParseKeys(a.Keys)
	if err != nil {
		return result{}, err
	}
	if len(combos) != 1 {
		return result{}, errors.New("key_down / key_up take one chord, e.g. \"shift\" or \"ctrl+shift\"")
	}
	down := a.Action == "key_down"
	if down {
		if err := x.foregroundAllowed(); err != nil {
			return result{}, err
		}
	}
	keys := combos[0]
	if !down {
		keys = append(Combo(nil), keys...)
		for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
			keys[i], keys[j] = keys[j], keys[i]
		}
	}
	for _, k := range keys {
		if err := sys.Key(k, down); err != nil {
			return result{}, err
		}
		heldMu.Lock()
		if down {
			held.keys[k] = true
		} else {
			delete(held.keys, k)
		}
		heldMu.Unlock()
	}
	if down {
		return result{text: "Holding " + a.Keys + ". Release with key_up."}, nil
	}
	return result{text: "Released " + a.Keys + "."}, nil
}

func (x *runner) focus(q string) (result, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return result{}, errors.New("focus_window needs window: an id from list_windows or words from the title")
	}
	wins, err := sys.Windows()
	if err != nil {
		return result{}, err
	}
	var pick *Window
	lq := strings.ToLower(q)
	for i := range wins {
		w := &wins[i]
		if strings.EqualFold(w.ID, q) {
			pick = w
			break
		}
		if pick == nil && (strings.Contains(strings.ToLower(w.Title), lq) || strings.EqualFold(strings.TrimSuffix(strings.ToLower(w.App), ".exe"), lq)) {
			pick = w
		}
	}
	if pick == nil {
		return result{}, fmt.Errorf("no window matches %q — call list_windows", q)
	}
	if matchesApp(*pick, x.cfg.ExcludedApps) {
		return result{}, errors.New("that window belongs to an app the user excluded from computer use")
	}
	if err := sys.Focus(pick.ID); err != nil {
		return result{}, err
	}
	return result{text: fmt.Sprintf("Focused %q (%s).", pick.Title, pick.App)}, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Supported reports whether computer use works on this OS, and why not.
func Supported() (bool, string) {
	if sys == nil {
		return false, unsupported
	}
	return true, runtime.GOOS
}

// ListMonitors returns the displays in the order monitor numbers use
// (0 = primary).
func ListMonitors() ([]Monitor, error) {
	if sys == nil {
		return nil, errors.New(unsupported)
	}
	return monitors()
}
