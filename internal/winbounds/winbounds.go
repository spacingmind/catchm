// Package winbounds normalizes and clamps a persisted window rect
// against the monitor topology it's about to be restored into. It has
// no platform dependency and no I/O — the desktop binary supplies the
// monitor list (native, Windows-only enumeration — see
// cmd/catchm-desktop/monitors_windows.go, since Wails v2.16's
// runtime.ScreenGetAll reports only monitor SIZE, never position or
// work area, verified against internal/frontend/desktop/windows/
// screen.go in the module source) and applies the result.
package winbounds

// Rect is a rectangle in absolute virtual-screen coordinates (the same
// space Wails' WindowGetPosition/WindowGetSize report in).
type Rect struct {
	X, Y, W, H int
}

// MonitorArea is one display's work area (excludes taskbar/reserved
// regions), in absolute virtual-screen coordinates.
type MonitorArea struct {
	WorkArea Rect
	Primary  bool
}

// MinVisibleTitleWidth and MinVisibleTitleHeight are the minimum slice
// of a window's top-left corner (its title bar, where the OS lets a
// user grab and drag it back into view) that must land inside some
// monitor's work area for a saved rect to be accepted as-is.
const (
	MinVisibleTitleWidth  = 100
	MinVisibleTitleHeight = 40
)

// Clamp returns saved unchanged if its title-bar corner is already
// visible on some monitor (a monitor may have moved slightly, e.g. a
// resolution change, without invalidating the saved position). If no
// monitor shows enough of it — the window was dragged off-screen, or
// the monitor it was on is gone — it falls back to centered on the
// primary monitor (or the first one, if none is marked primary). With
// no monitors at all (enumeration failed), saved is returned unchanged
// since there's nothing to clamp against.
func Clamp(saved Rect, monitors []MonitorArea) Rect {
	if len(monitors) == 0 {
		return saved
	}
	for _, m := range monitors {
		if titleBarVisible(saved, m.WorkArea) {
			return saved
		}
	}
	return centerOn(saved, primaryOrFirst(monitors).WorkArea)
}

// titleBarVisible reports whether at least MinVisibleTitleWidth x
// MinVisibleTitleHeight of r's top-left corner overlaps work.
func titleBarVisible(r Rect, work Rect) bool {
	overlapW := overlap(r.X, r.X+r.W, work.X, work.X+work.W)
	overlapH := overlap(r.Y, r.Y+MinVisibleTitleHeight, work.Y, work.Y+work.H)
	return overlapW >= MinVisibleTitleWidth && overlapH >= MinVisibleTitleHeight
}

func overlap(aStart, aEnd, bStart, bEnd int) int {
	start := max(aStart, bStart)
	end := min(aEnd, bEnd)
	if end <= start {
		return 0
	}
	return end - start
}

// centerOn returns r's size (shrunk to fit if it's larger than work),
// centered within work.
func centerOn(r Rect, work Rect) Rect {
	w, h := r.W, r.H
	if w > work.W {
		w = work.W
	}
	if h > work.H {
		h = work.H
	}
	return Rect{
		X: work.X + (work.W-w)/2,
		Y: work.Y + (work.H-h)/2,
		W: w,
		H: h,
	}
}

func primaryOrFirst(monitors []MonitorArea) MonitorArea {
	for _, m := range monitors {
		if m.Primary {
			return m
		}
	}
	return monitors[0]
}
