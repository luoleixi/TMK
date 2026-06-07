package main

import (
	"embed"

	"log"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[string]("time")
	application.RegisterEvent[TranscriptMsg]("transcript")
	application.RegisterEvent[TranslationMsg]("translation")
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
	sessionSvc = &SessionService{}
	captureSvc = NewCaptureService()

	app := application.New(application.Options{
		Name:        "TMK-Client",
		Description: "A demo of using raw HTML & CSS",
		Services: []application.Service{
			application.NewService(sessionSvc),
			application.NewService(captureSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// -- main window --
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "TMK",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})

	// -- subtitle window (hidden, always-on-top, frameless) --
	subtitleWin := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:              "subtitle",
		Width:             600,
		Height:            80,
		AlwaysOnTop:       true,
		Frameless:         true,
		DisableResize:     true,
		BackgroundType:    application.BackgroundTypeTransparent,
		Hidden:            true,
		URL:               "/?mode=subtitle",
		IgnoreMouseEvents: true,
	})
	sessionSvc.SetSubtitleWindow(subtitleWin)

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
