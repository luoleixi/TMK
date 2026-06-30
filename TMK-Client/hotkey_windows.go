//go:build windows

package main

import (
	"log"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

const (
	hotkeyStartID = 0x544d4b01
	hotkeyPauseID = 0x544d4b02
	hotkeyStopID  = 0x544d4b03

	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modNoRepeat = 0x4000
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
	hotkeyMu             sync.Mutex
	hotkeyHWND           uintptr
)

func registerGlobalHotkeys(hwnd uintptr) {
	hotkeyMu.Lock()
	defer hotkeyMu.Unlock()
	if hotkeyHWND != 0 || hwnd == 0 {
		return
	}
	hotkeyHWND = hwnd
	registerHotkey(hwnd, hotkeyStartID, modControl|modShift|modNoRepeat, 'S')
	registerHotkey(hwnd, hotkeyPauseID, modControl|modShift|modNoRepeat, 'P')
	registerHotkey(hwnd, hotkeyStopID, modControl|modShift|modNoRepeat, 'X')
}

func unregisterGlobalHotkeys() {
	hotkeyMu.Lock()
	defer hotkeyMu.Unlock()
	if hotkeyHWND == 0 {
		return
	}
	unregisterHotkey(hotkeyHWND, hotkeyStartID)
	unregisterHotkey(hotkeyHWND, hotkeyPauseID)
	unregisterHotkey(hotkeyHWND, hotkeyStopID)
	hotkeyHWND = 0
}

func registerHotkey(hwnd uintptr, id int, modifiers, key uintptr) {
	ret, _, err := procRegisterHotKey.Call(hwnd, uintptr(id), modifiers, key)
	if ret == 0 {
		log.Printf("[hotkey] register id=%d failed: %v", id, err)
	}
}

func unregisterHotkey(hwnd uintptr, id int) {
	procUnregisterHotKey.Call(hwnd, uintptr(id))
}

func handleGlobalHotkey(wParam uintptr) (string, bool) {
	switch int(wParam) {
	case hotkeyStartID:
		return shortcutStart, true
	case hotkeyPauseID:
		return shortcutPause, true
	case hotkeyStopID:
		return shortcutStop, true
	default:
		return "", false
	}
}

func isHotkeyMessage(msg uint32) bool {
	return msg == w32.WM_HOTKEY
}
