package window

type Window interface {
	Show()
	Hide()
	Focus()
	IsVisible() bool
	SetAlwaysOnTop(bool)
}

type EventEmitter func(name string, value any)

type WindowService struct {
	mainWindow     func() Window
	subtitleWindow func() Window
	emit           EventEmitter
}

func NewService(mainWindow, subtitleWindow func() Window, emit EventEmitter) *WindowService {
	return &WindowService{mainWindow: mainWindow, subtitleWindow: subtitleWindow, emit: emit}
}

func (s *WindowService) ShowMain() {
	if window := s.mainWindow(); window != nil {
		window.Show()
		window.Focus()
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
		window.SetAlwaysOnTop(true)
		window.Show()
	} else {
		window.Hide()
	}
	if s.emit != nil {
		s.emit("subtitle-visibility-changed", visible)
	}
	return visible
}
