//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows/registry"

	"github.com/spacingmind/catchm/internal/autostart"
)

// autostartKeyPath and autostartValueName: verified against
// golang.org/x/sys/windows/registry's actual API (OpenKey/
// GetStringValue/SetStringValue/DeleteValue, ErrNotExist =
// syscall.ERROR_FILE_NOT_FOUND) before writing the calls below.
const (
	autostartKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	autostartValueName = "CatchM"
)

type winAutostartStore struct{}

func newAutostartStore() autostart.Store { return winAutostartStore{} }

func (winAutostartStore) IsEnabled() (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false, nil // key doesn't exist yet: not enabled, not an error
	}
	defer k.Close()
	if _, _, err := k.GetStringValue(autostartValueName); err != nil {
		return false, nil
	}
	return true, nil
}

func (winAutostartStore) SetEnabled(enabled bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, autostartKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(autostartValueName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(autostartValueName, autostart.CommandLine(exe))
}
