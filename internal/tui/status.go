package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

// The status strip sits right above the input: the mode switcher, the work
// track, the model, YOLO and attached images. It is part of the live view,
// so Shift+Tab and Ctrl+T simply change state and the strip follows.

// modeColor gives each mode a colour of its own.
func modeColor(m agent.Mode) color.Color {
	switch m {
	case agent.ModePlan:
		return colCyan
	case agent.ModeAsk:
		return colMagenta
	case agent.ModeLoop:
		return colAmber
	}
	return colLime
}

// domainColor gives each domain a colour of its own.
func domainColor(d agent.Domain) color.Color {
	switch d {
	case agent.DomainDocs:
		return colCyan
	case agent.DomainCyber:
		return colRed
	case agent.DomainDevOps:
		return colAmber
	case agent.DomainPM:
		return colMagenta
	}
	return colLime
}

func domainStyle(d agent.Domain) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(domainColor(d)).Bold(true)
}

func (r *REPL) mode() agent.Mode {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	if r.sess == nil || r.sess.Mode == "" {
		return agent.ModeBuild
	}
	return r.sess.Mode
}

func (r *REPL) setMode(m agent.Mode) {
	r.stateMu.Lock()
	r.sess.Mode = m
	r.stateMu.Unlock()
}

// SetMode picks the mode the session starts in (the -mode flag).
func (r *REPL) SetMode(m agent.Mode) { r.setMode(m) }

func (r *REPL) track() agent.Track {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()
	if r.sess == nil || r.sess.Track.Domain == "" {
		return agent.Track{Domain: agent.DomainSoftware, Flavor: agent.Workflows[0].Flavors[0].Name}
	}
	return r.sess.Track
}

// setTrack switches domain/flavor and saves it as the default.
func (r *REPL) setTrack(t agent.Track) {
	r.stateMu.Lock()
	r.sess.Track = t
	r.sess.Work = nil // a new kind of work gets its own work folder
	r.stateMu.Unlock()
	r.update(func(c *config.Config) error { c.Domain, c.Flavor = string(t.Domain), t.Flavor; return nil })
}

// cycleDomain moves to the next domain with its default flavor.
func (r *REPL) cycleDomain() {
	d := agent.NextDomain(r.track().Domain)
	r.setTrack(agent.Track{Domain: d, Flavor: agent.WorkflowFor(d).Flavors[0].Name})
}

// statusStrip renders: mode tabs │ track │ model ⚡YOLO ▣ n · what it does.
func (r *REPL) statusStrip(width int) string {
	m := r.mode()
	var b strings.Builder
	b.WriteString("  ")
	for _, x := range agent.Modes {
		b.WriteString(modeTab(x, x == m))
	}
	t := r.track()
	sep := slate(" │ ")
	b.WriteString(sep + domainStyle(t.Domain).Render(t.String()))
	cfg := r.app.Config.Get()
	if _, pc := cfg.Active(); pc != nil && pc.Model != "" {
		b.WriteString(sep + slate(clip(pc.Model, 28)))
	}
	if r.yolo() {
		b.WriteString(" " + badge(colAmber, "⚡ YOLO"))
	}
	if cb := computerBadge(r.computerState()); cb != "" {
		b.WriteString(" " + cb)
	}
	if n := r.pendingCount(); n > 0 {
		b.WriteString(" " + magenta(fmt.Sprintf("▣ %d img", n)))
	}
	desc := m.Describe()
	if m == agent.ModeBuild || m == agent.ModeLoop {
		desc = t.FlavorSpec().Describe
	}
	if room := width - visibleLen(b.String()) - 4; room > 8 {
		b.WriteString(slate(" · " + clip(desc, room)))
	}
	return b.String()
}

// trackLine describes the track after it changes.
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
		more = slate(" · also: " + strings.Join(others, ", "))
	}
	return "  " + badge(domainColor(t.Domain), strings.ToUpper(t.String())) + " " + slate(f.Describe) + more
}
