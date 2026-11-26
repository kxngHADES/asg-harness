//go:build !windows && !linux && !darwin

package computer

var unsupported = "computer use is not available on this operating system yet (Windows, Linux with X11 and macOS are supported)"
