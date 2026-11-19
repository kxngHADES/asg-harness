package tui

import (
	"image/color"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// The "signal console" palette: acid lime for ASG itself, cyan for tools,
// magenta for skills and questions, amber for permission gates and the
// loop, red for errors. Every colour and style in the TUI comes from this
// file (see AGENTS.md). Bubble Tea downsamples colours to what the terminal
// supports and honours NO_COLOR, so nothing here detects the terminal.
var (
	colLime    = lipgloss.Color("#AFFF00")
	colCyan    = lipgloss.Color("#00D7FF")
	colMagenta = lipgloss.Color("#FF5FFF")
	colAmber   = lipgloss.Color("#FFAF00")
	colRed     = lipgloss.Color("#FF5F5F")
	colSlate   = lipgloss.Color("#8A8A8A")
	colRule    = lipgloss.Color("#3A3A3A")
	colInk     = lipgloss.Color("#000000")
)

func paint(c color.Color) func(string) string {
	st := lipgloss.NewStyle().Foreground(c)
	return func(s string) string { return st.Render(s) }
}

func strongPaint(c color.Color) func(string) string {
	st := lipgloss.NewStyle().Foreground(c).Bold(true)
	return func(s string) string { return st.Render(s) }
}

// Foreground helpers. Don't nest them (an inner reset ends the outer
// colour); concatenate styled pieces instead.
var (
	lime    = paint(colLime)
	cyan    = paint(colCyan)
	magenta = paint(colMagenta)
	amber   = paint(colAmber)
	red     = paint(colRed)
	slate   = paint(colSlate)
	green   = lime
	yellow  = amber

	limeBold    = strongPaint(colLime)
	cyanBold    = strongPaint(colCyan)
	magentaBold = strongPaint(colMagenta)
	amberBold   = strongPaint(colAmber)

	boldStyle   = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	italicStyle = lipgloss.NewStyle().Italic(true).Foreground(colSlate)
)

func bold(s string) string   { return boldStyle.Render(s) }
func dim(s string) string    { return dimStyle.Render(s) }
func italic(s string) string { return italicStyle.Render(s) }

// badge is text on a coloured background, e.g. the mode tab or " GATE ".
func badge(c color.Color, s string) string {
	return lipgloss.NewStyle().Background(c).Foreground(colInk).Bold(true).Padding(0, 1).Render(s)
}

// Boxes printed into the scrollback.
var (
	gateBox = lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(colAmber).
		PaddingLeft(1).MarginLeft(2)
	askBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colMagenta).
		Padding(0, 1).MarginLeft(2)
	echoStyle = lipgloss.NewStyle().Foreground(colLime).Bold(true)
	ruleStyle = lipgloss.NewStyle().Foreground(colRule)
)

// gutter marks ASG's own replies in the scrollback.
var gutter = lime("┃") + " "

func gutterLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = gutter + l
	}
	return strings.Join(lines, "\n")
}

func rule(width int) string {
	return ruleStyle.Render(strings.Repeat("─", max(width, 1)))
}

func helpStyles() help.Styles {
	s := help.DefaultDarkStyles()
	s.ShortKey = lipgloss.NewStyle().Foreground(colLime)
	s.ShortDesc = lipgloss.NewStyle().Foreground(colSlate)
	s.ShortSeparator = lipgloss.NewStyle().Foreground(colRule)
	s.Ellipsis = s.ShortSeparator
	return s
}

// richColor reports whether the terminal can show the pixel mascot: it
// needs at least 256 colours (NO_COLOR and dumb terminals get the one-line
// crab or nothing).
var richColor = func() bool {
	p := colorprofile.Detect(os.Stdout, os.Environ())
	return p == colorprofile.ANSI256 || p == colorprofile.TrueColor
}()

// darkBackground is asked once, before Bubble Tea takes the terminal.
var darkBackground = true

func detectBackground() {
	defer func() { _ = recover() }() // a terminal that can't answer is fine
	darkBackground = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
}
