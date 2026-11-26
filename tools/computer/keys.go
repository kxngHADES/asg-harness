package computer

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// keyAliases maps the many names models use for a key to one canonical
// name. Single printable characters ("a", "7", "/") are their own name.
var keyAliases = map[string]string{
	"ctrl": "ctrl", "control": "ctrl", "ctl": "ctrl", "lctrl": "ctrl", "rctrl": "ctrl",
	"alt": "alt", "option": "alt", "opt": "alt", "lalt": "alt", "ralt": "alt", "altgr": "alt",
	"shift": "shift", "lshift": "shift", "rshift": "shift",
	"super": "super", "win": "super", "windows": "super", "cmd": "super", "command": "super", "meta": "super", "lwin": "super", "rwin": "super",
	"enter": "enter", "return": "enter", "kp_enter": "enter",
	"esc": "escape", "escape": "escape",
	"tab": "tab", "space": "space", "spacebar": "space",
	"backspace": "backspace", "bksp": "backspace", "back_space": "backspace",
	"delete": "delete", "del": "delete",
	"insert": "insert", "ins": "insert",
	"home": "home", "end": "end",
	"pageup": "pageup", "page_up": "pageup", "pgup": "pageup", "prior": "pageup",
	"pagedown": "pagedown", "page_down": "pagedown", "pgdn": "pagedown", "next": "pagedown",
	"up": "up", "arrowup": "up", "arrow_up": "up", "uparrow": "up",
	"down": "down", "arrowdown": "down", "arrow_down": "down", "downarrow": "down",
	"left": "left", "arrowleft": "left", "arrow_left": "left", "leftarrow": "left",
	"right": "right", "arrowright": "right", "arrow_right": "right", "rightarrow": "right",
	"capslock": "capslock", "caps_lock": "capslock",
	"printscreen": "printscreen", "print": "printscreen", "prtsc": "printscreen", "print_screen": "printscreen",
	"menu": "menu", "apps": "menu", "contextmenu": "menu",
	"plus": "+", "minus": "-", "comma": ",", "period": ".", "slash": "/", "backslash": "\\",
	"semicolon": ";", "apostrophe": "'", "quote": "'", "grave": "`", "backtick": "`",
	"bracketleft": "[", "bracketright": "]", "equal": "=", "equals": "=",
	"volumeup": "volumeup", "volumedown": "volumedown", "volumemute": "volumemute", "mute": "volumemute",
	"playpause": "playpause", "media_play_pause": "playpause",
}

// modifiers are pressed first and released last in a combo.
var modifiers = map[string]bool{"ctrl": true, "alt": true, "shift": true, "super": true}

// Combo is one chord such as ctrl+shift+t: modifiers, then the main key.
type Combo []string

func (c Combo) String() string { return strings.Join(c, "+") }

// ParseKeys turns "ctrl+c", "alt+tab", "Return" or a sequence such as
// "ctrl+a ctrl+c" into chords of canonical key names.
func ParseKeys(s string) ([]Combo, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("no keys given")
	}
	var out []Combo
	for _, chord := range strings.Fields(s) {
		c, err := parseChord(chord)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

func parseChord(chord string) (Combo, error) {
	var parts []string
	// "ctrl++" means ctrl and the plus key; "+" alone is the plus key.
	switch {
	case chord == "+":
		parts = []string{"+"}
	case strings.HasSuffix(chord, "++"):
		parts = append(strings.Split(strings.TrimSuffix(chord, "++"), "+"), "+")
	default:
		parts = strings.Split(chord, "+")
	}
	var mods, keys []string
	for _, p := range parts {
		k, err := keyName(p)
		if err != nil {
			return nil, err
		}
		if modifiers[k] {
			if !contains(mods, k) {
				mods = append(mods, k)
			}
		} else {
			keys = append(keys, k)
		}
	}
	if len(keys) > 1 {
		return nil, fmt.Errorf("%q presses %d keys at once — use one key per chord (e.g. \"ctrl+c\"), separate chords with spaces", chord, len(keys))
	}
	var ordered []string // a fixed modifier order so "shift+ctrl+x" == "ctrl+shift+x"
	for _, m := range []string{"ctrl", "alt", "shift", "super"} {
		if contains(mods, m) {
			ordered = append(ordered, m)
		}
	}
	return append(ordered, keys...), nil
}

func keyName(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty key in combo")
	}
	if utf8.RuneCountInString(p) == 1 {
		return strings.ToLower(p), nil
	}
	l := strings.ToLower(p)
	if k, ok := keyAliases[l]; ok {
		return k, nil
	}
	if n, ok := fkey(l); ok {
		return fmt.Sprintf("f%d", n), nil
	}
	return "", fmt.Errorf("unknown key %q (use names like ctrl, alt, shift, super, enter, escape, tab, space, backspace, delete, up, down, pageup, home, f5, or a single character)", p)
}

// fkey parses f1…f24.
func fkey(l string) (int, bool) {
	var n int
	if _, err := fmt.Sscanf(l, "f%d", &n); err != nil || fmt.Sprintf("f%d", n) != l || n < 1 || n > 24 {
		return 0, false
	}
	return n, true
}

// sensitiveCombos close, quit, delete or lock things — the gate always asks.
var sensitiveCombos = map[string]bool{
	"alt+f4": true, "ctrl+w": true, "ctrl+q": true, "super+q": true, "super+w": true,
	"shift+delete": true, "ctrl+shift+delete": true, "super+l": true, "ctrl+alt+delete": true,
	"super+backspace": true, "super+shift+q": true,
}

// sensitiveKeys reports whether any chord in s is one of sensitiveCombos.
func sensitiveKeys(s string) bool {
	combos, err := ParseKeys(s)
	if err != nil {
		return false
	}
	for _, c := range combos {
		if sensitiveCombos[c.String()] {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
