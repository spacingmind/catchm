package main

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/spacingmind/catchm/internal/hotkeys"
)

// fakePlatformHotkeys lets hotkeyManager's registration/wiring logic
// be tested without a real Windows RegisterHotKey call. failCombo, if
// set, makes that exact combo string fail to "register" — the same
// shape a real "already taken by another app" failure has.
type fakePlatformHotkeys struct {
	mu         sync.Mutex
	registered map[string]*fakeActiveHotkey
	failCombo  string
}

func newFakePlatformHotkeys() *fakePlatformHotkeys {
	return &fakePlatformHotkeys{registered: map[string]*fakeActiveHotkey{}}
}

func (f *fakePlatformHotkeys) register(combo hotkeys.Combo) (activeHotkey, error) {
	s := combo.String()
	f.mu.Lock()
	defer f.mu.Unlock()
	if s == f.failCombo {
		return nil, errFakeRegisterFailed
	}
	hk := &fakeActiveHotkey{ch: make(chan struct{})}
	f.registered[s] = hk
	return hk, nil
}

func (f *fakePlatformHotkeys) isRegistered(combo string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	hk, ok := f.registered[combo]
	return ok && !hk.unregistered
}

var errFakeRegisterFailed = fakeErr("hotkey: already taken (fake)")

type fakeErr string

func (e fakeErr) Error() string { return string(e) }

type fakeActiveHotkey struct {
	mu           sync.Mutex
	ch           chan struct{}
	unregistered bool
}

func (h *fakeActiveHotkey) keydown() <-chan struct{} { return h.ch }

func (h *fakeActiveHotkey) unregister() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.unregistered {
		return
	}
	h.unregistered = true
	close(h.ch)
}

func TestHotkeyManagerAppliesDefaultsOnFirstLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop_settings.json")
	platform := newFakePlatformHotkeys()
	m := newHotkeyManager(path, platform, nil, nil)

	errs := m.applyAll()
	if len(errs) != 0 {
		t.Fatalf("applyAll() errs = %v, want none", errs)
	}
	defaults := hotkeys.DefaultBindings()
	for action, combo := range defaults {
		parsed, err := hotkeys.ParseCombo(combo)
		if err != nil {
			t.Fatalf("ParseCombo(%q) error = %v", combo, err)
		}
		if !platform.isRegistered(parsed.String()) {
			t.Fatalf("action %s's default combo %s was never registered", action, parsed)
		}
	}
}

func TestHotkeyManagerSetPersistsAndReRegisters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop_settings.json")
	platform := newFakePlatformHotkeys()
	m := newHotkeyManager(path, platform, nil, nil)
	m.applyAll()

	next := hotkeys.DefaultBindings()
	next[hotkeys.ActionShowHide] = "Ctrl+Alt+X"
	settings, errs := m.set(next)
	if len(errs) != 0 {
		t.Fatalf("set() errs = %v, want none", errs)
	}
	if settings.Shortcuts[hotkeys.ActionShowHide] != "Ctrl+Alt+X" {
		t.Fatalf("settings.Shortcuts[ShowHide] = %q, want Ctrl+Alt+X", settings.Shortcuts[hotkeys.ActionShowHide])
	}
	if !platform.isRegistered("Ctrl+Alt+X") {
		t.Fatal("new combo Ctrl+Alt+X was never registered")
	}
	if platform.isRegistered("Ctrl+Alt+C") { // the old default
		t.Fatal("old combo Ctrl+Alt+C is still registered after set() replaced it")
	}

	// Persisted to disk, and a fresh manager loads the new binding.
	reloaded := newHotkeyManager(path, platform, nil, nil)
	if got := reloaded.Settings().Shortcuts[hotkeys.ActionShowHide]; got != "Ctrl+Alt+X" {
		t.Fatalf("reloaded Shortcuts[ShowHide] = %q, want Ctrl+Alt+X", got)
	}
}

// A combo that fails to register (e.g. already taken by another app)
// surfaces inline as a per-action error — the sibling actions still
// register successfully.
func TestHotkeyManagerSurfacesRegistrationFailureInline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop_settings.json")
	platform := newFakePlatformHotkeys()
	platform.failCombo = "Ctrl+Alt+C" // the ShowHide default
	m := newHotkeyManager(path, platform, nil, nil)

	errs := m.applyAll()
	if errs[hotkeys.ActionShowHide] == "" {
		t.Fatal("applyAll() did not report the failed ShowHide registration")
	}
	if errs[hotkeys.ActionStartStopMeeting] != "" || errs[hotkeys.ActionToggleCompact] != "" {
		t.Fatalf("applyAll() = %v, want only ShowHide flagged", errs)
	}
	if !platform.isRegistered("Ctrl+Alt+M") {
		t.Fatal("StartStopMeeting's combo should still have registered despite ShowHide's failure")
	}

	// GetShortcuts-equivalent (LastErrors) reflects the same failure
	// without a fresh applyAll call.
	if got := m.LastErrors()[hotkeys.ActionShowHide]; got == "" {
		t.Fatal("LastErrors() lost the registration failure")
	}
}

// Two of OUR OWN actions pointing at the same combo must both be
// flagged (via hotkeys.ValidateBindings, wired into applyAll) —
// verified live on real Windows that without this, only whichever
// action lost the OS-level RegisterHotKey race got an error, which
// depended on iteration order rather than reporting the actual
// conflict on both sides.
func TestHotkeyManagerDuplicateComboFlagsBothActions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop_settings.json")
	platform := newFakePlatformHotkeys()
	m := newHotkeyManager(path, platform, nil, nil)

	next := hotkeys.DefaultBindings()
	next[hotkeys.ActionShowHide] = "Ctrl+Alt+M" // same as StartStopMeeting's default
	_, errs := m.set(next)

	if errs[hotkeys.ActionShowHide] == "" || errs[hotkeys.ActionStartStopMeeting] == "" {
		t.Fatalf("set() = %v, want both colliding actions flagged", errs)
	}
	if errs[hotkeys.ActionToggleCompact] != "" {
		t.Fatalf("set() = %v, want ToggleCompact untouched", errs)
	}
	// Neither colliding combo should have reached platform.register —
	// caught by ValidateBindings before ever attempting the OS call.
	if platform.isRegistered("Ctrl+Alt+M") {
		t.Fatal("a duplicate combo was registered with the OS despite both actions being flagged invalid")
	}
}

func TestHotkeyManagerInvalidComboIsRejectedAtSetTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop_settings.json")
	platform := newFakePlatformHotkeys()
	m := newHotkeyManager(path, platform, nil, nil)
	m.applyAll()

	next := hotkeys.DefaultBindings()
	next[hotkeys.ActionToggleCompact] = "not a combo"
	_, errs := m.set(next)
	if errs[hotkeys.ActionToggleCompact] == "" {
		t.Fatal("set() with an unparseable combo did not report an error")
	}
}

func TestHotkeyManagerUnregisterAllClearsEverything(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop_settings.json")
	platform := newFakePlatformHotkeys()
	m := newHotkeyManager(path, platform, nil, nil)
	m.applyAll()

	m.unregisterAll()
	for _, combo := range hotkeys.DefaultBindings() {
		parsed, _ := hotkeys.ParseCombo(combo)
		if platform.isRegistered(parsed.String()) {
			t.Fatalf("combo %s still registered after unregisterAll()", parsed)
		}
	}
}

func TestHotkeyManagerNotificationsEnabledPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop_settings.json")
	m := newHotkeyManager(path, newFakePlatformHotkeys(), nil, nil)
	if !m.Settings().NotificationsEnabled {
		t.Fatal("default NotificationsEnabled = false, want true")
	}
	m.setNotificationsEnabled(false)
	if m.Settings().NotificationsEnabled {
		t.Fatal("setNotificationsEnabled(false) did not take effect")
	}
	reloaded := newHotkeyManager(path, newFakePlatformHotkeys(), nil, nil)
	if reloaded.Settings().NotificationsEnabled {
		t.Fatal("NotificationsEnabled=false was not persisted across reload")
	}
}
