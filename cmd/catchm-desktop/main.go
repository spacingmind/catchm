// Command catchm-desktop opens a native desktop window showing the same
// web UI and API that `catchm serve` exposes over HTTP, reusing
// internal/server's http.Handler directly as the window's asset source
// (no separate frontend, no TCP port, no code fork). A system tray icon
// keeps the app alive in the background when the window is closed, so an
// in-progress capture survives; the tray's Quit item is the only way the
// app actually exits.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailswindows "github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/mcpserver"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/server"
	"github.com/spacingmind/catchm/internal/tools"
)

// appID is a stable reverse-DNS identifier shared by SingleInstanceLock
// (below) and the AUMID toast notifications need (M22 §5,
// SetCurrentProcessExplicitAppUserModelID) — the spec asks for the same
// string in both places.
const appID = "com.catchm.desktop"

// Brand colours per theme (web/src/index.css :root and .dark:
// --background, --foreground, --secondary), packed for
// windows.ThemeSettings via windows.RGB. The window starts on
// windows.SystemDefault, matching the web UI's default "system" theme;
// web/src/lib/theme.ts then pins it with the Wails JS runtime's
// WindowSetLightTheme / WindowSetDarkTheme (or hands it back with
// WindowSetSystemDefaultTheme) and sets the webview background, so no
// bridge method is needed.
var (
	titleBarColour    = wailswindows.RGB(0xFB, 0xF7, 0xEA) // --background
	titleTextColour   = wailswindows.RGB(0x6C, 0x35, 0x24) // --foreground
	titleBorderColour = wailswindows.RGB(0xD9, 0xC4, 0xB4) // --secondary

	darkTitleBarColour    = wailswindows.RGB(0x1E, 0x14, 0x11) // .dark --background
	darkTitleTextColour   = wailswindows.RGB(0xFB, 0xF7, 0xEA) // .dark --foreground
	darkTitleBorderColour = wailswindows.RGB(0x3B, 0x2A, 0x22) // .dark --secondary
)

// Shared between the Wails callbacks and the systray goroutine: the
// runtime context from OnStartup, and the flag the tray's Quit item
// sets so OnBeforeClose lets the real close through.
var (
	mu         sync.Mutex
	wailsCtx   context.Context
	reallyQuit bool
)

// windowVisible tracks show/hide state ourselves — Wails v2 has no
// WindowIsVisible getter (checked pkg/runtime/window.go) — so the M22
// §5 show/hide hotkey knows which direction to toggle, and toasts only
// fire while the window is actually hidden. Starts true: the window
// opens visible on a normal launch (false only for --autostart, which
// sets StartHidden below).
var windowVisible atomic.Bool

func main() {
	// `<CatchM executable> mcp` (CatchM.app/Contents/MacOS/CatchM on macOS,
	// catchm-desktop.exe on Windows): the stdio MCP server an MCP client launches
	// (docs/plans/active/local-mcp.md). No window, no tray, no
	// single-instance lock — it only reads/writes the meeting folder.
	if len(os.Args) >= 2 && os.Args[1] == "mcp" {
		if err := mcpserver.RunStdio(context.Background(), version); err != nil {
			fmt.Fprintln(os.Stderr, "catchm mcp:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	home := os.Getenv("CATCHM_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolve home dir: %w", err)
		}
		home = filepath.Join(userHome, ".catchm")
	}

	store, err := meeting.NewStore(filepath.Join(home, "meetings"))
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	handler := server.New(tools.New(store), cfg)

	// M22 §5 launch-at-login: autostart_windows.go writes this exact
	// flag into the HKCU Run command line (internal/autostart.
	// CommandLine); starting hidden here is what makes "launch at
	// login" open to the tray instead of popping the window on every
	// sign-in.
	autostartLaunch := slices.Contains(os.Args[1:], "--autostart")
	windowVisible.Store(!autostartLaunch)

	// catchm://meeting/<id> protocol activation (toast click): captured
	// here because os.Args is read-once at process start; handled in
	// OnStartup (the Wails runtime context doesn't exist yet here).
	// A second instance never gets this far — SingleInstanceLock
	// forwards its args to the first instance and exits first — so
	// this is exclusively the app-not-running (cold start) case.
	launchMeetingID := parseMeetingURI(meetingArgFromArgs(os.Args))

	setProcessAUMID(appID)

	pollCtx, stopPoll := context.WithCancel(context.Background())
	defer stopPoll()

	hkManager := newHotkeyManager(filepath.Join(home, desktopSettingsFileName), newPlatformHotkeys(), handler, cfg)

	// M23: update checks (startup-delayed + 24h periodic). Bound here so
	// DesktopBridge's update methods (update.go) reach the same manager.
	updates := newUpdateManager()

	startTray(pollCtx, handler, cfg, hkManager, stopPoll)

	stateTracker := newWindowStateTracker(filepath.Join(home, windowStateFileName))
	go stateTracker.poll(pollCtx)

	wailsErr := wails.Run(&options.App{
		Title:       "CatchM",
		Width:       1180,
		Height:      760,
		StartHidden: autostartLaunch,
		// Webview background before the page paints (Wails defaults to
		// white). Light brand cream; a dark-theme user sees it only until
		// lib/theme.ts's WindowSetBackgroundColour runs at page load.
		BackgroundColour: options.NewRGB(0xFB, 0xF7, 0xEA),
		// Wails v2 only enables the macOS zoom (green) button when Mac
		// options are set (zoomable is read inside its `Mac != nil`
		// branch), so an empty struct is what turns it on.
		// WebviewIsTransparent lets the compact overlay show the content
		// behind it once setWindowTransparent makes the NSWindow
		// non-opaque (transparent_darwin.go); while the window is opaque
		// this changes nothing — the page paints its own background.
		Mac:               &mac.Options{WebviewIsTransparent: true},
		HideWindowOnClose: hideWindowOnClose,
		AssetServer: &assetserver.Options{
			Handler: handler,
		},
		Menu: appMenu(func() {
			mu.Lock()
			reallyQuit = true
			mu.Unlock()
			if appCtx := appContext(); appCtx != nil {
				runtime.Quit(appCtx)
			}
		}),
		Bind: []interface{}{
			&DesktopBridge{hk: hkManager, updates: updates, exp: handler},
		},
		// M22 §4 WebView hardening. EnableDefaultContextMenu and Debug are
		// deliberately left unset: verified against frontend.go directly
		// — PutAreDefaultContextMenusEnabled(f.debug ||
		// EnableDefaultContextMenu) and PutAreDevToolsEnabled(devtoolsEnabled)
		// (gated on the `devtools` build tag Taskfile's desktop:build never
		// passes) already disable the context menu and devtools in a
		// production build and re-enable both in `wails dev`, matching the
		// spec exactly with no extra code. File/URL drops onto the webview:
		// DragAndDrop.DisableWebViewDrop (its own doc comment: "prevent
		// accidental file opening of dragged in files in the webview").
		DragAndDrop: &options.DragAndDrop{
			DisableWebViewDrop: true,
		},
		// SingleInstanceLock: verified against
		// internal/frontend/desktop/windows/{single_instance,frontend}.go
		// — a second launch's args are forwarded via WM_COPYDATA to a
		// hidden message-only window in the first instance (which then
		// os.Exit(0)s itself) and OnSecondInstanceLaunch fires on the
		// FIRST instance; WindowShow already un-minimizes and focuses
		// (win32.RestoreWindow when minimized, else ShowWindow, always
		// finishing with SetForegroundWindow — read directly in
		// frontend.go's ShowWindow), so no separate unminimize/focus
		// calls are needed here.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: appID,
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				if ctx := appContext(); ctx != nil {
					// A toast click while the app is already running:
					// Windows launches a second exe with the
					// catchm://meeting/<id> arg; the lock forwards it
					// here (WM_COPYDATA) and this instance exits the
					// second one. Navigate when the arg is a meeting
					// URI, always show the window.
					if id := parseMeetingURI(meetingArgFromArgs(data.Args)); id != "" {
						runtime.EventsEmit(ctx, openMeetingEvent, id)
					}
					showMainWindow(ctx)
				}
			},
		},
		// Native title bar kept (see docs/plans/active/m22-native-shell.md:
		// frameless loses Snap Layouts/Alt+Space, and Wails v2 has no
		// WM_NCHITTEST integration to get them back). Mica shows through
		// instead — WindowIsTranslucent + BackdropType is the v2.16 pair
		// that enables it (pkg/options/windows/windows.go); Wails' own
		// win32.SupportsBackdropTypes() (internal/frontend/desktop/windows/
		// win32/theme.go) gates this on Windows 11 22621+ and silently
		// falls back to a plain window below that build — no fallback
		// logic needed on this side.
		Windows: &wailswindows.Options{
			WindowIsTranslucent: true,
			// WebviewIsTransparent lets the compact see-through mode
			// (transparent_windows.go) composite alpha-0 pixels down to
			// the DWM backdrop. Normal mode is visually unchanged:
			// index.css's desktop-mica:not(.compact-transparent) rules
			// re-paint --background where the transparent webview would
			// otherwise show Mica.
			WebviewIsTransparent: true,
			BackdropType:         wailswindows.Mica,
			Theme:                wailswindows.SystemDefault,
			// M22 §4: browser zoom off (Ctrl+±, Ctrl+wheel, pinch) — verified
			// against frontend.go: IsZoomControlEnabled maps straight to
			// WebView2's PutIsZoomControlEnabled (its own docs: gates
			// Ctrl+/Ctrl-/Ctrl+MouseWheel), DisablePinchZoom to
			// PutIsPinchZoomEnabled(!DisablePinchZoom).
			IsZoomControlEnabled: false,
			DisablePinchZoom:     true,
			CustomTheme: &wailswindows.ThemeSettings{
				LightModeTitleBar:          titleBarColour,
				LightModeTitleText:         titleTextColour,
				LightModeBorder:            titleBorderColour,
				LightModeTitleBarInactive:  titleBarColour,
				LightModeTitleTextInactive: titleTextColour,
				LightModeBorderInactive:    titleBorderColour,
				DarkModeTitleBar:           darkTitleBarColour,
				DarkModeTitleText:          darkTitleTextColour,
				DarkModeBorder:             darkTitleBorderColour,
				DarkModeTitleBarInactive:   darkTitleBarColour,
				DarkModeTitleTextInactive:  darkTitleTextColour,
				DarkModeBorderInactive:     darkTitleBorderColour,
			},
		},
		OnStartup: func(ctx context.Context) {
			mu.Lock()
			wailsCtx = ctx
			mu.Unlock()
			stateTracker.restore(ctx)
			hkManager.applyAll()
			// Cold-start protocol activation: the webview may not have
			// run any JS yet (EventsEmit before the frontend subscribes
			// would be lost), so this is deferred a beat — see
			// handleLaunchURI.
			if launchMeetingID != "" {
				go handleLaunchURI(ctx, launchMeetingID)
			}
			updates.startBackgroundChecks(ctx)
		},
		OnBeforeClose: func(ctx context.Context) (prevent bool) {
			stateTracker.saveIfChanged(ctx)
			mu.Lock()
			quit := reallyQuit
			mu.Unlock()
			if quit {
				return false
			}
			return onCloseRequest(ctx)
		},
		// M22 §5: unregister every global hotkey on quit — a stale
		// RegisterHotKey would otherwise keep intercepting its combo
		// system-wide even after the process exits, if the OS is slow to
		// reclaim it.
		OnShutdown: func(context.Context) {
			hkManager.unregisterAll()
		},
	})

	// M23: after the UI exits, finish a pending update — wait for the
	// elevated installer to replace files, then relaunch the new exe
	// (update_windows.go). Runs after wails.Run returns because the
	// running exe must exit before the installer can overwrite it; an
	// update-relaunch failure never blocks the exit itself.
	if err := finishUpdateRelaunch(); err != nil {
		log.Printf("update: %v", err)
	}
	return wailsErr
}

func appContext() context.Context {
	mu.Lock()
	defer mu.Unlock()
	return wailsCtx
}
