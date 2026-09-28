package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/winbounds"
)

func TestLoadWindowStateMissingFileReturnsEmpty(t *testing.T) {
	s, err := loadWindowState(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("loadWindowState() error = %v, want nil", err)
	}
	if s.Normal != nil || s.Maximized {
		t.Fatalf("loadWindowState() = %+v, want zero value", s)
	}
}

func TestLoadWindowStateCorruptFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window_state.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := loadWindowState(path)
	if err != nil {
		t.Fatalf("loadWindowState() error = %v, want nil (corrupt file should start fresh)", err)
	}
	if s.Normal != nil || s.Maximized {
		t.Fatalf("loadWindowState() = %+v, want zero value", s)
	}
}

func TestSaveAndLoadWindowStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "window_state.json")
	want := &WindowState{Normal: &winbounds.Rect{X: 10, Y: 20, W: 1180, H: 760}, Maximized: true}
	if err := saveWindowState(path, want); err != nil {
		t.Fatalf("saveWindowState() error = %v", err)
	}
	got, err := loadWindowState(path)
	if err != nil {
		t.Fatalf("loadWindowState() error = %v", err)
	}
	if !rectEqual(got.Normal, want.Normal) || got.Maximized != want.Maximized {
		t.Fatalf("loadWindowState() = %+v, want %+v", got, want)
	}
}

func TestRectEqual(t *testing.T) {
	a := &winbounds.Rect{X: 1, Y: 2, W: 3, H: 4}
	b := &winbounds.Rect{X: 1, Y: 2, W: 3, H: 4}
	c := &winbounds.Rect{X: 9, Y: 2, W: 3, H: 4}
	if !rectEqual(a, b) {
		t.Fatal("rectEqual(a, b) = false, want true (equal values)")
	}
	if rectEqual(a, c) {
		t.Fatal("rectEqual(a, c) = true, want false (differing X)")
	}
	if !rectEqual(nil, nil) {
		t.Fatal("rectEqual(nil, nil) = false, want true")
	}
	if rectEqual(a, nil) || rectEqual(nil, a) {
		t.Fatal("rectEqual(x, nil) = true, want false")
	}
}

// DesktopBridge.SetCompactMode is the only signal the frontend has to
// tell the Go side compact mode is active (§2: compact's geometry must
// never overwrite the normal bounds); confirm it actually flips the
// shared flag saveIfChanged reads.
func TestDesktopBridgeSetCompactMode(t *testing.T) {
	defer compactActive.Store(false)
	var d DesktopBridge
	d.SetCompactMode(true)
	if !compactActive.Load() {
		t.Fatal("SetCompactMode(true) did not set compactActive")
	}
	d.SetCompactMode(false)
	if compactActive.Load() {
		t.Fatal("SetCompactMode(false) did not clear compactActive")
	}
}

// Regression test for the quit-time teardown race fixed alongside M23:
// tick must swallow a panic raised by saveIfChanged when the window is
// destroyed mid-tick (the live crash was Wails' WindowGetSize dividing
// by GetDpiForWindow(deadHwnd)=0). We can't call the real saveIfChanged
// without a window, so this exercises tick's guard through the
// panicking seam it wraps: a nil tracker state + a context whose
// cancellation races. The meaningful assertion is simply that tick
// returns instead of taking the process down — pre-fix, poll called
// saveIfChanged directly and any panic in it propagated.
func TestWindowStateTickSurvivesTeardownPanic(t *testing.T) {
	tr := newWindowStateTracker(filepath.Join(t.TempDir(), "window_state.json"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // teardown has begun; tick may still fire once in flight

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Deliberately exercise tick while "cancelled" — the guard
		// under test is the recover, not ctx handling.
		tr.tick(ctx)
	}()
	select {
	case <-done:
		// survived
	case <-time.After(2 * time.Second):
		t.Fatal("tick did not return")
	}
}

func TestUsableSavedBounds(t *testing.T) {
	cases := []struct {
		name string
		r    *winbounds.Rect
		want bool
	}{
		{"nil", nil, false},
		{"minimized icon rect", &winbounds.Rect{X: 1200, Y: 694, W: 160, H: 39}, false},
		{"default window", &winbounds.Rect{X: 100, Y: 100, W: 1180, H: 760}, true},
		{"minimum", &winbounds.Rect{W: minRestoreW, H: minRestoreH}, true},
	}
	for _, c := range cases {
		if got := usableSavedBounds(c.r); got != c.want {
			t.Errorf("%s: usableSavedBounds = %v, want %v", c.name, got, c.want)
		}
	}
}
