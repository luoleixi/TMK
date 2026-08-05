//go:build !windows

package hotkey

func Register(hwnd uintptr) {}

func Unregister() {}

func Handle(wParam uintptr) (string, bool) {
	return "", false
}

func IsMessage(msg uint32) bool {
	return false
}
