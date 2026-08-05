package main

import "github.com/wailsapp/wails/v3/pkg/application"

var (
	mainWindow     application.Window
	subtitleWindow application.Window
)

type WindowService struct{}

func NewWindowService() *WindowService {
	return &WindowService{}
}

func (s *WindowService) ShowMain() {
	if mainWindow != nil {
		mainWindow.Show().Focus()
	}
}

func (s *WindowService) HideMain() {
	if mainWindow != nil {
		mainWindow.Hide()
	}
}

func (s *WindowService) ShowSubtitle() {
	setSubtitleVisible(true)
}

func (s *WindowService) HideSubtitle() {
	setSubtitleVisible(false)
}

func (s *WindowService) ToggleSubtitle() bool {
	if subtitleWindow == nil {
		return false
	}
	if subtitleWindow.IsVisible() {
		return setSubtitleVisible(false)
	}
	return setSubtitleVisible(true)
}

func setSubtitleVisible(visible bool) bool {
	if subtitleWindow == nil {
		return false
	}
	if visible {
		subtitleWindow.SetAlwaysOnTop(true).Show()
	} else {
		subtitleWindow.Hide()
	}
	application.Get().Event.Emit("subtitle-visibility-changed", visible)
	return visible
}
