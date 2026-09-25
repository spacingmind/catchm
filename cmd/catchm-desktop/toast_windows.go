//go:build windows

package main

import (
	"log"

	"github.com/go-toast/toast"
)

// Build-tagged windows: github.com/go-toast/toast@v0.0.0-20190211's
// own toast.go sets syscall.SysProcAttr{HideWindow: true} with no
// build tag of its own — a Windows-only struct field that fails to
// even COMPILE on any other GOOS (found by trying `go build ./...` on
// this Linux sandbox, not by inspection). toast_other.go stubs notify
// out everywhere else so the rest of the module keeps building.

// notify shows a Windows toast for `title`/`message` when the caller's
// event matters right now, unless the user opted out in Settings. No
// meeting titles in `message` — the same screen-share-privacy rule
// tray.go's tooltip already follows (a toast pops up over whatever's
// on screen, including a shared one).
//
// AppID is deliberately the real AUMID (appID, "com.catchm.desktop"),
// not a "pretty name" — go-toast's own doc comment on Notification
// recommends a pretty name, but reading its actual generated
// PowerShell script (toast.go's toastTemplate) shows $APP_ID is passed
// straight to
// [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier,
// which (for an unpackaged Win32 app) only attributes correctly when
// that string matches the AUMID set on a real Start-menu shortcut
// (build/windows/installer/New-AumidShortcut.ps1, run by project.nsi)
// and via SetCurrentProcessExplicitAppUserModelID at startup
// (aumid_windows.go) — otherwise it falls back to a generic/PowerShell
// identity. Verified by reading the library's source, not its doc
// comment.
func notify(hkManager *hotkeyManager, title, message string) {
	if !hkManager.Settings().NotificationsEnabled {
		return
	}
	n := toast.Notification{
		AppID:   appID,
		Title:   title,
		Message: message,
	}
	if err := n.Push(); err != nil {
		log.Printf("toast: %v", err)
	}
}
