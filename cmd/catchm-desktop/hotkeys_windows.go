//go:build windows

package main

import (
	"fmt"

	"golang.design/x/hotkey"

	"github.com/spacingmind/catchm/internal/hotkeys"
)

// winHotkeys implements platformHotkeys (hotkey_manager.go) using
// golang.design/x/hotkey — RegisterHotKey under the hood, chosen over
// a hand-rolled message loop per the spec's research notes: verified
// against its actual v0.6.1 hotkey_windows.go, each Hotkey runs its
// OWN LockOSThread'd goroutine with its own PeekMessage/GetMessage
// loop (RegisterHotKey(0, ...) — thread 0 means "this thread"), so it
// never shares or fights Wails' or systray's own message loops.
type winHotkeys struct{}

func newPlatformHotkeys() platformHotkeys { return winHotkeys{} }

func (winHotkeys) register(combo hotkeys.Combo) (activeHotkey, error) {
	mods, err := toModifiers(combo.Mods)
	if err != nil {
		return nil, err
	}
	key, err := toKey(combo.Key)
	if err != nil {
		return nil, err
	}
	hk := hotkey.New(mods, key)
	if err := hk.Register(); err != nil {
		return nil, err
	}
	return winActiveHotkey{hk}, nil
}

type winActiveHotkey struct{ hk *hotkey.Hotkey }

// keydown forwards from the library's channel to our own struct{}-typed
// one (needed only for the type adaptation — hotkey.Event and struct{}
// are shape-identical but distinct types). Buffered and non-blocking: a
// slow consumer (hotkeyManager.watch's trigger()) drops a redundant
// repeat rather than blocking this goroutine, which would otherwise
// stall draining `src` and grow the library's own internal event queue
// (newEventChan's unbounded []Event buffer) without bound. A hotkey
// repeat is a discrete, idempotent-ish signal (show/hide, start/stop,
// toggle compact) — losing one under real backpressure is preferable to
// unbounded memory growth or backpressure into the OS-level hotkey loop.
func (w winActiveHotkey) keydown() <-chan struct{} {
	src := w.hk.Keydown()
	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		for range src {
			select {
			case out <- struct{}{}:
			default:
			}
		}
	}()
	return out
}

func (w winActiveHotkey) unregister() { _ = w.hk.Unregister() }

func toModifiers(mods []hotkeys.Modifier) ([]hotkey.Modifier, error) {
	out := make([]hotkey.Modifier, 0, len(mods))
	for _, m := range mods {
		switch m {
		case hotkeys.ModCtrl:
			out = append(out, hotkey.ModCtrl)
		case hotkeys.ModAlt:
			out = append(out, hotkey.ModAlt)
		case hotkeys.ModShift:
			out = append(out, hotkey.ModShift)
		case hotkeys.ModWin:
			out = append(out, hotkey.ModWin)
		default:
			return nil, fmt.Errorf("hotkeys: unknown modifier %q", m)
		}
	}
	return out, nil
}

// keyByName mirrors exactly the vocabulary internal/hotkeys.ParseCombo
// accepts (verified 1:1 against golang.design/x/hotkey@v0.6.1's own
// Key constants before choosing that vocabulary) — every combo that
// parses here is guaranteed a matching entry.
var keyByName = map[string]hotkey.Key{
	"Space": hotkey.KeySpace, "Return": hotkey.KeyReturn, "Escape": hotkey.KeyEscape,
	"Delete": hotkey.KeyDelete, "Tab": hotkey.KeyTab,
	"Left": hotkey.KeyLeft, "Right": hotkey.KeyRight, "Up": hotkey.KeyUp, "Down": hotkey.KeyDown,
	"A": hotkey.KeyA, "B": hotkey.KeyB, "C": hotkey.KeyC, "D": hotkey.KeyD, "E": hotkey.KeyE,
	"F": hotkey.KeyF, "G": hotkey.KeyG, "H": hotkey.KeyH, "I": hotkey.KeyI, "J": hotkey.KeyJ,
	"K": hotkey.KeyK, "L": hotkey.KeyL, "M": hotkey.KeyM, "N": hotkey.KeyN, "O": hotkey.KeyO,
	"P": hotkey.KeyP, "Q": hotkey.KeyQ, "R": hotkey.KeyR, "S": hotkey.KeyS, "T": hotkey.KeyT,
	"U": hotkey.KeyU, "V": hotkey.KeyV, "W": hotkey.KeyW, "X": hotkey.KeyX, "Y": hotkey.KeyY,
	"Z": hotkey.KeyZ,
	"0": hotkey.Key0, "1": hotkey.Key1, "2": hotkey.Key2, "3": hotkey.Key3, "4": hotkey.Key4,
	"5": hotkey.Key5, "6": hotkey.Key6, "7": hotkey.Key7, "8": hotkey.Key8, "9": hotkey.Key9,
	"F1": hotkey.KeyF1, "F2": hotkey.KeyF2, "F3": hotkey.KeyF3, "F4": hotkey.KeyF4,
	"F5": hotkey.KeyF5, "F6": hotkey.KeyF6, "F7": hotkey.KeyF7, "F8": hotkey.KeyF8,
	"F9": hotkey.KeyF9, "F10": hotkey.KeyF10, "F11": hotkey.KeyF11, "F12": hotkey.KeyF12,
	"F13": hotkey.KeyF13, "F14": hotkey.KeyF14, "F15": hotkey.KeyF15, "F16": hotkey.KeyF16,
	"F17": hotkey.KeyF17, "F18": hotkey.KeyF18, "F19": hotkey.KeyF19, "F20": hotkey.KeyF20,
}

func toKey(name string) (hotkey.Key, error) {
	k, ok := keyByName[name]
	if !ok {
		return 0, fmt.Errorf("hotkeys: unknown key %q", name)
	}
	return k, nil
}
