//go:build !windows

package main

import (
	"errors"

	"github.com/spacingmind/catchm/internal/autostart"
)

type noopAutostartStore struct{}

func newAutostartStore() autostart.Store { return noopAutostartStore{} }

func (noopAutostartStore) IsEnabled() (bool, error) { return false, nil }

func (noopAutostartStore) SetEnabled(bool) error {
	return errors.New("launch at login is only supported on Windows")
}
