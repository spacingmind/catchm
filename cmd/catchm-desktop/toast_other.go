//go:build !windows

package main

// notify is a no-op outside Windows — see toast_windows.go for why
// github.com/go-toast/toast can only be imported from a
// windows-build-tagged file at all.
func notify(*hotkeyManager, string, string, string) {}
