package main

import (
	_ "embed"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed build/appicon.png
var trayIcon []byte

func installTray(app *application.App, window *application.WebviewWindow) {
	var quitting atomic.Bool

	tray := app.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip("Trojan-Go Desktop")

	menu := app.NewMenu()
	menu.Add("显示主界面").OnClick(func(*application.Context) {
		showMainWindow(window)
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) {
		quitting.Store(true)
		app.Quit()
	})
	tray.SetMenu(menu)
	tray.OnClick(func() {
		showMainWindow(window)
	})

	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if quitting.Load() {
			return
		}
		window.Hide()
		event.Cancel()
	})
}

func showMainWindow(window *application.WebviewWindow) {
	if window == nil {
		return
	}
	if window.IsMinimised() {
		window.UnMinimise()
	}
	window.Show().Focus()
}
