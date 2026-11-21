//go:build !devserve

// Command asg-desktop is the ASG desktop app: sessions, projects, coding
// sessions, artifacts and schedules around the same agent the terminal UI
// runs. It is a Wails v2 app; the UI lives in frontend/dist (plain HTML, CSS
// and JS, no build step).
//
// Build: see docs/desktop.md (make desktop).
package main

import (
	"fmt"
	"io/fs"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/kxngHADES/asg-harness/internal/agent"
	"github.com/kxngHADES/asg-harness/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "asg-desktop:", err)
		os.Exit(1)
	}
	app := NewApp(agent.NewApp(cfg))
	dist, _ := fs.Sub(assets, "frontend/dist")

	err = wails.Run(&options.App{
		Title:     "ASG",
		Width:     1320,
		Height:    860,
		MinWidth:  720,
		MinHeight: 520,
		// macOS keeps its native traffic-light buttons over a full-height
		// content view; Windows and Linux draw their own controls.
		Frameless:        runtime.GOOS != "darwin",
		BackgroundColour: &options.RGBA{R: 21, G: 21, B: 19, A: 255},
		AssetServer:      &assetserver.Options{Assets: dist},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "asg-desktop-6f1d2c4e",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if app.ctx != nil {
					wruntime.WindowUnminimise(app.ctx)
					wruntime.WindowShow(app.ctx)
				}
			},
		},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarHiddenInset(),
			Appearance: mac.DefaultAppearance,
			About:      &mac.AboutInfo{Title: "ASG", Message: "The ASG agent harness, as a desktop app."},
		},
		Linux: &linux.Options{ProgramName: "asg-desktop"},
		Windows: &windows.Options{
			Theme:                windows.SystemDefault,
			DisablePinchZoom:     true,
			IsZoomControlEnabled: false,
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "asg-desktop:", err)
		os.Exit(1)
	}
}
