// Package proc holds small helpers for starting child processes.
package proc

import "os/exec"

// Hide keeps a child process from opening a console window on Windows when
// the parent is a GUI app (the desktop app). In a terminal the child shares
// the parent's console, so nothing changes there. It is a no-op elsewhere.
func Hide(cmd *exec.Cmd) *exec.Cmd {
	hide(cmd)
	return cmd
}
