//go:build !windows

package main

import (
	"syscall"
	"time"
)

// waitForExit blocks until process pid has exited or timeout passes.
func waitForExit(pid int, timeout time.Duration) {
	end := time.Now().Add(timeout)
	for time.Now().Before(end) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
