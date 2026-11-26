//go:build linux

package computer

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kxngHADES/asg-harness/internal/proc"
	"github.com/kxngHADES/asg-harness/tools/screenshot"
)

// Linux drives X11 with xdotool (input), xrandr (monitors), wmctrl
// (windows) and the usual screenshot programs. Wayland compositors don't
// allow synthetic input from ordinary programs, so a Wayland session needs
// XWayland apps or an X11 session.

var unsupported = ""

func init() {
	switch {
	case os.Getenv("DISPLAY") == "":
		unsupported = "computer use on Linux needs an X11 display (DISPLAY is not set; Wayland-only sessions can't receive synthetic input) — log in to an X11 session"
	case !have("xdotool"):
		unsupported = "computer use on Linux needs xdotool (and wmctrl for windows): e.g. sudo apt install xdotool wmctrl"
	default:
		sys = &linuxDriver{}
	}
}

type linuxDriver struct{}

var reXrandr = regexp.MustCompile(`^\s*\d+:\s+\+?(\*?)(\S+)\s+(\d+)/\d+x(\d+)/\d+\+(-?\d+)\+(-?\d+)`)

func (linuxDriver) Monitors() ([]Monitor, error) {
	if have("xrandr") {
		if out, err := runCmd("xrandr", "--listmonitors"); err == nil {
			var ms []Monitor
			for _, line := range strings.Split(out, "\n") {
				m := reXrandr.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				w, _ := strconv.Atoi(m[3])
				h, _ := strconv.Atoi(m[4])
				x, _ := strconv.Atoi(m[5])
				y, _ := strconv.Atoi(m[6])
				ms = append(ms, Monitor{Name: m[2], Rect: Rect{x, y, w, h}, Primary: m[1] == "*"})
			}
			if len(ms) > 0 {
				return ms, nil
			}
		}
	}
	out, err := runCmd("xdotool", "getdisplaygeometry")
	if err != nil {
		return nil, err
	}
	var w, h int
	if _, err := fmt.Sscanf(out, "%d %d", &w, &h); err != nil {
		return nil, fmt.Errorf("could not read the display size: %q", out)
	}
	return []Monitor{{Name: "screen", Rect: Rect{0, 0, w, h}, Primary: true}}, nil
}

func (linuxDriver) Capture(r Rect) (image.Image, error) {
	path := tempPNG()
	defer os.Remove(path)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := screenshot.CaptureScreen(ctx, path); err != nil {
		return nil, err
	}
	img, err := decodePNGFile(path)
	if err != nil {
		return nil, err
	}
	// The capture is the whole X screen, whose origin is (0, 0).
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return img, nil
	}
	return sub.SubImage(image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)), nil
}

func (linuxDriver) Cursor() (int, int, error) {
	out, err := runCmd("xdotool", "getmouselocation", "--shell")
	if err != nil {
		return 0, 0, err
	}
	var x, y int
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(line, "X="); ok {
			x, _ = strconv.Atoi(v)
		}
		if v, ok := strings.CutPrefix(line, "Y="); ok {
			y, _ = strconv.Atoi(v)
		}
	}
	return x, y, nil
}

func (linuxDriver) Move(x, y int) error {
	noteMove(x, y)
	_, err := runCmd("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y))
	return err
}

var xButtons = map[Button]string{Left: "1", Middle: "2", Right: "3"}

func (linuxDriver) Button(b Button, down bool) error {
	verb := "mouseup"
	if down {
		verb = "mousedown"
	}
	_, err := runCmd("xdotool", verb, xButtons[b])
	return err
}

func (linuxDriver) Scroll(dx, dy int) error {
	click := func(btn string, n int) error {
		_, err := runCmd("xdotool", "click", "--repeat", strconv.Itoa(n), "--delay", "30", btn)
		return err
	}
	switch {
	case dy > 0:
		return click("5", dy)
	case dy < 0:
		return click("4", -dy)
	case dx > 0:
		return click("7", dx)
	case dx < 0:
		return click("6", -dx)
	}
	return nil
}

func (linuxDriver) Type(text string) error {
	_, err := runCmd("xdotool", "type", "--delay", "8", "--", text)
	return err
}

var xKeysyms = map[string]string{
	"ctrl": "Control_L", "alt": "Alt_L", "shift": "Shift_L", "super": "Super_L",
	"enter": "Return", "escape": "Escape", "tab": "Tab", "space": "space", "backspace": "BackSpace",
	"delete": "Delete", "insert": "Insert", "home": "Home", "end": "End", "pageup": "Prior", "pagedown": "Next",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right", "capslock": "Caps_Lock",
	"printscreen": "Print", "menu": "Menu", "volumeup": "XF86AudioRaiseVolume", "volumedown": "XF86AudioLowerVolume",
	"volumemute": "XF86AudioMute", "playpause": "XF86AudioPlay",
	"+": "plus", "-": "minus", ",": "comma", ".": "period", "/": "slash", "\\": "backslash", ";": "semicolon",
	"'": "apostrophe", "`": "grave", "[": "bracketleft", "]": "bracketright", "=": "equal",
}

func (linuxDriver) Key(name string, down bool) error {
	sym, ok := xKeysyms[name]
	if !ok {
		if n, isF := fkey(name); isF {
			sym = "F" + strconv.Itoa(n)
		} else if len([]rune(name)) == 1 {
			sym = name
		} else {
			return fmt.Errorf("unknown key %q", name)
		}
	}
	verb := "keyup"
	if down {
		verb = "keydown"
	}
	_, err := runCmd("xdotool", verb, "--", sym)
	return err
}

func (linuxDriver) Windows() ([]Window, error) {
	if !have("wmctrl") {
		return nil, errors.New("listing windows needs wmctrl (e.g. sudo apt install wmctrl)")
	}
	out, err := runCmd("wmctrl", "-lpG")
	if err != nil {
		return nil, err
	}
	active := ""
	if a, err := runCmd("xdotool", "getactivewindow"); err == nil {
		if n, err := strconv.ParseUint(a, 10, 64); err == nil {
			active = fmt.Sprintf("0x%x", n)
		}
	}
	me := os.Getpid()
	var wins []Window
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		id := strings.ToLower(f[0])
		if n, err := strconv.ParseUint(strings.TrimPrefix(id, "0x"), 16, 64); err == nil {
			id = fmt.Sprintf("0x%x", n)
		}
		pid, _ := strconv.Atoi(f[2])
		x, _ := strconv.Atoi(f[3])
		y, _ := strconv.Atoi(f[4])
		w, _ := strconv.Atoi(f[5])
		h, _ := strconv.Atoi(f[6])
		title := ""
		if len(f) > 8 {
			title = strings.Join(f[8:], " ")
		}
		app := ""
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
			app = strings.TrimSpace(string(b))
		}
		wins = append(wins, Window{ID: id, Title: title, App: app, Rect: Rect{x, y, w, h}, Focused: id == active, Own: pid == me})
	}
	// Front-most first: the focused window, then the rest.
	for i, w := range wins {
		if w.Focused && i > 0 {
			wins = append([]Window{w}, append(wins[:i:i], wins[i+1:]...)...)
			break
		}
	}
	return wins, nil
}

func (linuxDriver) Focus(id string) error {
	if !have("wmctrl") {
		_, err := runCmd("xdotool", "windowactivate", "--sync", id)
		return err
	}
	_, err := runCmd("wmctrl", "-ia", id)
	return err
}

func (linuxDriver) Open(target string) error {
	var cmd *exec.Cmd
	switch {
	case looksLikePathOrURL(target):
		cmd = exec.Command("xdg-open", target)
	case have(target):
		cmd = exec.Command(target)
	default:
		cmd = exec.Command("gtk-launch", target)
	}
	proc.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not open %q: %w", target, err)
	}
	go cmd.Wait() //nolint:errcheck // detached
	return nil
}
