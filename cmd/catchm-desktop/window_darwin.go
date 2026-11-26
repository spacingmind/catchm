//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#include <AppKit/AppKit.h>

static BOOL appHiddenOrMinimised(void) {
	__block BOOL hidden = NO;
	void (^check)(void) = ^{
		if ([NSApp isHidden]) { hidden = YES; return; }
		NSWindow *w = [NSApp mainWindow] ?: [[NSApp windows] firstObject];
		hidden = (w == nil) || [w isMiniaturized] || ![w isVisible];
	};
	if ([NSThread isMainThread]) check(); else dispatch_sync(dispatch_get_main_queue(), check);
	return hidden;
}
*/
import "C"

// appHiddenOrMinimised reports whether CatchM is out of sight: app
// hidden (close button / Cmd+H) or its window minimised or ordered
// out. Read on the main thread, as AppKit requires.
func appHiddenOrMinimised() bool { return bool(C.appHiddenOrMinimised()) }
