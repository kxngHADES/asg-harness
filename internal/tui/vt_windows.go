//go:build windows

package tui

import (
	"os"

	"golang.org/x/sys/windows"
)

// Enable ANSI escape processing on classic Windows consoles (cmd.exe,
// older PowerShell hosts). Windows Terminal already has it on.
func init() {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) == nil {
		_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
}
