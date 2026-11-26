//go:build darwin

package computer

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/kxngHADES/asg-harness/internal/proc"
)

// macOS uses screencapture (pixels), cliclick (mouse; brew install
// cliclick) and System Events through osascript (keyboard, windows).
// The terminal or ASG app needs Screen Recording and Accessibility
// permission in System Settings → Privacy & Security.

var unsupported = ""

func init() {
	if !have("cliclick") {
		unsupported = "computer use on macOS needs cliclick for the mouse: brew install cliclick — then allow Screen Recording and Accessibility for your terminal (or ASG) in System Settings → Privacy & Security"
		return
	}
	sys = &macDriver{mods: map[string]bool{}}
}

type macDriver struct {
	mu   sync.Mutex
	mods map[string]bool // modifiers held by key_down
}

func jxa(script string) (string, error) {
	return runCmd("osascript", "-l", "JavaScript", "-e", script)
}

func (*macDriver) Monitors() ([]Monitor, error) {
	out, err := jxa(`ObjC.import("AppKit"); var s = $.NSScreen.screens.js; var mainH = s[0].frame.size.height;
JSON.stringify(s.map(function (x, i) { var f = x.frame; return {name: "display " + (i + 1), x: f.origin.x, y: mainH - f.origin.y - f.size.height, w: f.size.width, h: f.size.height, primary: i == 0}; }))`)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Name       string
		X, Y, W, H float64
		Primary    bool
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("could not read the displays: %w", err)
	}
	var ms []Monitor
	for _, m := range raw {
		ms = append(ms, Monitor{Name: m.Name, Rect: Rect{int(m.X), int(m.Y), int(m.W), int(m.H)}, Primary: m.Primary})
	}
	return ms, nil
}

func (*macDriver) Capture(r Rect) (image.Image, error) {
	path := tempPNG()
	defer os.Remove(path)
	if _, err := runCmd("screencapture", "-x", "-R", fmt.Sprintf("%d,%d,%d,%d", r.X, r.Y, r.W, r.H), path); err != nil {
		return nil, fmt.Errorf("%w (allow Screen Recording for your terminal or ASG in System Settings)", err)
	}
	return decodePNGFile(path)
}

var reInts = regexp.MustCompile(`-?\d+`)

func (*macDriver) Cursor() (int, int, error) {
	out, err := runCmd("cliclick", "p")
	if err != nil {
		return 0, 0, err
	}
	n := reInts.FindAllString(out, -1)
	if len(n) < 2 {
		return 0, 0, fmt.Errorf("could not read the mouse position: %q", out)
	}
	x, _ := strconv.Atoi(n[len(n)-2])
	y, _ := strconv.Atoi(n[len(n)-1])
	return x, y, nil
}

func (*macDriver) Move(x, y int) error {
	noteMove(x, y)
	_, err := runCmd("cliclick", fmt.Sprintf("m:%d,%d", x, y))
	return err
}

func (*macDriver) Button(b Button, down bool) error {
	switch b {
	case Left:
		verb := "du:."
		if down {
			verb = "dd:."
		}
		_, err := runCmd("cliclick", verb)
		return err
	case Right:
		if down { // cliclick has no separate right press / release
			_, err := runCmd("cliclick", "rc:.")
			return err
		}
		return nil
	}
	return errors.New("the middle button is not supported on macOS")
}

func (*macDriver) Scroll(dx, dy int) error {
	// No dependency-free way to post scroll-wheel events: use the keyboard.
	key, n := "", 0
	switch {
	case dy > 0:
		key, n = "125", dy // down arrow
	case dy < 0:
		key, n = "126", -dy
	case dx > 0:
		key, n = "124", dx
	case dx < 0:
		key, n = "123", -dx
	default:
		return nil
	}
	script := fmt.Sprintf(`tell application "System Events" to repeat %d times
key code %s
end repeat`, n*2, key)
	_, err := runCmd("osascript", "-e", script)
	return err
}

// keystroke types text, passing it as an argument (never inside the script).
func keystroke(text string, using string) error {
	script := []string{"-e", "on run argv", "-e", `tell application "System Events" to keystroke (item 1 of argv)` + using, "-e", "end run", "--", text}
	_, err := runCmd("osascript", script...)
	return err
}

func keycode(code int, using string) error {
	_, err := runCmd("osascript", "-e", fmt.Sprintf(`tell application "System Events" to key code %d%s`, code, using))
	return err
}

func (*macDriver) Type(text string) error {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			if err := keystroke(l, ""); err != nil {
				return fmt.Errorf("%w (allow Accessibility for your terminal or ASG in System Settings)", err)
			}
		}
		if i < len(lines)-1 {
			if err := keycode(36, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

var macCodes = map[string]int{
	"enter": 36, "tab": 48, "space": 49, "backspace": 51, "escape": 53, "delete": 117,
	"home": 115, "end": 119, "pageup": 116, "pagedown": 121,
	"left": 123, "right": 124, "down": 125, "up": 126, "capslock": 57,
	"volumeup": 72, "volumedown": 73, "volumemute": 74,
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97, "f7": 98, "f8": 100,
	"f9": 101, "f10": 109, "f11": 103, "f12": 111,
}

var macMods = map[string]string{"ctrl": "control down", "alt": "option down", "shift": "shift down", "super": "command down"}

// Key: modifiers are remembered and applied to the next key press, since
// System Events can only press a key "using" modifiers.
func (d *macDriver) Key(name string, down bool) error {
	d.mu.Lock()
	if _, ok := macMods[name]; ok {
		if down {
			d.mods[name] = true
		} else {
			delete(d.mods, name)
		}
		d.mu.Unlock()
		return nil
	}
	var using []string
	for _, m := range []string{"ctrl", "alt", "shift", "super"} {
		if d.mods[m] {
			using = append(using, macMods[m])
		}
	}
	d.mu.Unlock()
	if !down {
		return nil
	}
	u := ""
	if len(using) > 0 {
		u = " using {" + strings.Join(using, ", ") + "}"
	}
	if c, ok := macCodes[name]; ok {
		return keycode(c, u)
	}
	if len([]rune(name)) == 1 {
		return keystroke(name, u)
	}
	return fmt.Errorf("key %q is not supported on macOS", name)
}

func (*macDriver) Windows() ([]Window, error) {
	out, err := jxa(`var se = Application("System Events"); var out = [];
se.processes.whose({backgroundOnly: false})().forEach(function (p) {
  var front = false; try { front = p.frontmost(); } catch (e) {}
  var ws = []; try { ws = p.windows(); } catch (e) {}
  ws.forEach(function (w, i) { try { var pos = w.position(), sz = w.size();
    out.push({id: p.unixId() + ":" + (i + 1), app: p.name(), pid: p.unixId(), title: w.name() || "", x: pos[0], y: pos[1], w: sz[0], h: sz[1], front: front && i == 0});
  } catch (e) {} });
});
JSON.stringify(out)`)
	if err != nil {
		return nil, fmt.Errorf("%w (allow Accessibility for your terminal or ASG in System Settings)", err)
	}
	var raw []struct {
		ID, App, Title string
		Pid            int
		X, Y, W, H     float64
		Front          bool
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	me := os.Getpid()
	var wins []Window
	for _, w := range raw {
		win := Window{ID: w.ID, Title: w.Title, App: w.App, Rect: Rect{int(w.X), int(w.Y), int(w.W), int(w.H)}, Focused: w.Front, Own: w.Pid == me}
		if w.Front {
			wins = append([]Window{win}, wins...)
		} else {
			wins = append(wins, win)
		}
	}
	return wins, nil
}

func (*macDriver) Focus(id string) error {
	pid, idx, ok := strings.Cut(id, ":")
	if !ok {
		return fmt.Errorf("bad window id %q", id)
	}
	if _, err := strconv.Atoi(pid); err != nil {
		return fmt.Errorf("bad window id %q", id)
	}
	if _, err := strconv.Atoi(idx); err != nil {
		return fmt.Errorf("bad window id %q", id)
	}
	_, err := runCmd("osascript", "-e", fmt.Sprintf(`tell application "System Events"
set p to first process whose unix id is %s
set frontmost of p to true
try
perform action "AXRaise" of window %s of p
end try
end tell`, pid, idx))
	return err
}

func (*macDriver) Open(target string) error {
	args := []string{target}
	if !looksLikePathOrURL(target) {
		args = []string{"-a", target}
	}
	cmd := exec.Command("open", args...)
	proc.Hide(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("could not open %q: %v %s", target, err, strings.TrimSpace(string(out)))
	}
	return nil
}
