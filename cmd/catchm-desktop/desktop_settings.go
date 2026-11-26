// desktop_settings.go: M22 §5's persisted desktop-only preferences
// (global shortcut bindings, the notifications opt-out) and the
// platform-independent hotkey registration/wiring built on top of
// them. Kept separate from internal/config's config.yaml since these
// are desktop-shell concerns with no meaning for the headless
// `catchm serve` CLI that also uses that shared file.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/hotkeys"
)

// autostartStore is package-level like compactActive/wailsCtx — a
// single-window app doesn't need per-instance DI for one Store value
// (M11's own stated precedent for this kind of shared state).
var autostartStore = newAutostartStore()

const desktopSettingsFileName = "desktop_settings.json"

// DesktopSettings is the on-disk shape of desktop_settings.json.
type DesktopSettings struct {
	Shortcuts            hotkeys.Bindings `json:"shortcuts"`
	NotificationsEnabled bool             `json:"notifications_enabled"`
}

func defaultDesktopSettings() DesktopSettings {
	return DesktopSettings{Shortcuts: hotkeys.DefaultBindings(), NotificationsEnabled: true}
}

func loadDesktopSettings(path string) DesktopSettings {
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultDesktopSettings()
	}
	var s DesktopSettings
	if err := json.Unmarshal(data, &s); err != nil || len(s.Shortcuts) == 0 {
		return defaultDesktopSettings()
	}
	return s
}

func saveDesktopSettings(path string, s DesktopSettings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// activeHotkey is one registered global shortcut.
type activeHotkey interface {
	keydown() <-chan struct{}
	unregister()
}

// platformHotkeys is implemented by hotkeys_windows.go (real
// RegisterHotKey via golang.design/x/hotkey) and hotkeys_other.go (a
// stub that always errors — M22 is Windows-only scope), so this file
// stays platform-independent.
type platformHotkeys interface {
	register(combo hotkeys.Combo) (activeHotkey, error)
}

// hotkeyManager owns the currently-registered global shortcuts and the
// persisted DesktopSettings they (and the notifications flag) come
// from.
type hotkeyManager struct {
	path     string
	platform platformHotkeys
	handler  http.Handler
	cfg      *config.Config

	mu         sync.Mutex
	settings   DesktopSettings
	active     map[hotkeys.Action]activeHotkey
	lastErrors map[hotkeys.Action]string
}

func newHotkeyManager(path string, platform platformHotkeys, handler http.Handler, cfg *config.Config) *hotkeyManager {
	return &hotkeyManager{
		path:     path,
		platform: platform,
		handler:  handler,
		cfg:      cfg,
		settings: loadDesktopSettings(path),
		active:   map[hotkeys.Action]activeHotkey{},
	}
}

// Settings returns a copy of the current persisted settings.
func (m *hotkeyManager) Settings() DesktopSettings {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	s.Shortcuts = m.settings.Shortcuts.Clone()
	return s
}

// applyAll registers every currently-persisted shortcut, unregistering
// anything already active first (used at startup and after Set
// changes the bindings). Returns per-action errors for combos that
// failed to register (invalid, a duplicate between our own actions, or
// taken by another app) — those actions simply have no active hotkey,
// never a silent no-op.
//
// hotkeys.ValidateBindings runs first so two of OUR OWN actions
// pointing at the same combo are flagged deterministically on both
// (real Windows testing found this matters: without it, Windows'
// RegisterHotKey rejects the SECOND registration of a combo we
// registered first ourselves, so which action got flagged depended on
// hotkeys.Actions' iteration order rather than reporting the actual
// conflict — still not a silent failure, but a confusing one).
func (m *hotkeyManager) applyAll() map[hotkeys.Action]string {
	m.mu.Lock()
	bindings := m.settings.Shortcuts.Clone()
	m.mu.Unlock()

	m.unregisterAll()

	errs := hotkeys.ValidateBindings(bindings)
	for _, action := range hotkeys.Actions {
		if _, invalid := errs[action]; invalid {
			continue
		}
		combo, err := hotkeys.ParseCombo(bindings[action])
		if err != nil {
			// Defensive: ValidateBindings already parsed this combo
			// successfully, so this can't actually happen.
			errs[action] = err.Error()
			continue
		}
		hk, err := m.platform.register(combo)
		if err != nil {
			errs[action] = err.Error()
			continue
		}
		m.mu.Lock()
		m.active[action] = hk
		m.mu.Unlock()
		go m.watch(action, hk)
	}
	m.mu.Lock()
	m.lastErrors = errs
	m.mu.Unlock()
	return errs
}

// LastErrors returns the registration errors from the most recent
// applyAll (startup, or the last set() call) — what GetShortcuts shows
// the Settings page on load, not just right after a change.
func (m *hotkeyManager) LastErrors() map[hotkeys.Action]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[hotkeys.Action]string, len(m.lastErrors))
	for k, v := range m.lastErrors {
		out[k] = v
	}
	return out
}

// watch runs the action for `action` on every key-down until the
// hotkey is unregistered (its keydown channel then closes). Reads the
// live Wails context fresh via appContext() at trigger time (not a
// captured one) — same pattern as tray.go's click handlers.
func (m *hotkeyManager) watch(action hotkeys.Action, hk activeHotkey) {
	for range hk.keydown() {
		m.trigger(action)
	}
}

func (m *hotkeyManager) trigger(action hotkeys.Action) {
	ctx := appContext()
	if ctx == nil {
		return
	}
	switch action {
	case hotkeys.ActionShowHide:
		toggleMainWindow(ctx)
	case hotkeys.ActionStartStopMeeting:
		onTrayStartStopClick(m.handler, m.cfg)
	case hotkeys.ActionToggleCompact:
		runtime.EventsEmit(ctx, trayToggleCompactEvent)
	}
}

func (m *hotkeyManager) unregisterAll() {
	m.mu.Lock()
	active := m.active
	m.active = map[hotkeys.Action]activeHotkey{}
	m.mu.Unlock()
	for _, hk := range active {
		hk.unregister()
	}
}

// set replaces the shortcut bindings, persists them, and re-applies —
// returning the resulting per-action registration errors so a failed
// combo (already taken, invalid) surfaces inline in Settings instead
// of failing silently. Actions that DID register keep their new combo
// even if a sibling action's combo failed.
func (m *hotkeyManager) set(next hotkeys.Bindings) (DesktopSettings, map[hotkeys.Action]string) {
	m.mu.Lock()
	m.settings.Shortcuts = next.Clone()
	settings := m.settings
	m.mu.Unlock()
	_ = saveDesktopSettings(m.path, settings)
	errs := m.applyAll()
	return m.Settings(), errs
}

func (m *hotkeyManager) setNotificationsEnabled(enabled bool) {
	m.mu.Lock()
	m.settings.NotificationsEnabled = enabled
	settings := m.settings
	m.mu.Unlock()
	_ = saveDesktopSettings(m.path, settings)
}

// ShortcutsInfo is what DesktopBridge.GetShortcuts/SetShortcuts return
// to the frontend: the bindings currently persisted, plus any
// per-action registration error (an already-taken combo, an invalid
// one, or "Windows only" on any other OS) — surfaced inline in
// Settings, never silently dropped.
type ShortcutsInfo struct {
	Bindings hotkeys.Bindings          `json:"bindings"`
	Errors   map[hotkeys.Action]string `json:"errors"`
}

// GetShortcuts reflects the current bindings and their live
// registration status (e.g. on Settings page load, not just right
// after a change).
func (d DesktopBridge) GetShortcuts() ShortcutsInfo {
	return ShortcutsInfo{Bindings: d.hk.Settings().Shortcuts, Errors: d.hk.LastErrors()}
}

// SetShortcuts replaces the bindings and re-registers every hotkey,
// returning the same shape as GetShortcuts.
func (d DesktopBridge) SetShortcuts(bindings map[string]string) ShortcutsInfo {
	next := hotkeys.Bindings{}
	for k, v := range bindings {
		next[hotkeys.Action(k)] = v
	}
	settings, errs := d.hk.set(next)
	return ShortcutsInfo{Bindings: settings.Shortcuts, Errors: errs}
}

// GetAutostart reports whether launch-at-login is currently enabled.
func (DesktopBridge) GetAutostart() (bool, error) {
	return autostartStore.IsEnabled()
}

// SetAutostart toggles launch-at-login (HKCU Run key, opt-in — see
// autostart_windows.go).
func (DesktopBridge) SetAutostart(enabled bool) error {
	return autostartStore.SetEnabled(enabled)
}

// GetNotificationsEnabled reports the toast opt-out setting.
func (d DesktopBridge) GetNotificationsEnabled() bool {
	return d.hk.Settings().NotificationsEnabled
}

// SetNotificationsEnabled toggles the toast opt-out setting.
func (d DesktopBridge) SetNotificationsEnabled(enabled bool) {
	d.hk.setNotificationsEnabled(enabled)
}

// ReadyForEvents is the frontend's half of the cold-start protocol-
// activation handshake: App.tsx calls it once its openMeetingEvent
// subscription is in place, flipping frontendReady so
// handleLaunchURI's EventsEmit (a catchm://meeting/<id> toast click
// that launched the whole app) is guaranteed a listener instead of
// being emitted into the void before the webview ran any JS.
func (DesktopBridge) ReadyForEvents() {
	frontendReady.Store(true)
}

// NotifyCloudSyncConflict is called by SettingsCloudSync.tsx right
// after a manual sync's response shows conflicted meetings — there is
// no background/automatic sync loop in this codebase to poll for this
// the way tray.go polls capture state (see
// docs/plans/active/m22-native-shell.md's Decisions), so the toast
// fires from the same place the conflict is already detected today,
// rather than adding new sync automation this milestone doesn't ask
// for.
func (d DesktopBridge) NotifyCloudSyncConflict(count int) {
	if count <= 0 {
		return
	}
	plural := "s"
	if count == 1 {
		plural = ""
	}
	notify(d.hk, "CatchM", fmt.Sprintf("Cloud sync: %d meeting%s need attention", count, plural), "")
}
