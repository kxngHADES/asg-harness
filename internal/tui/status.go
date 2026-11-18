package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

// The status strip is one line above the prompt showing the mode switcher
// and the work track. Shift+Tab and Ctrl+T redraw it in place instead of
// printing a new line on every press, so cycling stays tidy.

// domainColor gives each domain a colour of its own.
func domainColor(d agent.Domain) string {
	switch d {
	case agent.DomainDocs:
		return cyan
	case agent.DomainCyber:
		return red
	case agent.DomainDevOps:
		return amber
	case agent.DomainPM:
		return magenta
	}
	return lime
}

// modeBadge is the mode as a fixed-width coloured badge, so the prompt
// doesn't jump around when the mode changes.
func modeBadge(m agent.Mode) string {
	return onColor(modeColor(m)) + bold + fmt.Sprintf(" %-5s ", strings.ToUpper(string(m))) + reset
}

func (r *REPL) track() agent.Track {
	if r.sess == nil || r.sess.Track.Domain == "" {
		return agent.Track{Domain: agent.DomainSoftware, Flavor: agent.Workflows[0].Flavors[0].Name}
	}
	return r.sess.Track
}

// statusStrip renders: mode switcher │ track — what the mode does.
func (r *REPL) statusStrip() string {
	width := termWidth() - 1
	m := r.mode()
	var b strings.Builder
	plain := 2
	b.WriteString("  ")
	for _, x := range agent.Modes {
		label := " " + string(x) + " "
		if x == m {
			b.WriteString(onColor(modeColor(x)) + bold + label + reset)
		} else {
			b.WriteString(slate + label + reset)
		}
		plain += len(label)
	}
	t := r.track()
	tr := t.String()
	fmt.Fprintf(&b, " %s│%s %s%s%s", slate, reset, domainColor(t.Domain)+bold, tr, reset)
	plain += 3 + utf8.RuneCountInString(tr)
	desc := m.Describe()
	if m == agent.ModeBuild || m == agent.ModeLoop {
		desc = t.FlavorSpec().Describe
	}
	if room := width - plain - 3; room > 8 {
		fmt.Fprintf(&b, " %s· %s%s", slate, clip(desc, room), reset)
	}
	return b.String()
}

// showStrip prints the status strip above the prompt while the line
// editor is active, replacing the strip it printed last time.
func (r *REPL) showStrip() {
	s := r.statusStrip() + "\r\n"
	if r.stripShown {
		s = "\x1b[1A\r\x1b[2K" + s // go up to the old strip and overwrite it
	}
	r.t.SetPrompt(r.prompt())
	fmt.Fprint(r.t, s) // the editor clears its line, writes s, redraws prompt + input
	r.stripShown = true
}

// printAbove prints a line above the prompt while the editor is active.
func (r *REPL) printAbove(s string) {
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	fmt.Fprint(r.t, strings.ReplaceAll(strings.TrimSuffix(s, "\n"), "\n", "\r\n")+"\r\n")
	r.stripShown = false
}

// setTrack switches domain/flavor and saves it as the default.
func (r *REPL) setTrack(t agent.Track) {
	r.sess.Track = t
	r.sess.Work = nil // a new kind of work gets its own work folder
	r.update(func(c *config.Config) error { c.Domain, c.Flavor = string(t.Domain), t.Flavor; return nil })
}

// cycleDomain moves to the next domain with its default flavor.
func (r *REPL) cycleDomain() {
	d := agent.NextDomain(r.track().Domain)
	r.setTrack(agent.Track{Domain: d, Flavor: agent.WorkflowFor(d).Flavors[0].Name})
}

// trackLine describes the track after it changes (outside the editor).
func (r *REPL) trackLine() string {
	t := r.track()
	f := t.FlavorSpec()
	var others []string
	for _, x := range t.Workflow().Flavors {
		if x.Name != f.Name {
			others = append(others, x.Name)
		}
	}
	more := ""
	if len(others) > 0 {
		more = fmt.Sprintf(" %s· also: %s%s", slate, strings.Join(others, ", "), reset)
	}
	return fmt.Sprintf("  %s%s %s %s %s%s%s%s\n", bold, onColor(domainColor(t.Domain)), strings.ToUpper(t.String()), reset, slate, f.Describe, reset, more)
}
