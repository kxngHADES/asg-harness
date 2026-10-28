package tui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// The "signal console" palette: acid lime for ASG itself, cyan for tools,
// magenta for skills, amber for permission gates. 256-colour codes so it
// works in every modern terminal; NO_COLOR disables styling entirely.
var (
	reset   = "\x1b[0m"
	bold    = "\x1b[1m"
	dim     = "\x1b[2m"
	italic  = "\x1b[3m"
	red     = "\x1b[38;5;203m"
	green   = "\x1b[38;5;154m"
	lime    = "\x1b[38;5;154m"
	cyan    = "\x1b[38;5;45m"
	magenta = "\x1b[38;5;207m"
	yellow  = "\x1b[38;5;214m"
	amber   = "\x1b[38;5;214m"
	slate   = "\x1b[38;5;245m"
	onLime  = "\x1b[48;5;154m\x1b[38;5;16m"
)

func init() {
	if os.Getenv("NO_COLOR") != "" {
		for _, p := range []*string{&reset, &bold, &dim, &italic, &red, &green, &lime, &cyan, &magenta, &yellow, &amber, &slate, &onLime} {
			*p = ""
		}
	}
}

const gutter = "┃ "

var (
	reBold   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reInline = regexp.MustCompile("`([^`]+)`")
)

func termWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 100
	}
	return w
}

// wordmark prints the ASG block logo with a lime→cyan gradient.
func wordmark() string {
	rows := []string{
		"▄▀█ █▀ █▀▀",
		"█▀█ ▄█ █▄█",
	}
	ramp := []int{154, 154, 118, 84, 49, 50, 51, 45, 45, 39}
	var b strings.Builder
	for _, row := range rows {
		b.WriteString("  ")
		for i, r := range []rune(row) {
			if reset == "" {
				b.WriteRune(r)
				continue
			}
			fmt.Fprintf(&b, "\x1b[38;5;%dm%c", ramp[i%len(ramp)], r)
		}
		b.WriteString(reset + "\n")
	}
	return b.String()
}

// mdStream prints streamed markdown inside the ASG gutter. Text is written
// as it arrives; once a line is complete it is redrawn with styling when it
// fits on one row.
type mdStream struct {
	line    strings.Builder
	inCode  bool
	width   int
	started bool // gutter printed for the current line
}

func newMDStream() *mdStream { return &mdStream{width: termWidth()} }

func (m *mdStream) gutter() {
	if !m.started {
		fmt.Print(lime + gutter + reset)
		m.started = true
	}
}

func (m *mdStream) Write(delta string) {
	for {
		i := strings.IndexByte(delta, '\n')
		if i < 0 {
			if delta != "" {
				m.gutter()
			}
			m.line.WriteString(delta)
			fmt.Print(delta)
			return
		}
		m.gutter()
		m.line.WriteString(delta[:i])
		fmt.Print(delta[:i])
		m.endLine()
		delta = delta[i+1:]
	}
}

func (m *mdStream) endLine() {
	raw := m.line.String()
	m.line.Reset()
	styled := m.style(raw)
	if utf8.RuneCountInString(raw)+len([]rune(gutter)) < m.width {
		fmt.Print("\r\x1b[2K" + lime + gutter + reset + styled)
	}
	fmt.Print("\n")
	m.started = false
}

// Flush finishes a trailing partial line.
func (m *mdStream) Flush() {
	if m.line.Len() > 0 || m.started {
		m.endLine()
	}
}

func (m *mdStream) style(line string) string {
	trim := strings.TrimSpace(line)
	if strings.HasPrefix(trim, "```") {
		m.inCode = !m.inCode
		lang := strings.TrimPrefix(trim, "```")
		if m.inCode {
			return slate + "┌─ " + lang + reset
		}
		return slate + "└─" + reset
	}
	if m.inCode {
		return slate + "│ " + reset + cyan + line + reset
	}
	switch {
	case strings.HasPrefix(trim, "#"):
		return onLime + " " + strings.ToUpper(strings.TrimLeft(trim, "# ")) + " " + reset
	case strings.HasPrefix(trim, "> "):
		return slate + italic + "▏" + trim[2:] + reset
	case strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* "):
		indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
		line = indent + lime + "▪ " + reset + trim[2:]
	}
	line = reBold.ReplaceAllString(line, bold+"$1"+reset)
	line = reInline.ReplaceAllString(line, cyan+"$1"+reset)
	return line
}

// spinner shows a scanning bar until stopped.
type spinner struct {
	stop chan struct{}
	done sync.WaitGroup
	once sync.Once
}

func scanFrame(i int) string {
	const cells = 7
	pos := i % (2 * (cells - 1))
	if pos >= cells {
		pos = 2*(cells-1) - pos
	}
	var b strings.Builder
	for c := 0; c < cells; c++ {
		if c == pos {
			b.WriteString(lime + "▰")
		} else {
			b.WriteString(slate + "▱")
		}
	}
	return b.String() + reset
}

func startSpinner(label string) *spinner {
	s := &spinner{stop: make(chan struct{})}
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		t := time.NewTicker(80 * time.Millisecond)
		defer t.Stop()
		start := time.Now()
		for i := 0; ; i++ {
			fmt.Printf("\r  %s %s%s%s %s%.1fs%s", scanFrame(i), cyan, label, reset, slate, time.Since(start).Seconds(), reset)
			select {
			case <-s.stop:
				fmt.Print("\r\x1b[2K")
				return
			case <-t.C:
			}
		}
	}()
	return s
}

func (s *spinner) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.stop)
		s.done.Wait()
	})
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
		if utf8.RuneCountInString(p) > w && w > 10 {
			p = string([]rune(p)[:w]) + "…"
		}
		out = append(out, p)
	}
	return strings.Join(out, "\n")
}
