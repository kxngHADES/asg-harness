package tui

import (
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
)

// keyMap holds every shortcut of the main screen, so the help line and the
// handlers can't drift apart.
type keyMap struct {
	Submit    key.Binding
	Newline   key.Binding
	Menu      key.Binding
	Mode      key.Binding
	Domain    key.Binding
	Yolo      key.Binding
	Image     key.Binding
	Stop      key.Binding
	Exit      key.Binding
	Up        key.Binding
	Down      key.Binding
	Complete  key.Binding
	CloseMenu key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Submit:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		Newline:   key.NewBinding(key.WithKeys("ctrl+j", "alt+enter", "shift+enter"), key.WithHelp("ctrl+j", "newline")),
		Menu:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "menu")),
		Mode:      key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "mode")),
		Domain:    key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "domain")),
		Yolo:      key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "yolo")),
		Image:     key.NewBinding(key.WithKeys("ctrl+v"), key.WithHelp("ctrl+v", "image")),
		Stop:      key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "stop")),
		Exit:      key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "exit")),
		Up:        key.NewBinding(key.WithKeys("up", "ctrl+p")),
		Down:      key.NewBinding(key.WithKeys("down", "ctrl+n")),
		Complete:  key.NewBinding(key.WithKeys("tab")),
		CloseMenu: key.NewBinding(key.WithKeys("esc")),
	}
}

// ShortHelp is the footer under the input.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Menu, k.Mode, k.Domain, k.Yolo, k.Stop, k.Image, k.Newline}
}

func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// inputKeys adapts the textarea's own bindings: enter sends instead of
// inserting a newline, and ctrl+t belongs to the domain switcher.
func inputKeys(k keyMap) textarea.KeyMap {
	km := textarea.DefaultKeyMap()
	km.InsertNewline = k.Newline
	km.TransposeCharacterBackward = key.NewBinding(key.WithDisabled())
	km.Paste = key.NewBinding(key.WithKeys("ctrl+shift+v")) // ctrl+v is image paste first
	return km
}
