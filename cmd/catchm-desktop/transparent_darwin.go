//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#include <AppKit/AppKit.h>

static void setWindowTransparent(BOOL on) {
	dispatch_async(dispatch_get_main_queue(), ^{
		for (NSWindow *w in [NSApp windows]) {
			if (on) {
				[w setOpaque:NO];
				[w setBackgroundColor:[NSColor clearColor]];
			} else {
				[w setOpaque:YES];
			}
		}
	});
}
*/
import "C"

// Wails v2.16 never calls NSWindow setOpaque:NO (grep
// internal/frontend/desktop/darwin), so Mac.WebviewIsTransparent plus
// an alpha-0 background colour still shows an opaque window: macOS
// ignores the colour's alpha while the window is opaque (verified by
// experiment — see docs/plans/completed/compact-transparent.md). This
// shim is what actually makes the compact overlay see-through.

// setWindowTransparent toggles the app window's see-through state on
// the main queue (AppKit requirement): on makes the NSWindow
// non-opaque with a clear background so the content behind shows
// through; off restores opacity (the page then re-syncs the window
// colour via lib/theme.ts).
func setWindowTransparent(on bool) {
	C.setWindowTransparent(C.bool(on))
}

func transparentSupported() bool { return true }
