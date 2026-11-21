//go:build !windows

package main

import (
	"encoding/xml"
	"os"
	"strings"
)

// Only Windows keeps an uninstaller in the install folder; on macOS and
// Linux running the setup again offers Uninstall.
func installUninstaller(string) (string, error) { return "", nil }

func unregister() {}

func scheduleSelfDelete(string) {}

func removeAutostart(path string) { _ = os.Remove(path) }

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
