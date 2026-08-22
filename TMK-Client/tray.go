package main

import (
	"runtime"

	"tmk-client/internal/client/window"
	"tmk-client/internal/platform/shortcut"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/icons"
)

func setupSystemTray(app *application.App, mainWindow application.Window, windowSvc *window.WindowService) {
	if app == nil || mainWindow == nil {
		return
	}
	tray := app.SystemTray.New()
	tray.SetTooltip("TMK 同声传译")
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(icons.SystrayMacTemplate)
	} else {
		tray.SetIcon(trayIcon)
	}

	menu := app.Menu.New()
	menu.Add("显示主窗口").OnClick(func(ctx *application.Context) { mainWindow.Show().Focus() })
	menu.Add("隐藏到托盘").OnClick(func(ctx *application.Context) { mainWindow.Hide() })
	menu.AddSeparator()
	menu.Add("显示悬挂字幕").OnClick(func(ctx *application.Context) { windowSvc.SetSubtitleVisible(true) })
	menu.Add("隐藏悬挂字幕").OnClick(func(ctx *application.Context) { windowSvc.SetSubtitleVisible(false) })
	menu.AddSeparator()
	menu.Add("开始翻译").OnClick(func(ctx *application.Context) {
		shortcut.Emit(func(action string) { app.Event.Emit("shortcut", action) }, shortcut.Start)
	})
	menu.Add("暂停翻译").OnClick(func(ctx *application.Context) {
		shortcut.Emit(func(action string) { app.Event.Emit("shortcut", action) }, shortcut.Pause)
	})
	menu.Add("停止翻译").OnClick(func(ctx *application.Context) {
		shortcut.Emit(func(action string) { app.Event.Emit("shortcut", action) }, shortcut.Stop)
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(ctx *application.Context) { app.Quit() })

	tray.AttachWindow(mainWindow).WindowOffset(5).SetMenu(menu)
	tray.OnClick(func() {
		if mainWindow.IsVisible() {
			mainWindow.Hide()
			return
		}
		mainWindow.Show().Focus()
	})
	tray.OnRightClick(func() { tray.OpenMenu() })
}
