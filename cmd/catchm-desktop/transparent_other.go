//go:build !darwin

package main

// Non-macOS: see-through compact mode is unimplemented (see
// docs/plans/active/compact-transparent.md — Windows Mica only tints
// with the wallpaper), so transparentSupported reports false and
// setWindowTransparent is a no-op.

func setWindowTransparent(bool) {}

func transparentSupported() bool { return false }
