//go:build !windows

package computer

import (
	"math"
	"sync"
	"time"
)

// Without low-level input hooks, takeover is detected by polling the
// pointer: if it moves away from where ASG last put it, the user moved it.
// There is no global stop key; the app's Stop button and ctrl+c stop control.

func stopKey() string { return "" }

func indicate(State) {}

var poll struct {
	mu      sync.Mutex
	stop    chan struct{}
	lastSet [2]int
	set     bool
}

// noteMove records where ASG put the pointer.
func noteMove(x, y int) {
	poll.mu.Lock()
	poll.lastSet, poll.set = [2]int{x, y}, true
	poll.mu.Unlock()
}

func watchInput(on bool) {
	poll.mu.Lock()
	defer poll.mu.Unlock()
	if on && poll.stop == nil && sys != nil {
		poll.stop = make(chan struct{})
		poll.set = false
		go pollLoop(poll.stop)
	}
	if !on && poll.stop != nil {
		close(poll.stop)
		poll.stop = nil
	}
}

func pollLoop(stop chan struct{}) {
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		x, y, err := sys.Cursor()
		if err != nil {
			continue
		}
		poll.mu.Lock()
		last, set := poll.lastSet, poll.set
		if !set {
			poll.lastSet, poll.set = [2]int{x, y}, true
		}
		poll.mu.Unlock()
		if set && math.Hypot(float64(x-last[0]), float64(y-last[1])) > 12 {
			noteMove(x, y)
			physical("mouse")
		}
	}
}
