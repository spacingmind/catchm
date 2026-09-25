//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// setProcessAUMID calls shell32's SetCurrentProcessExplicitAppUserModelID
// (not wrapped by golang.org/x/sys/windows, so called directly —
// https://learn.microsoft.com/windows/win32/api/shobjidl_core/nf-shobjidl_core-setcurrentprocessexplicitappusermodelid).
// M22 §5: toast notifications only attribute to this app (rather than
// falling back to a generic/PowerShell identity) when the process AUMID
// matches the one set on the Start-menu shortcut the NSIS installer
// creates (build/windows/installer/project.nsi's WinShell::SetLnkAUMI)
// — both must use the exact same appID string.
func setProcessAUMID(appID string) {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	proc := shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	ptr, err := windows.UTF16PtrFromString(appID)
	if err != nil {
		return
	}
	_, _, _ = proc.Call(uintptr(unsafe.Pointer(ptr)))
}
