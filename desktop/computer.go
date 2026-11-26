package main

import (
	"github.com/kxngHADES/asg-harness/tools/computer"
)

// Computer use: the desktop shows a banner with Pause / Resume / Stop
// while ASG controls the mouse and keyboard ("asg:computer" events), and
// an emergency stop (Esc, the banner, or the phone) stops every running
// turn.

type ComputerInfo struct {
	Supported bool               `json:"supported"`
	Platform  string             `json:"platform"` // or why it isn't supported
	Monitors  []computer.Monitor `json:"monitors"`
	State     computer.State     `json:"state"`
}

// ComputerInfo describes computer use on this machine for the settings page.
func (a *App) ComputerInfo() ComputerInfo {
	ok, why := computer.Supported()
	info := ComputerInfo{Supported: ok, Platform: why, State: computer.Current(), Monitors: []computer.Monitor{}}
	if ok {
		if ms, err := computer.ListMonitors(); err == nil {
			info.Monitors = ms
		}
	}
	return info
}

func (a *App) ComputerPause()  { computer.Pause("paused from the ASG app") }
func (a *App) ComputerResume() { computer.Resume() }
func (a *App) ComputerStop()   { computer.Stop("stopped from the ASG app") }

// startComputer forwards state to the UI and wires the emergency stop. It
// returns a function that undoes both.
func (a *App) startComputer() func() {
	unsub := computer.Subscribe(func(s computer.State) { a.emit("asg:computer", s) })
	unstop := computer.OnEmergencyStop(a.stopAll)
	return func() { unsub(); unstop() }
}

// stopAll cancels every running turn.
func (a *App) stopAll() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, ls := range a.live {
		ls.mu.Lock()
		if ls.cancel != nil {
			ls.cancel()
		}
		ls.mu.Unlock()
	}
}
