package main

import "github.com/wailsapp/wails/v3/pkg/application"

const (
	shortcutStart = "start"
	shortcutPause = "pause"
	shortcutStop  = "stop"
)

func emitShortcut(app *application.App, action string) {
	if app != nil {
		app.Event.Emit("shortcut", action)
	}
}
