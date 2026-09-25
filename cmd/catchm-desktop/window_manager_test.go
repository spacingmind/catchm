package main

import (
	"os"
	"path/filepath"
	"testing"

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
