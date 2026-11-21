//go:build windows || linux

package main

// The tray (notification area) icon. Left-click opens the window; right-click
// shows the menu. On macOS the app has no tray icon (see tray_darwin.go):
// Wails owns the Cocoa run loop there, so the Dock is used instead.

import (
	"runtime"
	"time"

	"github.com/energye/systray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type trayItems struct {
	update *systray.MenuItem
}

func (a *App) startTray() {
	ready := make(chan struct{})
	go func() {
		// Windows ties the tray's hidden window to the thread that created it,
		// so the message loop has to stay on that thread.
		runtime.LockOSThread()
		systray.Run(func() {
			systray.SetIcon(trayIcon(runtime.GOOS))
			systray.SetTitle("ASG")
			systray.SetTooltip("ASG")
			systray.SetOnClick(func(systray.IMenu) { a.showWindow() })
			systray.SetOnDClick(func(systray.IMenu) { a.showWindow() })

			open := systray.AddMenuItem("Open ASG", "Show the ASG window")
			open.Click(a.showWindow)
			newChat := systray.AddMenuItem("New chat", "Start a new chat")
			newChat.Click(func() { a.showWindow(); a.emit("asg:nav", map[string]string{"view": "home"}) })
			schedules := systray.AddMenuItem("Schedules", "Open your scheduled prompts")
			schedules.Click(func() { a.showWindow(); a.emit("asg:nav", map[string]string{"view": "schedules"}) })
			systray.AddSeparator()
			upd := systray.AddMenuItem("Check for updates…", "See whether a newer ASG is out")
			upd.Click(func() {
				a.showWindow()
				a.emit("asg:nav", map[string]string{"view": "settings", "tab": "updates", "check": "1"})
			})
			settings := systray.AddMenuItem("Settings", "Open ASG settings")
			settings.Click(func() { a.showWindow(); a.emit("asg:nav", map[string]string{"view": "settings"}) })
			systray.AddSeparator()
			quit := systray.AddMenuItem("Quit ASG", "Close ASG completely (schedules stop)")
			quit.Click(a.quitApp)

			a.tray = &trayItems{update: upd}
			a.trayOn.Store(true)
			close(ready)
		}, nil)
	}()
	// If the tray can't start (no notification area), carry on without it.
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
	}
}

func (a *App) stopTray() {
	if a.trayOn.Load() {
		systray.Quit()
	}
}

// trayUpdate relabels the update item when a new version is out.
func (a *App) trayUpdate(info UpdateInfo) {
	if a.tray == nil {
		return
	}
	if info.Available && info.CanInstall {
		a.tray.update.SetTitle("Update to " + info.Latest + "…")
	} else {
		a.tray.update.SetTitle("Check for updates…")
	}
}

func (a *App) showWindow() {
	if a.ctx == nil {
		return
	}
	wruntime.WindowShow(a.ctx)
	wruntime.WindowUnminimise(a.ctx)
}
