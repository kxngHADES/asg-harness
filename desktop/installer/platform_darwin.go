package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// /Applications when this user may write there (admins can), otherwise
// ~/Applications.
func defaultInstallDir() string {
	if writable("/Applications") == nil {
		return "/Applications"
	}
	return filepath.Join(defaultHome(), "Applications")
}

func appPath(dir string) string { return filepath.Join(dir, "ASG.app") }

func features() Features {
	return Features{DesktopShortcut: true, LaunchAtLogin: true}
}

// postInstall clears the download quarantine so Gatekeeper doesn't block
// the first start (the app is ad-hoc signed, without a Developer ID).
func postInstall(app string) {
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", app).Run()
}

func createShortcuts(app string, o Options) ([]string, error) {
	if !o.DesktopShortcut {
		return nil, nil
	}
	link := filepath.Join(defaultHome(), "Desktop", "ASG")
	_ = os.Remove(link)
	if err := os.Symlink(app, link); err != nil {
		return nil, fmt.Errorf("desktop shortcut: %w", err)
	}
	return []string{link}, nil
}

const agentLabel = "com.kxnghades.asg.desktop"

func setAutostart(app string) (string, error) {
	p := filepath.Join(defaultHome(), "Library", "LaunchAgents", agentLabel+".plist")
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>/usr/bin/open</string><string>-a</string><string>%s</string></array>
  <key>RunAtLoad</key><true/>
</dict>
</plist>
`, agentLabel, xmlEscape(app))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, []byte(plist), 0o644)
}

func register(*Record, int64) error { return nil }

func launch(app string) error { return exec.Command("open", "-a", app).Start() }
