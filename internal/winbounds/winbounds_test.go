package winbounds

import "testing"

var singleMonitor = []MonitorArea{
	{WorkArea: Rect{X: 0, Y: 0, W: 1920, H: 1040}, Primary: true},
}

func TestClampLeavesOnScreenRectUnchanged(t *testing.T) {
	saved := Rect{X: 100, Y: 100, W: 1180, H: 760}
	got := Clamp(saved, singleMonitor)
	if got != saved {
		t.Fatalf("Clamp(%+v) = %+v, want unchanged", saved, got)
	}
}

// A rect whose title-bar corner still overlaps the monitor by exactly
// the minimum is accepted as-is, even though most of the window is
// off-screen — dragging it back into view is still possible.
func TestClampAcceptsMinimalOverlap(t *testing.T) {
	saved := Rect{X: 1920 - MinVisibleTitleWidth, Y: 1040 - MinVisibleTitleHeight, W: 1180, H: 760}
	got := Clamp(saved, singleMonitor)
	if got != saved {
		t.Fatalf("Clamp(%+v) = %+v, want unchanged (exactly at the minimum)", saved, got)
	}
}

// Deliberately off-screen (per the spec's acceptance criteria): a
// saved position entirely to the right of every monitor must be
// clamped back, centered on the primary monitor.
func TestClampCentersFullyOffscreenRect(t *testing.T) {
	saved := Rect{X: 5000, Y: 5000, W: 1180, H: 760}
	got := Clamp(saved, singleMonitor)
	want := Rect{X: (1920 - 1180) / 2, Y: (1040 - 760) / 2, W: 1180, H: 760}
	if got != want {
		t.Fatalf("Clamp(%+v) = %+v, want %+v", saved, got, want)
	}
}

// Negative coordinates are valid (a monitor to the left/above the
// primary's origin) and must not be treated as automatically
// off-screen.
func TestClampAcceptsNegativeCoordinatesOnSecondaryMonitor(t *testing.T) {
	monitors := []MonitorArea{
		{WorkArea: Rect{X: 0, Y: 0, W: 1920, H: 1040}, Primary: true},
		{WorkArea: Rect{X: -1920, Y: 0, W: 1920, H: 1040}, Primary: false},
	}
	saved := Rect{X: -1800, Y: 100, W: 1180, H: 760}
	got := Clamp(saved, monitors)
	if got != saved {
		t.Fatalf("Clamp(%+v) = %+v, want unchanged", saved, got)
	}
}

// Monitor topology changed (the saved monitor is gone, e.g. an
// external display unplugged): falls back to centered on whatever
// monitor is now primary, not left stranded off the remaining screen.
func TestClampFallsBackWhenMonitorTopologyChanged(t *testing.T) {
	saved := Rect{X: -1800, Y: 100, W: 1180, H: 760} // was valid on a second monitor that's now gone
	got := Clamp(saved, singleMonitor)
	want := Rect{X: (1920 - 1180) / 2, Y: (1040 - 760) / 2, W: 1180, H: 760}
	if got != want {
		t.Fatalf("Clamp(%+v) = %+v, want %+v (centered fallback)", saved, got, want)
	}
}

// A saved size larger than the (now smaller) monitor's work area
// shrinks to fit rather than overflowing it.
func TestClampShrinksOversizedRectToFitFallback(t *testing.T) {
	small := []MonitorArea{{WorkArea: Rect{X: 0, Y: 0, W: 800, H: 600}, Primary: true}}
	saved := Rect{X: 5000, Y: 5000, W: 1920, H: 1040}
	got := Clamp(saved, small)
	if got.W != 800 || got.H != 600 || got.X != 0 || got.Y != 0 {
		t.Fatalf("Clamp(%+v) = %+v, want shrunk to fill 800x600 at origin", saved, got)
	}
}

// No monitors enumerated (native lookup failed): nothing to clamp
// against, so the saved rect passes through untouched rather than
// being discarded.
func TestClampNoMonitorsPassesThrough(t *testing.T) {
	saved := Rect{X: 100, Y: 100, W: 1180, H: 760}
	got := Clamp(saved, nil)
	if got != saved {
		t.Fatalf("Clamp(%+v, nil) = %+v, want unchanged", saved, got)
	}
}

// Prefers the monitor actually marked primary over just the first in
// the slice, when the saved rect needs a fallback.
func TestClampFallbackPrefersPrimaryNotFirst(t *testing.T) {
	monitors := []MonitorArea{
		{WorkArea: Rect{X: -1920, Y: 0, W: 1920, H: 1040}, Primary: false},
		{WorkArea: Rect{X: 0, Y: 0, W: 1920, H: 1040}, Primary: true},
	}
	saved := Rect{X: 9000, Y: 9000, W: 1180, H: 760}
	got := Clamp(saved, monitors)
	want := Rect{X: (1920 - 1180) / 2, Y: (1040 - 760) / 2, W: 1180, H: 760}
	if got != want {
		t.Fatalf("Clamp(%+v) = %+v, want %+v (primary, not first)", saved, got, want)
	}
}

// Just short of the minimum visible corner is rejected — one pixel
// less than MinVisibleTitleWidth must not be accepted as "visible".
func TestClampRejectsJustBelowMinimumOverlap(t *testing.T) {
	saved := Rect{X: 1920 - MinVisibleTitleWidth + 1, Y: 100, W: 1180, H: 760}
	got := Clamp(saved, singleMonitor)
	if got == saved {
		t.Fatalf("Clamp(%+v) = unchanged, want fallback (below the minimum overlap)", saved)
	}
}
