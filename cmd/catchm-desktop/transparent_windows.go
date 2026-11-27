//go:build windows

package main

// Windows 11 compact see-through (docs/plans/completed/compact-transparent.md).
//
// How it works: the Wails window already has Windows.WindowIsTranslucent
// (WS_EX_NOREDIRECTIONBITMAP + DWMWA_SYSTEMBACKDROP_TYPE=Mica, set by
// Wails' frontend on build >= 22621). Our main.go sets
// Windows.WebviewIsTransparent, so the webview composites alpha-0
// pixels instead of an opaque background. Switching the backdrop type
// to DWMSBT_NONE (no system backdrop) therefore shows the desktop
// through those pixels; switching back to DWMSBT_MAINWINDOW restores
// the Mica normal mode. All via x/sys/windows (no cgo):
// DwmSetWindowAttribute, and EnumWindows to find this process's
// top-level "wailsWindow"-class window (Wails v2 exposes no HWND
// getter to Go callers).
//
// UNVERIFIED on real Windows 11 hardware — accepted by the user
// (decision, 2026-10-05); see the plan. Build < 22621 reports
// transparentSupported() false (the button is hidden).

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DWMWA_SYSTEMBACKDROP_TYPE and its values (dwmapi.h) — Wails' own
// frontend uses the same pair for the Mica normal mode.
const (
	dwmwaSystemBackdropType = 38
	dwmsbtNone              = 1
	dwmsbtMainWindow        = 2
)

var (
	dwmapi                    = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

// mainHWND finds this process's top-level window of Wails' registered
// "wailsWindow" class (checked against Wails v2.16
// internal/frontend/desktop/windows/window.go). The EnumWindows callback
// is created once: Go never frees syscall.NewCallback callbacks and caps
// them (~2000 per process), so one per toggle would eventually panic.
func mainHWND() (windows.HWND, bool) {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumFound = 0
	_ = windows.EnumWindows(enumCallback, nil)
	return enumFound, enumFound != 0
}

var (
	enumMu       sync.Mutex
	enumFound    windows.HWND // written by enumCallback under enumMu
	enumCallback = syscall.NewCallback(func(hwnd windows.HWND, _ unsafe.Pointer) uintptr {
		var pid uint32
		windows.GetWindowThreadProcessId(hwnd, &pid)
		if pid != windows.GetCurrentProcessId() {
			return 1 // keep enumerating
		}
		var buf [64]uint16
		if n, err := windows.GetClassName(hwnd, &buf[0], int32(len(buf))); err == nil && windows.UTF16ToString(buf[:n]) == "wailsWindow" {
			enumFound = hwnd
			return 0
		}
		return 1
	})
)

// transparentSupported: the system-backdrop API this relies on only
// exists on Windows 11 22H2 (build 22621) and later — the same gate
// Wails' Mica code uses.
func transparentSupported() bool {
	v := windows.RtlGetVersion()
	return v.MajorVersion > 10 || (v.MajorVersion == 10 && v.BuildNumber >= 22621)
}

// setWindowTransparent swaps the DWM system backdrop: DWMSBT_NONE to
// let the desktop show through the webview's alpha-0 pixels,
// DWMSBT_MAINWINDOW (Mica) to restore normal mode.
func setWindowTransparent(on bool) {
	hwnd, ok := mainHWND()
	if !ok {
		return
	}
	backdrop := int32(dwmsbtMainWindow)
	if on {
		backdrop = dwmsbtNone
	}
	_, _, _ = procDwmSetWindowAttribute.Call(
		uintptr(hwnd),
		uintptr(dwmwaSystemBackdropType),
		uintptr(unsafe.Pointer(&backdrop)),
		unsafe.Sizeof(backdrop),
	)
}
