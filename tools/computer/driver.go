package computer

import (
	"image"
	"strings"
)

// Monitor is one display. Rect is in screen coordinates.
type Monitor struct {
	Name    string `json:"name"`
	Rect    Rect   `json:"rect"`
	Primary bool   `json:"primary"`
}

// Window is a top-level window, front-most first.
type Window struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	App       string `json:"app"`
	Rect      Rect   `json:"rect"`
	Focused   bool   `json:"focused"`
	Minimized bool   `json:"minimized"`
	// Own marks windows of ASG itself (and the terminal it runs in, where
	// that can be told): the model may look at them but never send input.
	Own bool `json:"-"`
}

// Button is a mouse button.
type Button int

const (
	Left Button = iota
	Right
	Middle
)

func (b Button) String() string { return [...]string{"left", "right", "middle"}[b] }

// driver is an OS adapter. Windows is native (user32/gdi32); Linux and
// macOS drive the usual command-line helpers. Coordinates are screen
// coordinates in the space Monitors reports.
type driver interface {
	Monitors() ([]Monitor, error)
	// Capture returns the pixels of r; the image may be larger than r on
	// HiDPI screens.
	Capture(r Rect) (image.Image, error)
	Cursor() (x, y int, err error)
	Move(x, y int) error
	Button(b Button, down bool) error
	// Scroll scrolls by clicks: dy > 0 is down, dx > 0 is right.
	Scroll(dx, dy int) error
	// Type types literal text into the focused window.
	Type(text string) error
	// Key presses (down) or releases (up) one canonical key from keys.go.
	Key(name string, down bool) error
	Windows() ([]Window, error)
	Focus(id string) error
	// Open opens a file, folder, URL or app by name.
	Open(target string) error
}

// sys is the adapter for this OS, or nil with unsupported explaining why.
var sys driver

// held tracks buttons and keys pressed by key_down / mouse_down so an
// emergency stop (or the end of a turn) can release them.
var held = struct {
	keys    map[string]bool
	buttons map[Button]bool
}{keys: map[string]bool{}, buttons: map[Button]bool{}}

// releaseAll lets go of everything ASG is holding down.
func releaseAll() {
	if sys == nil {
		return
	}
	heldMu.Lock()
	keys, buttons := held.keys, held.buttons
	held.keys, held.buttons = map[string]bool{}, map[Button]bool{}
	heldMu.Unlock()
	for b := range buttons {
		_ = sys.Button(b, false)
	}
	for k := range keys {
		_ = sys.Key(k, false)
	}
}

// matchesApp reports whether a window belongs to an excluded app: the
// pattern matches the app (process) name, or appears in the title.
func matchesApp(w Window, patterns []string) bool {
	app := strings.ToLower(strings.TrimSuffix(strings.ToLower(w.App), ".exe"))
	title := strings.ToLower(w.Title)
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		p = strings.TrimSuffix(p, ".exe")
		if p == "" {
			continue
		}
		if app == p || strings.Contains(title, p) {
			return true
		}
	}
	return false
}
