package tui

import (
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// huhTheme dresses Huh forms in the signal-console palette.
func huhTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		t := huh.ThemeBase(isDark)
		fg := lipgloss.LightDark(isDark)(lipgloss.Color("#303030"), lipgloss.Color("#D0D0D0"))

		t.Focused.Base = t.Focused.Base.BorderForeground(colLime).MarginLeft(2)
		t.Focused.Card = t.Focused.Base
		t.Focused.Title = t.Focused.Title.Foreground(colLime).Bold(true)
		t.Focused.NoteTitle = t.Focused.NoteTitle.Foreground(colLime).Bold(true)
		t.Focused.Description = t.Focused.Description.Foreground(colSlate)
		t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(colRed)
		t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(colRed)
		t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(colLime).SetString("▸ ")
		t.Focused.NextIndicator = t.Focused.NextIndicator.Foreground(colLime)
		t.Focused.PrevIndicator = t.Focused.PrevIndicator.Foreground(colLime)
		t.Focused.Option = t.Focused.Option.Foreground(fg)
		t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(colLime).Bold(true)
		t.Focused.SelectedPrefix = lipgloss.NewStyle().Foreground(colLime).SetString("● ")
		t.Focused.UnselectedPrefix = lipgloss.NewStyle().Foreground(colSlate).SetString("○ ")
		t.Focused.UnselectedOption = t.Focused.UnselectedOption.Foreground(fg)
		t.Focused.FocusedButton = t.Focused.FocusedButton.Foreground(colInk).Background(colLime).Bold(true)
		t.Focused.Next = t.Focused.FocusedButton
		t.Focused.BlurredButton = t.Focused.BlurredButton.Foreground(fg).Background(colRule)
		t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(colLime)
		t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(colSlate)
		t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(colLime)

		t.Blurred = t.Focused
		t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Card = t.Blurred.Base
		t.Blurred.NextIndicator = lipgloss.NewStyle()
		t.Blurred.PrevIndicator = lipgloss.NewStyle()

		t.Group.Title = t.Focused.Title
		t.Group.Description = t.Focused.Description
		t.Help = helpStyles()
		return t
	})
}

// formReq asks the UI to show a Huh form and reports back on done.
type formReq struct {
	form *huh.Form
	done chan bool // true when completed, false when aborted (esc / ctrl+c)
	// hotkeys answer the form with one key press (the gate's y / a / g / n).
	hotkeys map[string]func()
	// onYolo, when set, makes ctrl+y turn YOLO on and complete the form.
	onYolo func()
}

type formMsg struct{ req *formReq }

// runForm shows f in the live area and blocks the worker goroutine until
// the user completes or aborts it. Never call it from Update.
func (r *REPL) runForm(f *huh.Form, configure ...func(*formReq)) bool {
	if r.out == nil {
		return false
	}
	f = f.WithTheme(huhTheme()).WithShowHelp(true)
	req := &formReq{form: f, done: make(chan bool, 1)}
	for _, c := range configure {
		c(req)
	}
	r.post(formMsg{req: req})
	return <-req.done
}

// optionLabel lays out one picker row: ● label  detail  hint.
func optionLabel(it menuItem, labelW, width int) string {
	mark := "  "
	if it.Mark {
		mark = "● "
	}
	label := clip(it.Label, labelW)
	label += strings.Repeat(" ", max(labelW-visibleLen(label), 0))
	room := width - 10 - labelW - visibleLen(it.Hint)
	out := mark + label
	if it.Detail != "" && room > 6 {
		out += "  " + clip(it.Detail, room)
	}
	if it.Hint != "" {
		out += "  [" + it.Hint + "]"
	}
	return out
}

// choose shows a picker and returns the chosen item, or nil.
func (r *REPL) choose(title string, items []menuItem, sel int) *menuItem {
	if len(items) == 0 {
		return nil
	}
	labelW := 0
	for _, it := range items {
		labelW = max(labelW, visibleLen(it.Label))
	}
	labelW = min(labelW, 32)
	opts := make([]huh.Option[int], len(items))
	for i, it := range items {
		opts[i] = huh.NewOption(optionLabel(it, labelW, termWidth()), i)
	}
	choice := max(0, min(sel, len(items)-1))
	s := huh.NewSelect[int]().
		Title(title).
		Options(opts...).
		Value(&choice)
	if len(items) > 10 {
		s = s.Height(12).Description("/ filters")
	}
	if !r.runForm(huh.NewForm(huh.NewGroup(s))) {
		return nil
	}
	return &items[choice]
}

// readSecret asks for a value without showing it.
func (r *REPL) readSecret(title string) string {
	var v string
	in := huh.NewInput().Title(title).Description("hidden · enter to skip").
		EchoMode(huh.EchoModePassword).Value(&v)
	if !r.runForm(huh.NewForm(huh.NewGroup(in))) {
		return ""
	}
	return strings.TrimSpace(v)
}
