//go:build !windows

// update_other.go: non-Windows stubs so update.go compiles on Linux CI
// (M23's auto-update is Windows-only scope; the update Settings UI is
// hidden outside the Wails window anyway).
package main

import "errors"

func launchUpdateBabysitter(installerPath string) error {
	return errors.New("update: not supported on this OS")
}

func finishUpdateRelaunch() error { return nil }
