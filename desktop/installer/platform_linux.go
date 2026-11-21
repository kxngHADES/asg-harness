package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(defaultHome(), ".local", "share")
}

func configHome() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(defaultHome(), ".config")
}

func defaultInstallDir() string { return filepath.Join(dataHome(), "asg-desktop") }

func appPath(dir string) string { return filepath.Join(dir, "asg-desktop") }

func features() Features {
	return Features{DesktopShortcut: true, MenuShortcut: true, MenuLabel: "app menu", LaunchAtLogin: true, AddToPath: true}
}

func postInstall(string) {}

func desktopEntry(app string) string {
	icon := filepath.Join(filepath.Dir(app), "asg-desktop.png")
	return fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=ASG
Comment=ASG agent harness
Exec=%s
Icon=%s
Terminal=false
Categories=Development;
StartupWMClass=asg-desktop
`, desktopQuote(app), icon)
}

// desktopQuote quotes an Exec path that has spaces (Desktop Entry spec).
func desktopQuote(p string) string {
	if !strings.ContainsAny(p, " \t\"\\$`") {
		return p
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`")
	return `"` + r.Replace(p) + `"`
}

// desktopDir asks xdg-user-dir for the (possibly translated) Desktop folder.
func desktopDir() string {
	if out, err := exec.Command("xdg-user-dir", "DESKTOP").Output(); err == nil {
		if d := strings.TrimSpace(string(out)); filepath.IsAbs(d) && d != defaultHome() {
			return d
		}
	}
	return filepath.Join(defaultHome(), "Desktop")
}

func createShortcuts(app string, o Options) ([]string, error) {
	var made []string
	entry := desktopEntry(app)
	if o.MenuShortcut {
		p := filepath.Join(dataHome(), "applications", "asg-desktop.desktop")
		if err := writeFile(p, entry, 0o644); err != nil {
			return made, err
		}
		made = append(made, p)
		_ = exec.Command("update-desktop-database", filepath.Dir(p)).Run()
	}
	if o.DesktopShortcut {
		p := filepath.Join(desktopDir(), "asg-desktop.desktop")
		if err := writeFile(p, entry, 0o755); err != nil {
			return made, err
		}
		made = append(made, p)
		// GNOME only starts desktop launchers marked as trusted.
		_ = exec.Command("gio", "set", p, "metadata::trusted", "true").Run()
	}
	if o.AddToPath {
		link := filepath.Join(defaultHome(), ".local", "bin", "asg-desktop")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return made, err
		}
		_ = os.Remove(link)
		if err := os.Symlink(app, link); err != nil {
			return made, err
		}
		made = append(made, link)
	}
	return made, nil
}

func writeFile(p, content string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		return err
	}
	return os.Chmod(p, mode)
}

func setAutostart(app string) (string, error) {
	p := filepath.Join(configHome(), "autostart", "asg-desktop.desktop")
	return p, writeFile(p, desktopEntry(app), 0o644)
}

func register(*Record, int64) error { return nil }

func launch(app string) error {
	cmd := exec.Command(app)
	cmd.Dir = filepath.Dir(app)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
