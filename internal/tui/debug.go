package tui

import (
	"os"

	"charm.land/log/v2"
)

// Bubble Tea owns stdout while the TUI runs, so diagnostics can't be
// printed. Set ASG_DEBUG=<file> to have them written there with Charm's
// logger (tail -f it in another terminal).
var debugLog = func() *log.Logger {
	path := os.Getenv("ASG_DEBUG")
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	return log.NewWithOptions(f, log.Options{
		Prefix:          "asg tui",
		ReportTimestamp: true,
		Level:           log.DebugLevel,
	})
}()

func debugf(format string, args ...any) {
	if debugLog != nil {
		debugLog.Debugf(format, args...)
	}
}
