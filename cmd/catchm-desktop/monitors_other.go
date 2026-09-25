//go:build !windows

package main

import "github.com/spacingmind/catchm/internal/winbounds"

// enumMonitors has no non-Windows implementation (M22 is Windows-only
// scope); returning an empty list makes winbounds.Clamp a no-op,
// keeping this build (and go vet/test) working on Linux/macOS.
func enumMonitors() ([]winbounds.MonitorArea, error) {
	return nil, nil
}
