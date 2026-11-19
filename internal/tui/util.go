package tui

import (
	"fmt"
	"strings"
	"sync/atomic"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// screenWidth is kept up to date from tea.WindowSizeMsg so code running on
// the worker goroutine can size its output.
var screenWidth atomic.Int32

func termWidth() int {
	if w := int(screenWidth.Load()); w > 0 {
		return w
	}
	return 100
}

func stripANSI(s string) string { return ansi.Strip(s) }

// visibleLen is the number of terminal cells s takes, ignoring styling.
func visibleLen(s string) int { return lipgloss.Width(s) }

// clip shortens plain text to n cells with an ellipsis.
func clip(s string, n int) string {
	if n <= 1 || ansi.StringWidth(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

// fit clips a styled line to the terminal width so it never wraps.
func fit(s string) string {
	if w := termWidth() - 1; ansi.StringWidth(s) > w {
		return ansi.Truncate(s, w, "…")
	}
	return s
}

// oneLine collapses whitespace and clips to max cells.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 10 {
		return clip(s, max)
	}
	return s
}

// preview returns the first lines of s, each clipped to the terminal width.
func preview(s string, lines int) string {
	s = strings.TrimRight(s, "\n")
	parts := strings.Split(s, "\n")
	w := termWidth() - 8
	var out []string
	for i, p := range parts {
		if i == lines {
			out = append(out, fmt.Sprintf("+%d more lines", len(parts)-lines))
			break
		}
		if w > 10 {
			p = clip(p, w)
		}
		out = append(out, p)
	}
	return strings.Join(out, "\n")
}

// lastLines keeps the bottom n lines of s.
func lastLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
