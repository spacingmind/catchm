//go:build windows

// update_windows.go: the elevation + relaunch half of M23. Split from
// update.go so the rest of the orchestration compiles on Linux CI
// (update_other.go).
package main

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// RE-LAUNCH DECISION (the spec offered two options: an NSIS
// .onInstSuccess hook vs. Go-side wait-and-relaunch): Go-side, via
// ShellExecuteExW + WaitForSingleObject in main() after wails.Run
// returns. Chosen because (a) project.nsi is already the most fragile
// file in this build (see its own comments: WinShell plugin
// unavailability, wails_tools.nsh coupling) — adding a lifecycle
// callback there risks breaking every future NSIS bump, while this
// side is one syscall; (b) ShellExecuteEx with SEE_MASK_NOCLOSEPROCESS
// returns an hProcess we can wait on even across the elevation
// boundary, which plain ShellExecuteW (x/sys' only wrapper) cannot.
//
// Ordering matters: the app must exit BEFORE the installer replaces
// files (Windows locks a running exe), so ApplyUpdate only *launches*
// the installer, stores its handle, and quits the UI; the wait and the
// relaunch of the updated exe happen in main() after wails.Run
// returns — the process body stays resident (its on-disk exe may
// already be replaced; the loaded image doesn't care).

const (
	seeMaskNOCloseProcess = 0x00000040
	swShownormal          = 1
)

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    syscall.Handle
	dwHotKey     uint32
	hIconOrMonk  syscall.Handle
	hProcess     syscall.Handle
}

var (
	shellExecuteExProc = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteExW")

	relaunchMu         sync.Mutex
	relaunchHandle     syscall.Handle // 0 = none pending
	errInstallerFailed = errors.New("update: installer exited with an error")
)

// launchInstallerElevated starts the NSIS installer silently (/S) with
// the runas verb (per-machine install under Program Files — the UAC
// prompt is Windows' own). Non-blocking: the handle is kept for
// finishUpdateRelaunch. ERROR_CANCELLED maps to errUACCanceled
// ("update deferred", not an error state).
func launchInstallerElevated(installerPath string) error {
	verbP, _ := syscall.UTF16PtrFromString("runas")
	fileP, _ := syscall.UTF16PtrFromString(installerPath)
	argsP, _ := syscall.UTF16PtrFromString("/S")
	sei := shellExecuteInfo{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:        seeMaskNOCloseProcess,
		lpVerb:       verbP,
		lpFile:       fileP,
		lpParameters: argsP,
		nShow:        swShownormal,
	}
	r1, _, err := shellExecuteExProc.Call(uintptr(unsafe.Pointer(&sei)))
	if r1 == 0 {
		if errors.Is(err, syscall.ERROR_CANCELLED) {
			return errUACCanceled
		}
		return fmt.Errorf("ShellExecuteExW: %w", err)
	}
	if sei.hProcess == 0 {
		return fmt.Errorf("ShellExecuteExW returned no process handle")
	}
	relaunchMu.Lock()
	relaunchHandle = sei.hProcess
	relaunchMu.Unlock()
	return nil
}

// finishUpdateRelaunch runs in main() after wails.Run returns (and the
// tray has quit): waits for the elevated installer to finish, then
// starts the updated exe at the same path the old one ran from
// (project.nsi's $INSTDIR never moves). No-op when no update applied.
func finishUpdateRelaunch() error {
	relaunchMu.Lock()
	h := relaunchHandle
	relaunchMu.Unlock()
	if h == 0 {
		return nil
	}
	defer syscall.CloseHandle(h)

	if s, err := syscall.WaitForSingleObject(h, syscall.INFINITE); err != nil || s == syscall.WAIT_FAILED {
		if err == nil {
			err = fmt.Errorf("WAIT_FAILED")
		}
		return fmt.Errorf("wait for installer: %w", err)
	}
	var exitCode uint32
	if err := syscall.GetExitCodeProcess(h, &exitCode); err != nil {
		return fmt.Errorf("installer exit code: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("%w: code %d", errInstallerFailed, exitCode)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("update relaunch: %w", err)
	}
	proc, err := os.StartProcess(exe, []string{exe}, &os.ProcAttr{})
	if err != nil {
		return fmt.Errorf("update relaunch %s: %w", exe, err)
	}
	return proc.Release()
}
