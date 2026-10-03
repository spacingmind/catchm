package update

import (
	"os"
	"testing"
)

// TestMain pins the package's tests to the Windows assets, so they run
// the same on any OS (Linux CI has no release assets of its own). Tests
// about another platform pin it themselves with withGOOS.
func TestMain(m *testing.M) {
	HostGOOS = "windows"
	os.Exit(m.Run())
}
