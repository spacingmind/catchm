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

// hideWindowOnClose is passed to Wails' HideWindowOnClose. On macOS
// it makes the red close button run [NSApp hide:] inside Wails'
// WindowDelegate (v2.16.0 WindowDelegate.m windowShouldClose) without
// ever reaching OnBeforeClose; a Dock click unhides the app, and
// recording keeps running meanwhile.
const hideWindowOnClose = true

// onCloseRequest runs from OnBeforeClose. With the close button
// handled natively (hideWindowOnClose), everything that still arrives
// here is a real quit: Cmd+Q, Dock → Quit, logout/shutdown — all of
// them go through applicationShouldTerminate → "Q" → OnBeforeClose.
// Preventing it (as the old minimise-on-close did) made the app
// unquittable from the Dock and blocked logout.
func onCloseRequest(context.Context) (prevent bool) { return false }

// showMainWindow brings CatchM back whether the close button hid the
// whole app (NSApp hide — WindowShow alone doesn't unhide it) or the
// window was minimised.
func showMainWindow(ctx context.Context) {
	windowVisible.Store(true)
	runtime.Show(ctx)
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
}

// toggleMainWindow is the show/hide hotkey. windowVisible can't be
// trusted here: the close button hides the app natively, so ask
// AppKit instead. Hiding is app-level, like Cmd+H, so the Dock icon
// brings it back (WindowHide would strand the window).
func toggleMainWindow(ctx context.Context) {
	if appHiddenOrMinimised() {
		showMainWindow(ctx)
		return
	}
	windowVisible.Store(false)
	runtime.Hide(ctx)
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
