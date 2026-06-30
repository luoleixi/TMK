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
	if subtitleWindow != nil {
		subtitleWindow.SetAlwaysOnTop(true).Show()
	}
}

func (s *WindowService) HideSubtitle() {
	if subtitleWindow != nil {
		subtitleWindow.Hide()
	}
}

func (s *WindowService) ToggleSubtitle() bool {
	if subtitleWindow == nil {
		return false
	}
	if subtitleWindow.IsVisible() {
		subtitleWindow.Hide()
		return false
	}
	subtitleWindow.SetAlwaysOnTop(true).Show()
	return true
}
