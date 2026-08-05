package shortcut

import "github.com/wailsapp/wails/v3/pkg/application"

const (
	Start = "start"
	Pause = "pause"
	Stop  = "stop"
)

func Emit(app *application.App, action string) {
	if app != nil {
		app.Event.Emit("shortcut", action)
	}
}
