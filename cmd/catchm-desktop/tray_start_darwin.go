//go:build darwin

package main

import (
	"context"
	"net/http"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/spacingmind/catchm/internal/config"
)

// goosDarwin lets tray.go no-op applyTrayState without importing Go's
// runtime package (tray.go already names the Wails runtime "runtime").
const goosDarwin = true

// startTray on macOS starts only the state poller: energye/systray
// both runs AppKit's loop off the main thread (SIGTRAP under Wails)
// and replaces Wails' NSApplication delegate (setDelegate: in its
// systray_darwin.m), so no tray library can coexist with Wails v2
// here. pollTrayState still runs because its transition toasts are
// platform no-ops that matter once notifications land (M25);
// applyTrayState never touches tray menu items on darwin.
func startTray(ctx context.Context, handler http.Handler, cfg *config.Config, hk *hotkeyManager, onExit func()) {
	go pollTrayState(ctx, handler, hk)
	_ = cfg
	_ = onExit
}

// hideOnClose minimises to the Dock instead of hiding: a hidden window
// is only recoverable through a tray icon (Windows) or a Dock-reopen
// handler Wails v2 doesn't expose, but a minimised window comes back
// with one Dock click, and recording keeps running meanwhile.
func hideOnClose(ctx context.Context) {
	windowVisible.Store(false)
	runtime.WindowMinimise(ctx)
}

// appMenu is the macOS application menu. Wails' default AppMenu()
// can't be used: its Quit item routes through OnBeforeClose
// (v2.16.0 frontend/desktop/darwin/frontend.go:365), which our
// minimise-on-close handler prevents — Cmd+Q would be swallowed. A
// custom submenu calls quit() directly, which sets reallyQuit (the
// same guard the tray's Quit item uses) before runtime.Quit so
// OnBeforeClose lets it through. EditMenu() is included because
// Cmd+C/V/X/A/Z don't work in text fields on macOS without it.
func appMenu(quit func()) *menu.Menu {
	app := menu.NewMenu()
	catchm := app.AddSubmenu("CatchM")
	// Hides the whole app (NSApp hide), like every Mac app's Cmd+H: a
	// Dock click brings it back. WindowHide would hide just the window,
	// which nothing but the show/hide hotkey can restore here.
	catchm.AddText("Hide CatchM", keys.CmdOrCtrl("h"), func(*menu.CallbackData) {
		if appCtx := appContext(); appCtx != nil {
			runtime.Hide(appCtx)
		}
	})
	catchm.AddSeparator()
	catchm.AddText("Quit CatchM", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { quit() })
	app.Append(menu.EditMenu())
	return app
}
