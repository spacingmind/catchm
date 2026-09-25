//go:build !windows

package main

import (
	"errors"

	"github.com/spacingmind/catchm/internal/hotkeys"
)

// M22 is Windows-only scope (see docs/plans/active/m22-native-shell.md's
// Out of scope). newPlatformHotkeys still needs a real implementation
// on every OS this package's plain `go build`/`go test` runs on
// (Linux/macOS CI, this sandbox) — register always fails cleanly
// rather than pretending to succeed.
type noopHotkeys struct{}

func newPlatformHotkeys() platformHotkeys { return noopHotkeys{} }

func (noopHotkeys) register(hotkeys.Combo) (activeHotkey, error) {
	return nil, errors.New("hotkeys: global shortcuts are only supported on Windows")
}
