// Package autostart defines the launch-at-login command line and the
// Store interface cmd/catchm-desktop's Settings toggle uses. The real
// implementation (an HKCU ...\CurrentVersion\Run value) lives behind
// autostart_windows.go/autostart_other.go, build-tagged, so the toggle
// logic itself is testable on any OS against a fake Store.
package autostart

import "fmt"

// CommandLine returns the Run-key value for exePath: a quoted path (so
// spaces in Program Files don't split the command) plus --autostart,
// which main.go checks to start hidden to the tray instead of opening
// the window on every login.
func CommandLine(exePath string) string {
	// Plain double quotes, not Go's %q (which would escape the path's
	// backslashes into an invalid Windows command line).
	return fmt.Sprintf(`"%s" --autostart`, exePath)
}

// Store toggles launch-at-login. SetEnabled(false) must be idempotent
// (removing an already-absent entry is not an error) since Settings
// can call it without first checking IsEnabled.
type Store interface {
	IsEnabled() (bool, error)
	SetEnabled(enabled bool) error
}
