package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kxngHADES/asg-harness/internal/config"
	"github.com/kxngHADES/asg-harness/tools/computer"
)

// watchComputer follows computer-use state for the status strip, prints
// when control pauses or stops, and cancels the turn on an emergency stop.
// It returns a function that stops watching.
func (r *REPL) watchComputer() func() {
	var last computer.State
	unsub := computer.Subscribe(func(s computer.State) {
		r.mu.Lock()
		r.comp = s
		r.mu.Unlock()
		switch {
		case s.Stopped && !last.Stopped:
			r.printf("  %s %s", badge(colRed, "■ computer"), red("control stopped — "+s.Reason))
		case s.Paused && !last.Paused:
			r.printf("  %s %s", badge(colAmber, "⏸ computer"), amber("control paused — "+s.Reason+" (ASG will ask before it continues)"))
		case s.Active && !last.Active:
			hint := "ctrl+c stops"
			if s.StopKey != "" {
				hint = s.StopKey + " or ctrl+c stops it · grab the mouse to pause"
			}
			r.printf("  %s %s", badge(colAmber, "🖱 computer"), slate("ASG is controlling your mouse and keyboard — "+hint))
		}
		last = s
		r.post(computerMsg{})
	})
	stop := computer.OnEmergencyStop(r.interrupt)
	return func() { unsub(); stop() }
}

// computerMsg asks the view to redraw the status strip.
type computerMsg struct{}

func (r *REPL) computerState() computer.State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.comp
}

// computerBadge is the status-strip marker while ASG controls the computer.
func computerBadge(s computer.State) string {
	switch {
	case !s.Active:
		return ""
	case s.Stopped:
		return badge(colRed, "■ computer stopped")
	case s.Paused:
		return badge(colSlate, "⏸ computer paused")
	}
	label := "🖱 computer"
	if s.StopKey != "" {
		label += " · " + s.StopKey + " stops"
	}
	return badge(colAmber, label)
}

// computerCommand handles /computer [on|off|status|monitor <n>|approval <level>].
func (r *REPL) computerCommand(args []string) {
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}
	switch sub {
	case "on", "off":
		if ok, why := computer.Supported(); !ok && sub == "on" {
			r.printf("  %s", yellow(why))
		}
		r.update(func(c *config.Config) error { c.Computer.Enabled = sub == "on"; return nil })
		if sub == "on" {
			r.printf("  %s %s", green("✓ computer use on"), slate("— ASG can now see your screen and use your mouse and keyboard; every action asks first unless you allow it (approval: "+r.app.Config.Get().Computer.Approval+")"))
		} else {
			r.printf("  %s", slate("computer use off"))
		}
		return
	case "monitor":
		if len(args) < 2 {
			r.printf("  usage: /computer monitor <n>  (0 = primary, -1 = all monitors)")
			return
		}
		n, err := strconv.Atoi(args[1])
		if err != nil || n < -1 {
			r.printf("  %s", yellow("monitor must be a number: 0 = primary, 1… the others, -1 = all"))
			return
		}
		r.update(func(c *config.Config) error { c.Computer.Monitor = n; return nil })
	case "approval":
		lv := ""
		if len(args) > 1 {
			lv = strings.ToLower(args[1])
		}
		switch lv {
		case config.ComputerApproveActions, config.ComputerApproveEvery, config.ComputerApproveAuto:
			r.update(func(c *config.Config) error { c.Computer.Approval = lv; return nil })
		default:
			r.printf("  usage: /computer approval actions|every|auto  (sensitive actions always ask)")
			return
		}
	case "", "status":
	default:
		r.printf("  usage: /computer [on|off|status|monitor <n>|approval actions|every|auto]")
		return
	}
	c := r.app.Config.Get().Computer
	state := red("off")
	if c.Enabled {
		state = green("on")
	}
	vision := "the chat model looks at screenshots"
	if c.VisionModel != "" {
		vision = "vision model " + c.VisionModel
		if c.VisionProvider != "" {
			vision += " on " + c.VisionProvider
		}
	}
	supported, why := computer.Supported()
	plat := slate(why)
	if !supported {
		plat = yellow(why)
	}
	excl := "none"
	if len(c.ExcludedApps) > 0 {
		excl = strings.Join(c.ExcludedApps, ", ")
	}
	r.printf("  computer use: %s · platform: %s\n  monitor: %d · approval: %s · max %d actions/turn · %d min/session\n  perception: %s\n  excluded apps: %s · excluded monitors: %v\n  %s",
		state, plat, c.Monitor, c.Approval, c.MaxActions, c.SessionMinutes, vision, excl, c.ExcludedMonitors,
		slate(fmt.Sprintf("more settings (vision_model, excluded_apps, move_ms, …) under \"computer\" in %s", r.app.Config.Path())))
}
