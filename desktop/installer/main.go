// Command asg-setup is the ASG setup wizard: a small Wails app that installs
// the ASG desktop app (embedded in payload/ at build time), creates the
// shortcuts, picks the data and memory folders and sets up the first model
// providers. Copied into the install folder as "uninstall", it removes ASG
// again.
//
// Build: desktop/build/package.sh builds the app, packs it into
// payload/app.tar.gz and then builds this wizard around it. Without a payload
// (a plain `go build`) the wizard runs as a preview that changes nothing.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// version is set with -ldflags "-X main.version=v1.2.3".
var version = "dev"

//go:embed all:frontend
var ui embed.FS

func main() {
	uninstall := flag.Bool("uninstall", false, "remove ASG instead of installing it")
	flag.Parse()

	s := NewSetup(*uninstall || startedAsUninstaller())
	assets, _ := fs.Sub(ui, "frontend")
	err := wails.Run(&options.App{
		Title:            map[bool]string{true: "Uninstall ASG", false: "ASG Setup"}[s.uninstall],
		Width:            1000,
		Height:           680,
		DisableResize:    true,
		Frameless:        runtime.GOOS != "darwin",
		BackgroundColour: &options.RGBA{R: 11, G: 12, B: 10, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        s.startup,
		OnShutdown:       s.shutdown,
		Bind:             []interface{}{s},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "asg-setup-3b9e71a0",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if s.ctx != nil {
					wruntime.WindowUnminimise(s.ctx)
					wruntime.WindowShow(s.ctx)
				}
			},
		},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.NSAppearanceNameDarkAqua,
			About:      &mac.AboutInfo{Title: "ASG Setup", Message: "Installs the ASG desktop app."},
		},
		Linux: &linux.Options{ProgramName: "asg-setup"},
		Windows: &windows.Options{
			Theme:                windows.Dark,
			DisablePinchZoom:     true,
			IsZoomControlEnabled: false,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "asg-setup:", err)
		os.Exit(1)
	}
}

// startedAsUninstaller reports whether this is the copy the installer left
// in the install folder (uninstall.exe / uninstall).
func startedAsUninstaller() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(filepath.Base(exe)), "uninstall")
}
