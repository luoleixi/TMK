package main

import (
	"embed"

	"log"
	"time"

	"changeme/internal/client/capture"
	clientexport "changeme/internal/client/export"
	"changeme/internal/client/session"
	"changeme/internal/client/settings"
	"changeme/internal/client/window"
	"changeme/internal/platform/hotkey"
	"changeme/internal/platform/shortcut"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

//go:embed frontend/public/wails.png
var trayIcon []byte

func init() {
	application.RegisterEvent[string]("time")
	application.RegisterEvent[session.TranscriptMsg]("transcript")
	application.RegisterEvent[session.TranslationMsg]("translation")
	application.RegisterEvent[bool]("stream-reset")
	application.RegisterEvent[string]("shortcut")
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {

	// Create a new Wails application by providing the necessary options.
	// Variables 'Name' and 'Description' are for application metadata.
	// 'Assets' configures the asset server with the 'FS' variable pointing to the frontend files.
	// 'Bind' is a list of Go struct instances. The frontend has access to the methods of these instances.
	// 'Mac' options tailor the application when running an macOS.
	sessionSvc := session.NewService()
	captureSvc := capture.NewService(sessionSvc.SendAudio)
	settingsSvc := settings.NewService()
	exportSvc := clientexport.NewService()
	var mainWindow application.Window
	var subtitleWindow application.Window
	windowSvc := window.NewService(
		func() application.Window { return mainWindow },
		func() application.Window { return subtitleWindow },
	)

	var app *application.App
	app = application.New(application.Options{
		Name:        "TMK-Client",
		Description: "A demo of using raw HTML & CSS",
		Services: []application.Service{
			application.NewService(sessionSvc),
			application.NewService(captureSvc),
			application.NewService(settingsSvc),
			application.NewService(exportSvc),
			application.NewService(windowSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		KeyBindings: map[string]func(window application.Window){
			"CmdOrCtrl+Shift+S": func(window application.Window) { shortcut.Emit(app, shortcut.Start) },
			"CmdOrCtrl+Shift+P": func(window application.Window) { shortcut.Emit(app, shortcut.Pause) },
			"CmdOrCtrl+Shift+X": func(window application.Window) { shortcut.Emit(app, shortcut.Stop) },
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
			WndProcInterceptor: func(hwnd uintptr, msg uint32, wParam, lParam uintptr) (uintptr, bool) {
				hotkey.Register(hwnd)
				if hotkey.IsMessage(msg) {
					if action, ok := hotkey.Handle(wParam); ok {
						shortcut.Emit(app, action)
						return 0, true
					}
				}
				return 0, false
			},
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
		},
	})
	defer hotkey.Unregister()

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:  "main",
		Title: "TMK 同声传译",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})
	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		mainWindow.Hide()
		e.Cancel()
	})
	subtitleWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "subtitle",
		Title:            "TMK 悬挂字幕",
		Width:            900,
		Height:           180,
		AlwaysOnTop:      true,
		Frameless:        true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/?window=subtitle",
		Hidden:           true,
		Windows: application.WindowsWindow{
			HiddenOnTaskbar: true,
		},
	})
	subtitleWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		e.Cancel()
		windowSvc.SetSubtitleVisible(false)
	})
	setupSystemTray(app, mainWindow, windowSvc)

	// Create a goroutine that emits an event containing the current time every second.
	// The frontend can listen to this event and update the UI accordingly.
	go func() {
		for {
			now := time.Now().Format(time.RFC1123)
			app.Event.Emit("time", now)
			time.Sleep(time.Second)
		}
	}()

	// Run the application. This blocks until the application has been exited.
	err := app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}
