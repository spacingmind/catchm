package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/spacingmind/catchm/internal/winbounds"
)

// windowStateFileName is CATCHM_HOME/window_state.json — desktop-only
// state, deliberately separate from config.yaml (internal/config),
// which holds engine/API settings shared with the non-desktop CLI.
const windowStateFileName = "window_state.json"

// windowStatePollInterval is how often the restored (non-maximized)
// bounds are sampled and persisted if changed. Wails v2 has no move/
// resize event to hook (checked pkg/runtime/events.go and the desktop
// JS runtime bundle; WM_MOVE only exists internally to notify the
// chromium child of a position change, per
// internal/frontend/desktop/windows/window.go — nothing reaches Go or
// JS callers). Polling is the closest portable substitute, and doubles
// as the spec's "debounced" requirement: a save only happens on the
// next tick, not on every pixel of drag.
const windowStatePollInterval = 2 * time.Second

// compactActive mirrors the frontend's compact-mode flag, set via the
// DesktopBridge.SetCompactMode Wails bind method. While true, saves
// are skipped: compact's 480x260 geometry must never overwrite the
// normal bounds it isn't a substitute for.
var compactActive atomic.Bool

// DesktopBridge is bound to the frontend (options.App.Bind) — the only
// Go<->JS surface this app has (everything else goes through the HTTP
// API per M5); M22 §2/§3 added SetCompactMode, §5 added the rest
// (desktop_settings.go: shortcuts, launch-at-login, notifications).
type DesktopBridge struct {
	hk *hotkeyManager
	// updates wires M23's auto-update bound methods (update.go).
	updates *updateManager
	// exp wires the meeting export bound method (export.go).
	exp meetingExporter
}

// SetCompactMode is called by useCompactMode's enter/exit.
func (DesktopBridge) SetCompactMode(active bool) {
	compactActive.Store(active)
}

// SetCompactTransparent toggles the see-through compact overlay
// (docs/plans/active/compact-transparent.md); a no-op where
// unsupported (non-macOS), so the frontend can call it unconditionally.
func (DesktopBridge) SetCompactTransparent(on bool) {
	if transparentSupported() {
		setWindowTransparent(on)
	}
}

// CompactTransparentSupported gates the compact overlay's toggle button.
func (DesktopBridge) CompactTransparentSupported() bool {
	return transparentSupported()
}

// WindowState is the persisted §2 state: the main window's last known
// RESTORED (non-maximized) bounds, plus whether it was maximized —
// tracked separately because WindowGetPosition/GetSize report the
// maximized rect while maximized, not the bounds a restore should use.
type WindowState struct {
	Normal    *winbounds.Rect `json:"normal,omitempty"`
	Maximized bool            `json:"maximized"`
}

func loadWindowState(path string) (*WindowState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &WindowState{}, nil
		}
		return nil, err
	}
	var s WindowState
	if err := json.Unmarshal(data, &s); err != nil {
		return &WindowState{}, nil // corrupt file: start fresh rather than failing launch
	}
	return &s, nil
}

func saveWindowState(path string, s *WindowState) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// windowStateTracker owns the persisted state in memory so the
// periodic poller and the close-time save agree on "last known" and
// only write to disk when something actually changed.
type windowStateTracker struct {
	path string
	mu   sync.Mutex
	last WindowState
}

func newWindowStateTracker(path string) *windowStateTracker {
	t := &windowStateTracker{path: path}
	if s, err := loadWindowState(path); err == nil {
		t.last = *s
	}
	return t
}

// restore applies the persisted, monitor-topology-clamped bounds. Call
// once, from OnStartup.
func (t *windowStateTracker) restore(ctx context.Context) {
	t.mu.Lock()
	normal, maximized := t.last.Normal, t.last.Maximized
	t.mu.Unlock()
	if !usableSavedBounds(normal) {
		// Nothing saved, or a bogus rect (e.g. saved while minimized by a
		// build without that guard): keep the default size, but still
		// honour a saved maximized state.
		if maximized {
			runtime.WindowMaximise(ctx)
		}
		return
	}
	monitors, _ := enumMonitors() // nil on error/non-Windows: Clamp no-ops
	target := winbounds.Clamp(*normal, monitors)
	runtime.WindowSetSize(ctx, target.W, target.H)
	setAbsolutePosition(ctx, target.X, target.Y)
	if maximized {
		runtime.WindowMaximise(ctx)
	}
}

// Below this, saved bounds can't be a real restored main window (the
// default is 1180x760; compact mode never saves). Guards restore against
// state files written while minimized.
const (
	minRestoreW = 400
	minRestoreH = 300
)

func usableSavedBounds(r *winbounds.Rect) bool {
	return r != nil && r.W >= minRestoreW && r.H >= minRestoreH
}

// setAbsolutePosition places the window at the exact absolute (x, y)
// regardless of which monitor WindowSetPosition treats as "current":
// verified against internal/frontend/desktop/windows/winc/
// controlbase.go, SetPos resolves its (x, y) argument as an offset
// from THAT monitor's work-area origin, while Pos()/WindowGetPosition
// returns absolute coordinates — the exact mismatch behind M21's 24px
// compact-mode drift (see docs/plans/active/m22-native-shell.md's
// Decisions). Setting (0, 0) first lands the window exactly at that
// monitor's work-area origin, in the SAME absolute space
// WindowGetPosition reports; reading it back gives the exact offset to
// cancel out. The second SetPosition call resolves against the same
// "current" monitor as the first (the window hasn't moved in between),
// so the arithmetic is exact without needing monitor enumeration here
// at all — unlike the clamping decision in restore, above.
func setAbsolutePosition(ctx context.Context, x, y int) {
	runtime.WindowSetPosition(ctx, 0, 0)
	ox, oy := runtime.WindowGetPosition(ctx)
	runtime.WindowSetPosition(ctx, x-ox, y-oy)
}

// saveIfChanged samples the current bounds and persists them only if
// they differ from the last save (or the last restore, at startup).
func (t *windowStateTracker) saveIfChanged(ctx context.Context) {
	// Minimized: Wails reports the taskbar icon's rect (~160x39), which
	// must never become the "restored" bounds the next launch opens at.
	if compactActive.Load() || runtime.WindowIsMinimised(ctx) {
		return
	}
	maximized := runtime.WindowIsMaximised(ctx)
	next := WindowState{Maximized: maximized}
	if !maximized {
		x, y := runtime.WindowGetPosition(ctx)
		w, h := runtime.WindowGetSize(ctx)
		next.Normal = &winbounds.Rect{X: x, Y: y, W: w, H: h}
	}

	t.mu.Lock()
	if maximized {
		next.Normal = t.last.Normal // keep last known restored bounds while maximized
	}
	changed := t.last.Maximized != next.Maximized || !rectEqual(t.last.Normal, next.Normal)
	if changed {
		t.last = next
	}
	t.mu.Unlock()

	if changed {
		_ = saveWindowState(t.path, &next)
	}
}

func rectEqual(a, b *winbounds.Rect) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// poll runs saveIfChanged on a timer until ctx is done. The close-time
// save is a separate, direct call from OnBeforeClose (not ctx.Done()
// here) since window teardown timing around a cancelled context isn't
// something to race against.
//
// The recover below guards a teardown race found live in M23's update
// flow but latent since M22 (reproduced as pre-existing mechanism, not
// M23-specific): this goroutine polls on its own cadence while any
// quit path (tray Quit, update apply, OS shutdown) destroys the HWND
// inside wails.Run — Wails' WindowGetSize then divides by the DPI of a
// dead window (GetDpiForWindow returns 0) and panics, killing the
// whole process mid-shutdown (an in-flight update apply dies with it).
// pollCtx is only cancelled after wails.Run RETURNS (run's defer), so
// no ordering fix can close this window; a tick that finds the window
// gone has nothing left to save anyway. Panics are swallowed by design.
func (t *windowStateTracker) poll(ctx context.Context) {
	ticker := time.NewTicker(windowStatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if wc := appContext(); wc != nil {
				t.tick(wc)
			}
		}
	}
}

// tick is one poll iteration. Two teardown guards, in order:
//   - ctx.Err(): the app is past wails.Run (or the context is dead for
//     any other reason) — calling into the Wails runtime then is at
//     best a log.Fatal'ing "invalid context" and at worst a use of a
//     destroyed window; there is nothing left to save either way.
//   - recover(): the narrower race the ctx check can't close — the
//     context is still live but the HWND is being destroyed RIGHT NOW
//     (pollCtx only cancels after wails.Run returns, i.e. after the
//     window is already gone). Wails' WindowGetSize divides by
//     GetDpiForWindow(deadHwnd)=0 and panics. See poll's comment.
func (t *windowStateTracker) tick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			_ = r
		}
	}()
	t.saveIfChanged(ctx)
}
