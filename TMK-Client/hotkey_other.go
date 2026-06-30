//go:build !windows

package main

func registerGlobalHotkeys(hwnd uintptr) {}

func unregisterGlobalHotkeys() {}

func handleGlobalHotkey(wParam uintptr) (string, bool) {
	return "", false
}

func isHotkeyMessage(msg uint32) bool {
	return false
}
