package window

import "github.com/wailsapp/wails/v3/pkg/application"

type WindowService struct {
	mainWindow     func() application.Window
	subtitleWindow func() application.Window
}

func NewService(mainWindow, subtitleWindow func() application.Window) *WindowService {
	return &WindowService{mainWindow: mainWindow, subtitleWindow: subtitleWindow}
}

func (s *WindowService) ShowMain() {
	if window := s.mainWindow(); window != nil {
		window.Show().Focus()
	}
}

func (s *WindowService) HideMain() {
	if window := s.mainWindow(); window != nil {
		window.Hide()
	}
}

func (s *WindowService) ShowSubtitle() { s.SetSubtitleVisible(true) }

func (s *WindowService) HideSubtitle() { s.SetSubtitleVisible(false) }

func (s *WindowService) ToggleSubtitle() bool {
	window := s.subtitleWindow()
	if window == nil {
		return false
	}
	return s.SetSubtitleVisible(!window.IsVisible())
}

func (s *WindowService) SetSubtitleVisible(visible bool) bool {
	window := s.subtitleWindow()
	if window == nil {
		return false
	}
	if visible {
		window.SetAlwaysOnTop(true).Show()
	} else {
		window.Hide()
	}
	application.Get().Event.Emit("subtitle-visibility-changed", visible)
	return visible
}
