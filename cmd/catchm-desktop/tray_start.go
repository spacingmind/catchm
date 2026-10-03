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

// hideOnClose hides the window: with the tray icon still present it
// stays reachable from there, and recording keeps running.
func hideOnClose(ctx context.Context) {
	windowVisible.Store(false)
	runtime.WindowHide(ctx)
}

// appMenu is nil off darwin: Wails' own default menu already covers
// Quit, and OnBeforeClose only fights it on macOS.
func appMenu(quit func()) *menu.Menu {
	return nil
}
