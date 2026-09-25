//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/spacingmind/catchm/internal/winbounds"
)

// monitorInfoFPrimary is MONITORINFOF_PRIMARY (winuser.h).
const monitorInfoFPrimary = 0x1

// win32MonitorInfo mirrors the win32 MONITORINFO struct layout exactly
// (winuser.h) — same fields Wails' own internal/frontend/desktop/
// windows/screen.go reads via its vendored w32.MONITORINFO, which this
// package can't import (it's an internal package of a different
// module). golang.org/x/sys/windows (already a transitive dependency
// here, via Wails' own win32/theme.go importing its registry
// subpackage) has no GetMonitorInfoW/EnumDisplayMonitors wrapper, so
// this calls user32.dll directly, the same two functions Wails calls
// internally — verified by reading that file, not guessed.
type win32MonitorInfo struct {
	CbSize    uint32
	RcMonitor windows.Rect
	RcWork    windows.Rect
	DwFlags   uint32
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
)

// enumMonitors lists every display's work area in absolute
// virtual-screen coordinates — the piece runtime.ScreenGetAll doesn't
// expose (see winbounds package doc).
func enumMonitors() ([]winbounds.MonitorArea, error) {
	var monitors []winbounds.MonitorArea
	cb := syscall.NewCallback(func(hMonitor uintptr, _ uintptr, _ *windows.Rect, _ uintptr) uintptr {
		var info win32MonitorInfo
		info.CbSize = uint32(unsafe.Sizeof(info))
		ret, _, _ := procGetMonitorInfoW.Call(hMonitor, uintptr(unsafe.Pointer(&info)))
		if ret != 0 {
			monitors = append(monitors, winbounds.MonitorArea{
				WorkArea: winbounds.Rect{
					X: int(info.RcWork.Left),
					Y: int(info.RcWork.Top),
					W: int(info.RcWork.Right - info.RcWork.Left),
					H: int(info.RcWork.Bottom - info.RcWork.Top),
				},
				Primary: info.DwFlags&monitorInfoFPrimary != 0,
			})
		}
		return 1 // non-zero: continue enumeration
	})
	ret, _, err := procEnumDisplayMonitors.Call(0, 0, cb, 0)
	if ret == 0 {
		return nil, err
	}
	return monitors, nil
}
