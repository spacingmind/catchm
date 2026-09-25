// Package hotkeys parses and validates global-shortcut combo strings
// (e.g. "Ctrl+Alt+M") and defines the fixed set of actions M22 §5 binds
// them to. It has no platform dependency — the actual OS-level
// registration (golang.design/x/hotkey on Windows, chosen over a
// hand-rolled message loop per docs/plans/active/m22-native-shell.md's
// research notes) lives in cmd/catchm-desktop, behind an interface, so
// this package stays testable on any OS.
package hotkeys

import (
	"fmt"
	"sort"
	"strings"
)

// Action is one of the three global shortcuts M22 §5 exposes in
// Settings → Shortcuts.
type Action string

const (
	ActionShowHide         Action = "show_hide"
	ActionStartStopMeeting Action = "start_stop_meeting"
	ActionToggleCompact    Action = "toggle_compact"
)

// Actions lists every action in a stable order (JSON output, defaults).
var Actions = []Action{ActionShowHide, ActionStartStopMeeting, ActionToggleCompact}

// Bindings maps each action to its combo string.
type Bindings map[Action]string

// DefaultBindings returns sensible, mutually distinct defaults.
// ToggleCompact deliberately does NOT reuse Ctrl+Shift+L — that's
// already M21's FOCUSED-window shortcut (useCompactMode.ts's own
// keydown listener); registering the same combo as a SYSTEM-WIDE
// hotkey too would fire both handlers at once while the window has
// focus, double-toggling compact mode.
func DefaultBindings() Bindings {
	return Bindings{
		ActionShowHide:         "Ctrl+Alt+C",
		ActionStartStopMeeting: "Ctrl+Alt+M",
		ActionToggleCompact:    "Ctrl+Alt+L",
	}
}

// Clone returns a copy so callers can't mutate a shared map.
func (b Bindings) Clone() Bindings {
	out := make(Bindings, len(b))
	for k, v := range b {
		out[k] = v
	}
	return out
}

// Modifier is one of the four combo modifiers RegisterHotKey supports.
type Modifier string

const (
	ModCtrl  Modifier = "Ctrl"
	ModAlt   Modifier = "Alt"
	ModShift Modifier = "Shift"
	ModWin   Modifier = "Win"
)

// keyNames is the exact key vocabulary golang.design/x/hotkey@v0.6.1
// exposes on Windows (hotkey_windows.go's Key constants) — verified
// against that source before choosing this list, so every name here is
// guaranteed to have a real Windows virtual-key mapping.
var keyNames = func() map[string]bool {
	names := []string{
		"Space", "Return", "Enter", "Escape", "Esc", "Delete", "Tab",
		"Left", "Right", "Up", "Down",
	}
	for c := 'A'; c <= 'Z'; c++ {
		names = append(names, string(c))
	}
	for d := '0'; d <= '9'; d++ {
		names = append(names, string(d))
	}
	for i := 1; i <= 20; i++ {
		names = append(names, fmt.Sprintf("F%d", i))
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[strings.ToUpper(n)] = true
	}
	return set
}()

// Combo is a parsed, normalized hotkey combination: at least one
// modifier plus exactly one key.
type Combo struct {
	Mods []Modifier
	Key  string // canonical form, e.g. "L", "F5", "Space"
}

// String renders the combo back to its canonical "+"-joined form, in a
// fixed modifier order (Ctrl, Alt, Shift, Win) regardless of input
// order — so re-parsing String()'s own output always round-trips.
func (c Combo) String() string {
	order := []Modifier{ModCtrl, ModAlt, ModShift, ModWin}
	have := map[Modifier]bool{}
	for _, m := range c.Mods {
		have[m] = true
	}
	parts := make([]string, 0, len(c.Mods)+1)
	for _, m := range order {
		if have[m] {
			parts = append(parts, string(m))
		}
	}
	parts = append(parts, c.Key)
	return strings.Join(parts, "+")
}

// ParseCombo parses a "+"-separated combo string (e.g. "Ctrl+Shift+L"),
// case-insensitive, modifier order not significant. It requires at
// least one modifier (a bare, unmodified global hotkey would hijack
// every ordinary keystroke system-wide) and exactly one non-modifier
// key from the vocabulary golang.design/x/hotkey actually supports.
func ParseCombo(s string) (Combo, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Combo{}, fmt.Errorf("hotkeys: empty combo")
	}
	tokens := strings.Split(s, "+")
	var mods []Modifier
	seen := map[Modifier]bool{}
	var key string
	for _, raw := range tokens {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			return Combo{}, fmt.Errorf("hotkeys: %q has an empty segment", s)
		}
		if m, ok := parseModifier(tok); ok {
			if seen[m] {
				return Combo{}, fmt.Errorf("hotkeys: %q repeats modifier %s", s, m)
			}
			seen[m] = true
			mods = append(mods, m)
			continue
		}
		if key != "" {
			return Combo{}, fmt.Errorf("hotkeys: %q has more than one key (%s and %s)", s, key, tok)
		}
		norm, ok := normalizeKey(tok)
		if !ok {
			return Combo{}, fmt.Errorf("hotkeys: %q is not a recognized key", tok)
		}
		key = norm
	}
	if len(mods) == 0 {
		return Combo{}, fmt.Errorf("hotkeys: %q needs at least one modifier (Ctrl/Alt/Shift/Win)", s)
	}
	if key == "" {
		return Combo{}, fmt.Errorf("hotkeys: %q has no key", s)
	}
	return Combo{Mods: mods, Key: key}, nil
}

func parseModifier(tok string) (Modifier, bool) {
	switch strings.ToUpper(tok) {
	case "CTRL", "CONTROL":
		return ModCtrl, true
	case "ALT":
		return ModAlt, true
	case "SHIFT":
		return ModShift, true
	case "WIN", "SUPER", "CMD", "META":
		return ModWin, true
	default:
		return "", false
	}
}

func normalizeKey(tok string) (string, bool) {
	upper := strings.ToUpper(tok)
	if !keyNames[upper] {
		return "", false
	}
	switch upper {
	case "ENTER":
		return "Return", true
	case "ESC":
		return "Escape", true
	}
	if len(upper) == 1 {
		return upper, true
	}
	return strings.ToUpper(upper[:1]) + strings.ToLower(upper[1:]), true
}

// ValidateBindings parses every combo and reports any that don't
// parse, plus the same combo assigned to more than one action (they'd
// silently shadow each other at registration time).
func ValidateBindings(b Bindings) map[Action]string {
	errs := map[Action]string{}
	byCombo := map[string][]Action{}
	for _, action := range Actions {
		combo, ok := b[action]
		if !ok || combo == "" {
			errs[action] = "no shortcut set"
			continue
		}
		parsed, err := ParseCombo(combo)
		if err != nil {
			errs[action] = err.Error()
			continue
		}
		norm := parsed.String()
		byCombo[norm] = append(byCombo[norm], action)
	}
	for combo, actions := range byCombo {
		if len(actions) < 2 {
			continue
		}
		sort.Slice(actions, func(i, j int) bool { return actions[i] < actions[j] })
		for _, a := range actions {
			errs[a] = fmt.Sprintf("%s is also bound to another shortcut", combo)
		}
	}
	return errs
}
