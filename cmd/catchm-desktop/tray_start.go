//go:build !darwin

package main

import (
	"context"
	"net/http"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/spacingmind/catchm/internal/config"
)

// goosDarwin: see tray_start_darwin.go.
const goosDarwin = false

// startTray runs the tray icon (Windows/Linux). macOS has no tray: a
// tray library can't coexist with Wails v2 there (see
// tray_start_darwin.go), so this only builds off darwin.
func startTray(ctx context.Context, handler http.Handler, cfg *config.Config, hk *hotkeyManager, onExit func()) {
	go systray.Run(
		func() { onTrayReady(ctx, handler, cfg, hk) },
		onExit,
	)
}

// hideWindowOnClose: see tray_start_darwin.go. Off darwin the close
// button goes through OnBeforeClose, which onCloseRequest handles.
const hideWindowOnClose = false

// onCloseRequest runs from OnBeforeClose for the close button: hide
// the window instead (the tray icon keeps it reachable, recording
// keeps running). The tray's Quit sets reallyQuit first, so it never
// gets here.
func onCloseRequest(ctx context.Context) (prevent bool) {
	windowVisible.Store(false)
	runtime.WindowHide(ctx)
	return true
}

func showMainWindow(ctx context.Context) {
	windowVisible.Store(true)
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
}

func toggleMainWindow(ctx context.Context) {
	if windowVisible.Load() {
		windowVisible.Store(false)
		runtime.WindowHide(ctx)
		return
	}
	showMainWindow(ctx)
}

// appMenu is nil off darwin: Wails' own default menu already covers
// Quit, and OnBeforeClose only fights it on macOS.
func appMenu(quit func()) *menu.Menu {
	return nil
}
