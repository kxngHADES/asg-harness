package main

import (
	"time"

	"golang.org/x/sys/windows"
)

// waitForExit blocks until process pid has exited or timeout passes.
func waitForExit(pid int, timeout time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return // already gone
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(timeout.Milliseconds()))
}
