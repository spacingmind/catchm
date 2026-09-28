//go:build !linux && !darwin && !windows

package sysinfo

import "errors"

var errUnsupported = errors.New("sysinfo: unsupported platform")

func totalRAM() (uint64, error)       { return 0, errUnsupported }
func freeDisk(string) (uint64, error) { return 0, errUnsupported }
