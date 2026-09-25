//go:build windows

// update_windows.go: the elevation + relaunch half of M23. Split from
// update.go so the rest of the orchestration compiles on Linux CI
// (update_other.go).
package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// RE-LAUNCH DESIGN (the spec offered two options: an NSIS
// .onInstSuccess hook vs. Go-side handling): Go-side, via an elevated
// PowerShell "babysitter". project.nsi is the most macro-fragile file
// in this build (see its own comments) and a .onInstSuccess hook
// there risks every future NSIS bump; the babysitter is one
// ShellExecuteEx call.
//
// Why a babysitter process at all (live-tested on real Windows, M23):
// NSIS /S silently fails to replace an exe that is still running and
// STILL EXITS 0 — an earlier design where the app itself launched the
// installer, quit the UI, then waited in-process (after wails.Run
// returned) produced exactly that: exit 0, old file untouched, because
// the waiting process WAS the lock. The waiter therefore must not be
// the process being replaced. The babysitter:
//
//	1. waits for our pid to exit (exe lock released)
//	2. runs the verified installer silently — already elevated, so no
//	   second UAC prompt
//	3. relaunches the updated exe UNELEVATED via explorer.exe (running
//	   the app itself elevated just because the updater was would be
//	   wrong; explorer.exe is the standard unelevated broker)
//
// One UAC prompt total, at babysitter launch — the same single prompt
// the previous design had.

const (
	seeMaskNOCloseProcess = 0x00000040
	swShownormal          = 1
	// HideWindow: the babysitter powershell never shows a console.
	swHide = 0
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

var shellExecuteExProc = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteExW")

// errWinCancelled is ERROR_CANCELLED (1223): stdlib syscall has no
// such constant, so the value is spelled out directly.
const errWinCancelled = syscall.Errno(1223)

// babysitterScript waits for the old pid to die, runs the installer
// (/S), and relaunches the updated exe unelevated via explorer.exe.
// Values arrive via environment variables inherited from this process
// (same mechanism project.nsi uses for New-AumidShortcut.ps1 — never
// interpolated into the command line, so no quoting/escaping surface).
const babysitterScript = `
$ErrorActionPreference = 'Stop'
try { Wait-Process -Id $env:CATCHM_UPDATE_OLD_PID -ErrorAction SilentlyContinue } catch {}
Start-Sleep -Milliseconds 800   # let file handles drain after process exit
$p = Start-Process $env:CATCHM_UPDATE_INSTALLER -ArgumentList '/S' -PassThru -Wait
if ($p.ExitCode -ne 0) { exit 2 }
Start-Process explorer.exe $env:CATCHM_UPDATE_EXE
exit 0
`

// encodePowerShell builds a powershell -EncodedCommand payload
// (UTF-16LE + base64 — the encoding PowerShell's -EncodedCommand
// flag is specified to accept), keeping the script off the command
// line entirely.
func encodePowerShell(script string) string {
	var u16 []uint16
	for _, r := range script {
		u16 = append(u16, uint16(r))
	}
	b := make([]byte, len(u16)*2)
	for i, v := range u16 {
		b[i*2] = byte(v)
		b[i*2+1] = byte(v >> 8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// launchUpdateBabysitter starts the elevated babysitter (single UAC
// prompt). ERROR_CANCELLED maps to errUACCanceled ("update deferred",
// not an error state). Returns once the babysitter has been handed to
// the elevated broker — it does not wait for it.
func launchUpdateBabysitter(installerPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("update relaunch: %w", err)
	}
	// Env for the babysitter; inherited at ShellExecuteEx's
	// CreateProcess (parent env copied synchronously at call time).
	// Cleaned up immediately after launch either way — the child has
	// its own copy.
	os.Setenv("CATCHM_UPDATE_OLD_PID", fmt.Sprint(os.Getpid()))
	os.Setenv("CATCHM_UPDATE_INSTALLER", installerPath)
	os.Setenv("CATCHM_UPDATE_EXE", exe)
	defer func() {
		os.Unsetenv("CATCHM_UPDATE_OLD_PID")
		os.Unsetenv("CATCHM_UPDATE_INSTALLER")
		os.Unsetenv("CATCHM_UPDATE_EXE")
	}()

	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(os.Getenv("windir") + `\System32\WindowsPowerShell\v1.0\powershell.exe`)
	args, _ := syscall.UTF16PtrFromString("-NoProfile -NonInteractive -WindowStyle Hidden -EncodedCommand " + encodePowerShell(babysitterScript))
	sei := shellExecuteInfo{
		cbSize:       uint32(unsafe.Sizeof(shellExecuteInfo{})),
		fMask:        seeMaskNOCloseProcess,
		lpVerb:       verb,
		lpFile:       file,
		lpParameters: args,
		nShow:        swHide,
	}
	r1, _, err := shellExecuteExProc.Call(uintptr(unsafe.Pointer(&sei)))
	if r1 == 0 {
		if errors.Is(err, errWinCancelled) {
			return errUACCanceled
		}
		return fmt.Errorf("ShellExecuteExW: %w", err)
	}
	if sei.hProcess != 0 {
		syscall.CloseHandle(sei.hProcess) // we never wait on it — it waits on us
	}
	return nil
}

// finishUpdateRelaunch is kept (as a no-op) so main.go's shutdown
// sequence reads the same on all platforms; the real relaunch work
// lives in the babysitter launched by ApplyUpdate.
func finishUpdateRelaunch() error { return nil }
