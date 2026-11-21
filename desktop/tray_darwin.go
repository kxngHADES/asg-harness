package main

import wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

// macOS: no tray icon. Wails runs the Cocoa event loop itself and a second
// NSStatusItem owner would fight it; the Dock icon reopens the window.
type trayItems struct{}

func (a *App) startTray()            {}
func (a *App) stopTray()             {}
func (a *App) trayUpdate(UpdateInfo) {}
func (a *App) showWindow() {
	if a.ctx != nil {
		wruntime.WindowShow(a.ctx)
		wruntime.WindowUnminimise(a.ctx)
	}
}
