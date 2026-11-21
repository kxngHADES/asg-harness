package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/kxngHADES/asg-harness/internal/proc"
)

const (
	uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\ASGDesktop`
	runKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue     = "ASG"
)

// Per-user install: no administrator rights needed.
func defaultInstallDir() string {
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, "Programs", "ASG")
	}
	return filepath.Join(defaultHome(), "AppData", "Local", "Programs", "ASG")
}

func appPath(dir string) string { return filepath.Join(dir, "asg-desktop.exe") }

func features() Features {
	return Features{DesktopShortcut: true, MenuShortcut: true, MenuLabel: "Start menu", LaunchAtLogin: true}
}

// installUninstaller copies this wizard into the install folder as
// uninstall.exe, which "Installed apps" in Settings runs.
func installUninstaller(dir string) (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "uninstall.exe")
	if samePath(self, dst) {
		return dst, nil
	}
	in, err := os.Open(self)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp := dst + ".asg-new"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return "", inUse(dst, err)
	}
	return dst, nil
}

func postInstall(string) {}

func createShortcuts(app string, o Options) ([]string, error) {
	var made []string
	add := func(folder *windows.KNOWNFOLDERID) error {
		dir, err := windows.KnownFolderPath(folder, windows.KF_FLAG_CREATE)
		if err != nil {
			return err
		}
		lnk := filepath.Join(dir, "ASG.lnk")
		if err := makeShortcut(lnk, app); err != nil {
			return fmt.Errorf("shortcut %s: %w", lnk, err)
		}
		made = append(made, lnk)
		return nil
	}
	if o.DesktopShortcut {
		if err := add(windows.FOLDERID_Desktop); err != nil {
			return made, err
		}
	}
	if o.MenuShortcut {
		if err := add(windows.FOLDERID_Programs); err != nil {
			return made, err
		}
	}
	return made, nil
}

// makeShortcut writes a .lnk through the WScript.Shell COM object.
func makeShortcut(lnk, target string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_FALSE / RPC_E_CHANGED_MODE just mean COM is already up on this thread.
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err == nil {
		defer ole.CoUninitialize()
	}
	unk, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return err
	}
	defer unk.Release()
	shell, err := unk.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer shell.Release()
	v, err := oleutil.CallMethod(shell, "CreateShortcut", lnk)
	if err != nil {
		return err
	}
	sc := v.ToIDispatch()
	defer sc.Release()
	for k, val := range map[string]string{
		"TargetPath":       target,
		"WorkingDirectory": filepath.Dir(target),
		"IconLocation":     target + ",0",
		"Description":      "ASG — the agent harness",
	} {
		if _, err := oleutil.PutProperty(sc, k, val); err != nil {
			return err
		}
	}
	_, err = oleutil.CallMethod(sc, "Save")
	return err
}

func setAutostart(app string) (string, error) {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	return `HKCU\` + runKey + `\` + runValue, k.SetStringValue(runValue, `"`+app+`"`)
}

func removeAutostart(string) {
	if k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(runValue)
		k.Close()
	}
}

// register adds ASG to Settings → Apps → Installed apps.
func register(r *Record, size int64) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	un := `"` + filepath.Join(r.InstallDir, "uninstall.exe") + `" --uninstall`
	num := r.Version
	if len(num) > 0 && num[0] == 'v' {
		num = num[1:]
	}
	for name, v := range map[string]string{
		"DisplayName":          "ASG",
		"DisplayVersion":       num,
		"Publisher":            "ASG",
		"DisplayIcon":          r.AppPath + ",0",
		"InstallLocation":      r.InstallDir,
		"UninstallString":      un,
		"QuietUninstallString": un,
		"URLInfoAbout":         "https://github.com/kxngHADES/asg-harness",
		"InstallDate":          r.Installed.Format("20060102"),
	} {
		if err := k.SetStringValue(name, v); err != nil {
			return err
		}
	}
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	_ = k.SetDWordValue("EstimatedSize", uint32(size/1024))
	return nil
}

func unregister() { _ = registry.DeleteKey(registry.CURRENT_USER, uninstallKey) }

func launch(app string) error {
	cmd := exec.Command(app)
	cmd.Dir = filepath.Dir(app)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// scheduleSelfDelete removes the uninstaller (and the then-empty install
// folder) a moment after this process exits; a running exe can't delete itself.
func scheduleSelfDelete(dir string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = "cmd.exe"
	}
	cmd := proc.Hide(exec.Command(comspec))
	// A raw command line: Go's argument quoting would garble cmd's.
	cmd.SysProcAttr.CmdLine = fmt.Sprintf(`cmd.exe /c ping 127.0.0.1 -n 3 > nul & del /f /q "%s" & rmdir "%s"`, self, dir)
	cmd.SysProcAttr.CreationFlags = windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS
	cmd.Dir = os.TempDir()
	_ = cmd.Start()
}
