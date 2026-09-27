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
	"path/filepath"
	"strings"
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
//	   second UAC prompt — with /D set to the running exe's
//	   directory, so the update always installs OVER the running copy
//	3. relaunches that same exe path UNELEVATED via explorer.exe
//	   (running the app itself elevated just because the updater was
//	   would be wrong; explorer.exe is the standard unelevated broker)
//
// /D matters because NSIS otherwise installs to the installer's own
// default ($PROGRAMFILES64\SpacingMind\CatchM from project.nsi's
// InstallDir), which is only correct if the running copy happens to
// live there. It really didn't, once: v0.1.0 was installed under
// Program Files\CatchM\CatchM (old company name), so updating to
// v0.1.2 installed a SECOND copy under Program Files\SpacingMind\
// CatchM and the babysitter relaunched the stale v0.1.0 exe. /D
// removes the divergence by construction — the new version lands
// exactly where the old one ran, and the relaunch path is right
// without any registry lookup. Per NSIS's command-line rules /D must
// be the LAST argument, followed by the path UNQUOTED even when it
// contains spaces (Program Files) — NSIS re-joins the tail of the
// command line itself. /D has no effect on per-machine vs per-user
// scope: that is decided by project.nsi's compile-time
// REQUEST_EXECUTION_LEVEL (the wails.setShellContext macro reads that
// define, not $INSTDIR), and $INSTDIR is used as-is wherever the
// directory page is skipped (/S), so shortcuts, the uninstaller, and
// the WebView2/AUMID steps all follow the /D directory.
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

// installerArgs builds the argument list the babysitter passes to the
// NSIS installer: /S for silent, then /D=<dir> pinned to the running
// exe's directory so the update installs over it (see the design
// comment above). Per NSIS rules /D must be LAST and its path
// UNQUOTED — Start-Process -ArgumentList keeps the string verbatim,
// and NSIS re-joins everything after /D= up to end-of-line as the
// path, spaces included. filepath.Dir normalizes trailing slashes so
// "/D=C:\" (which NSIS would read as an escaped quote mid-command in
// some parsing contexts) never forms.
func installerArgs(installDir string) []string {
	return []string{"/S", "/D=" + filepath.Clean(installDir)}
}

// babysitterScript waits for the old pid to die, runs the installer
// (args from installerArgs), and relaunches the updated exe
// unelevated via explorer.exe. The three values (old pid, installer
// path, exe path) are formatted into the script BEFORE
// -EncodedCommand encoding — live-tested finding: env vars do NOT
// survive the runas elevation boundary (the broker builds a fresh
// environment), so $env: passing silently delivered $null and the
// babysitter died doing nothing. Paths are base64-encoded into the
// script body (never string-interpolated into a PowerShell
// expression) so no quoting/escaping surface exists.
func babysitterScript(oldPID int, installerPath, exePath string) string {
	return `
$ErrorActionPreference = 'Stop'
$oldPid = ` + fmt.Sprint(oldPID) + `
$installer = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + base64.StdEncoding.EncodeToString([]byte(installerPath)) + `'))
$exe = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + base64.StdEncoding.EncodeToString([]byte(exePath)) + `'))
try { Wait-Process -Id $oldPid -ErrorAction SilentlyContinue } catch {}
Start-Sleep -Milliseconds 800   # let file handles drain after process exit
$p = Start-Process $installer -ArgumentList '` + strings.Join(installerArgs(filepath.Dir(exePath)), "', '") + `' -PassThru -Wait
if ($p.ExitCode -ne 0) { exit 2 }
Start-Process explorer.exe $exe
exit 0
`
}

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
// the elevated broker — it does not wait for it (it waits for us).
func launchUpdateBabysitter(installerPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("update relaunch: %w", err)
	}
	powershell := os.Getenv("windir") + `\System32\WindowsPowerShell\v1.0\powershell.exe`
	cmdline := "-NoProfile -NonInteractive -WindowStyle Hidden -EncodedCommand " +
		encodePowerShell(babysitterScript(os.Getpid(), installerPath, exe))

	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(powershell)
	args, _ := syscall.UTF16PtrFromString(cmdline)
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
