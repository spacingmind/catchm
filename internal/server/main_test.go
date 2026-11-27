package server

import (
	"context"
	"os"
	"testing"
)

// TestMain keeps the package's tests offline: the Have I Been Pwned
// lookup answers "not breached" unless a test swaps it.
func TestMain(m *testing.M) {
	passwordBreachCount = func(context.Context, string) (int, error) { return 0, nil }
	os.Exit(m.Run())
}
