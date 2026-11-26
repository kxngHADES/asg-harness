package computer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// State is what the front ends show while ASG controls the computer: a
// banner or status line, and pause / resume / stop buttons.
type State struct {
	// Active is true while a turn is using the mouse and keyboard.
	Active bool `json:"active"`
	// Paused means actions are refused until Resume (or an ask_user answer).
	Paused bool `json:"paused"`
	// Stopped means the user hit the emergency stop; it lasts until the next turn.
	Stopped bool `json:"stopped"`
	// Action describes the last action, e.g. "left_click (640, 300)".
	Action  string `json:"action,omitempty"`
	Actions int    `json:"actions"`
	// Reason says why control is paused or stopped.
	Reason string `json:"reason,omitempty"`
	// StopKey names the physical key that stops control, if the platform has one.
	StopKey string `json:"stop_key,omitempty"`
}

// Errors the model sees; they tell it what to do next.
var (
	errStopped = errors.New("computer control was stopped by the user (emergency stop). Do not try again: end your turn and tell the user what you did and where you stopped")
	errPaused  = "computer control is paused (%s). Do not send more actions: call ask_user to ask whether to continue (control resumes once they answer), or end your turn"
)

// idleEnd ends a run when a turn stops using the computer for this long
// without finishing (e.g. a context that is never cancelled).
const idleEnd = 3 * time.Minute

type controller struct {
	mu     sync.Mutex
	st     State
	run    *run
	subs   map[int]func(State)
	stops  map[int]func()
	nextID int

	// acting is set while an action sends input; physical input then means
	// the user grabbed the mouse or keyboard.
	acting   atomic.Bool
	takeover atomic.Bool
}

// run is one turn's use of the computer.
type run struct {
	key     <-chan struct{} // the turn context's Done channel
	started time.Time
	last    time.Time
	actions int
	view    *view
	thumbs  map[int][]uint8 // last screenshot per monitor, for change detection
	done    chan struct{}
}

// headless skips the on-screen indicator and input hooks (tests).
var headless bool

var ctl = &controller{subs: map[int]func(State){}, stops: map[int]func(){}}

// Subscribe calls fn with the state now and on every change, until the
// returned function is called. fn must not block.
func Subscribe(fn func(State)) (cancel func()) {
	ctl.mu.Lock()
	id := ctl.nextID
	ctl.nextID++
	ctl.subs[id] = fn
	st := ctl.st
	ctl.mu.Unlock()
	fn(st)
	return func() {
		ctl.mu.Lock()
		delete(ctl.subs, id)
		ctl.mu.Unlock()
	}
}

// OnEmergencyStop registers fn to run when the user stops computer control
// (Esc on Windows, or Stop()): front ends cancel the running turn there.
func OnEmergencyStop(fn func()) (cancel func()) {
	ctl.mu.Lock()
	id := ctl.nextID
	ctl.nextID++
	ctl.stops[id] = fn
	ctl.mu.Unlock()
	return func() {
		ctl.mu.Lock()
		delete(ctl.stops, id)
		ctl.mu.Unlock()
	}
}

// Current returns the state now.
func Current() State {
	ctl.mu.Lock()
	defer ctl.mu.Unlock()
	return ctl.st
}

// Pause stops sending input until Resume. A running action is interrupted.
func Pause(reason string) {
	if reason == "" {
		reason = "paused by you"
	}
	ctl.set(func(s *State) bool {
		if !s.Active || s.Paused || s.Stopped {
			return false
		}
		s.Paused, s.Reason = true, reason
		return true
	})
}

// Resume lets a paused run continue.
func Resume() {
	ctl.set(func(s *State) bool {
		if !s.Paused {
			return false
		}
		s.Paused, s.Reason = false, ""
		return true
	})
}

// Stop is the emergency stop: input stops at once, the rest of the turn's
// actions are refused, and OnEmergencyStop handlers cancel the turn.
func Stop(reason string) {
	if reason == "" {
		reason = "stopped by you"
	}
	changed := ctl.set(func(s *State) bool {
		if !s.Active || s.Stopped {
			return false
		}
		s.Stopped, s.Paused, s.Reason = true, false, reason
		return true
	})
	if !changed {
		return
	}
	releaseAll()
	ctl.mu.Lock()
	var fns []func()
	for _, f := range ctl.stops {
		fns = append(fns, f)
	}
	ctl.mu.Unlock()
	for _, f := range fns {
		go f()
	}
}

// set changes the state under the lock and notifies subscribers when fn
// reports a change.
func (c *controller) set(fn func(s *State) bool) bool {
	c.mu.Lock()
	if !fn(&c.st) {
		c.mu.Unlock()
		return false
	}
	st := c.st
	subs := make([]func(State), 0, len(c.subs))
	for _, f := range c.subs {
		subs = append(subs, f)
	}
	c.mu.Unlock()
	if !headless {
		indicate(st)
	}
	for _, f := range subs {
		f(st)
	}
	return true
}

// begin returns the run for ctx's turn, starting one if needed.
func (c *controller) begin(ctx context.Context) *run {
	c.mu.Lock()
	key := ctx.Done()
	if c.run != nil && c.run.key == key && key != nil {
		r := c.run
		c.mu.Unlock()
		return r
	}
	if c.run != nil && key == nil { // no turn context: share one run until it idles out
		r := c.run
		c.mu.Unlock()
		return r
	}
	old := c.run
	r := &run{key: key, started: time.Now(), last: time.Now(), thumbs: map[int][]uint8{}, done: make(chan struct{})}
	c.run = r
	c.mu.Unlock()
	if old != nil {
		close(old.done)
	}
	c.set(func(s *State) bool {
		*s = State{Active: true, StopKey: stopKey()}
		return true
	})
	if !headless {
		watchInput(true)
	}
	go c.watch(ctx, r)
	return r
}

// watch ends the run when its turn ends or it goes idle.
func (c *controller) watch(ctx context.Context, r *run) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-ctx.Done():
			c.end(r)
			return
		case <-t.C:
			c.mu.Lock()
			idle := time.Since(r.last) > idleEnd
			c.mu.Unlock()
			if idle {
				c.end(r)
				return
			}
		}
	}
}

func (c *controller) end(r *run) {
	c.mu.Lock()
	if c.run != r {
		c.mu.Unlock()
		return
	}
	c.run = nil
	c.mu.Unlock()
	releaseAll()
	if !headless {
		watchInput(false)
	}
	c.set(func(s *State) bool {
		*s = State{}
		return true
	})
}

// check refuses an action when control is paused, stopped or over its limits.
func (c *controller) check(r *run, maxActions int, session time.Duration, counts bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case c.st.Stopped:
		return errStopped
	case c.st.Paused:
		return fmt.Errorf(errPaused, c.st.Reason)
	case counts && r.actions >= maxActions:
		return fmt.Errorf("reached the limit of %d mouse/keyboard actions for one turn (computer.max_actions in config). Stop and tell the user what is left to do", maxActions)
	case time.Since(r.started) > session:
		return fmt.Errorf("the computer-use session ran past its %v limit (computer.session_minutes in config). Stop and tell the user what is left to do", session.Round(time.Minute))
	}
	return nil
}

// note records an action for the state line.
func (c *controller) note(r *run, action string, counts bool) {
	c.mu.Lock()
	r.last = time.Now()
	if counts {
		r.actions++
	}
	n := r.actions
	c.mu.Unlock()
	c.set(func(s *State) bool {
		s.Action, s.Actions = action, n
		return true
	})
}

// interrupted reports why a running action must stop now, if it must.
func (c *controller) interrupted(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if c.takeover.Load() {
		c.takeover.Store(false)
		Pause("you took over the mouse or keyboard")
		return fmt.Errorf(errPaused, "the user took over the mouse or keyboard mid-action")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.st.Stopped {
		return errStopped
	}
	if c.st.Paused {
		return fmt.Errorf(errPaused, c.st.Reason)
	}
	return nil
}

// physical is called by platform input watchers for input the user made
// with their own hands (not input ASG injected).
func physical(kind string) {
	switch kind {
	case "stop":
		go Stop("you pressed Esc")
	default:
		lastPhysical.Store(time.Now().UnixNano())
		if ctl.acting.Load() {
			ctl.takeover.Store(true)
		}
	}
}

// lastPhysical is when the user last touched the mouse or keyboard
// themselves (UnixNano), where the platform can tell.
var lastPhysical atomic.Int64

// ResumeAfterAnswer resumes a paused run once the user has answered a
// question — the model was told to ask before continuing.
func ResumeAfterAnswer() {
	if Current().Paused {
		Resume()
	}
}
