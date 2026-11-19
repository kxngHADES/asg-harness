package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

type menuItem struct {
	Label  string // main text, e.g. "/model" or a model name
	Detail string // dimmed description
	Hint   string // short right-hand tag, e.g. "no key"
	Mark   bool   // the current choice (●)
	Value  string
}

// filterItems keeps the items matching filter: an exact match first, then
// label prefix matches, then labels or details containing it.
func filterItems(items []menuItem, filter string) []menuItem {
	f := strings.ToLower(strings.TrimPrefix(filter, "/"))
	if f == "" {
		return items
	}
	var exact, prefix, sub []menuItem
	for _, it := range items {
		l := strings.ToLower(strings.TrimPrefix(it.Label, "/"))
		switch {
		case l == f:
			exact = append(exact, it)
		case strings.HasPrefix(l, f):
			prefix = append(prefix, it)
		case strings.Contains(l, f) || strings.Contains(strings.ToLower(it.Detail), f):
			sub = append(sub, it)
		}
	}
	return append(append(exact, prefix...), sub...)
}

// slashItems lists the commands and installed skills for the '/' menu.
func (r *REPL) slashItems() []menuItem {
	var items []menuItem
	for _, c := range commands {
		items = append(items, menuItem{Label: c.name, Detail: c.help, Value: c.name})
	}
	for _, s := range r.app.Skills.List() {
		items = append(items, menuItem{Label: "/" + s.Name, Detail: s.Description, Hint: "skill", Value: "/" + s.Name})
	}
	return items
}

// menuNeedsArg reports whether choosing value should insert it for editing
// rather than run it (commands that are useless without arguments, skills).
func (r *REPL) menuNeedsArg(value string) bool {
	for _, c := range commands {
		if c.name == value {
			return c.needsArg()
		}
	}
	return true // skills take a request
}

// slashFilter returns the typed filter when the '/' menu should be open:
// the input is a single word starting with '/'.
func slashFilter(input string) (string, bool) {
	if !strings.HasPrefix(input, "/") || strings.ContainsAny(input, " \n\t") || strings.HasPrefix(input, "//") {
		return "", false
	}
	return input, true
}

const menuRows = 8

var (
	menuSel    = lipgloss.NewStyle().Foreground(colLime).Bold(true)
	menuLabel  = lipgloss.NewStyle().Foreground(colLime)
	menuDetail = lipgloss.NewStyle().Foreground(colSlate)
	menuHint   = lipgloss.NewStyle().Foreground(colAmber)
)

// renderMenu draws the dropdown under the input.
func renderMenu(vis []menuItem, sel, width int) string {
	if len(vis) == 0 {
		return "    " + slate("no matching command — enter sends it as typed")
	}
	top := 0
	if sel >= menuRows {
		top = sel - menuRows + 1
	}
	labelW := 0
	for _, it := range vis {
		labelW = max(labelW, visibleLen(it.Label))
	}
	labelW = min(labelW, 28)
	var b strings.Builder
	for i := top; i < len(vis) && i < top+menuRows; i++ {
		it := vis[i]
		label := clip(it.Label, labelW)
		label += strings.Repeat(" ", max(labelW-visibleLen(label), 0))
		pre := "    "
		if i == sel {
			pre = "  " + lime("▸") + " "
			label = menuSel.Render(label)
		} else {
			label = menuLabel.Render(label)
		}
		room := width - 4 - labelW - 2 - visibleLen(it.Hint) - 3
		line := pre + label
		if it.Detail != "" && room > 6 {
			line += "  " + menuDetail.Render(clip(it.Detail, room))
		}
		if it.Hint != "" {
			line += "  " + menuHint.Render(it.Hint)
		}
		b.WriteString(line + "\n")
	}
	more := ""
	if len(vis) > menuRows {
		more = fmt.Sprintf("  %d/%d", sel+1, len(vis))
	}
	b.WriteString("    " + slate("↑↓ move · tab complete · enter run · esc close"+more))
	return b.String()
}
