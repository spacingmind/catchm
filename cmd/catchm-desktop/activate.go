package main

import (
	"context"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// frontendReady flips true when the frontend calls
// DesktopBridge.ReadyForEvents (its first act after subscribing to
// openMeetingEvent) — the handshake that makes cold-start protocol
// activation deliverable (see handleLaunchURI).
var frontendReady atomic.Bool

// URI scheme the NSIS installer registers (wails.json's info.protocols,
// written under HKCU\Software\Classes by wails_tools.nsh's
// wails.associateCustomProtocols). Toasts launch catchm://meeting/<id>
// (activationType="protocol"), Windows resolves the scheme to this exe,
// and the running (single) instance navigates to that meeting.
const meetingURIScheme = "catchm"

// openMeetingEvent is the Wails event emitted when a catchm://meeting/<id>
// activation arrives (cold start or already-running); web/src/App.tsx
// subscribes to this exact name via windowEventsOn and navigates to the
// meeting.
const openMeetingEvent = "app:open-meeting"

// parseMeetingURI extracts the meeting id from a catchm://meeting/<id>
// activation argument. Returns "" for anything else (no arg, another
// catchm:// path, or an id that can't be a meeting id — ids are
// hex-encoded newID output, so this stays strict rather than passing
// arbitrary text into the app). Windows passes the raw URI as the %1
// argument. Deliberately prefix-based, not net/url: catchm: is not one
// of net/url's "special" schemes, so url.Parse puts "meeting" in Host
// and the id in Path — the authority/path split it applies to http://
// doesn't hold for a custom scheme.
func parseMeetingURI(arg string) string {
	prefix := meetingURIScheme + "://meeting/"
	s := strings.TrimSpace(arg)
	s = strings.TrimSuffix(s, "/") // tolerate a trailing slash
	if !strings.HasPrefix(s, prefix) {
		return ""
	}
	id := strings.TrimPrefix(s, prefix)
	if !validMeetingID(id) {
		return ""
	}
	return id
}

// validMeetingID mirrors internal/meeting's id shape (hex newID output)
// without importing it: this lives in the desktop shell, which only
// needs to reject anything that can't be an id before handing it to the
// API/UI, where non-existent ids are an ordinary not-found case.
func validMeetingID(id string) bool {
	if len(id) == 0 || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

// meetingArgFromArgs returns the first catchm:// argument in args (os.Args
// shape: [exe, ...]); "" when none.
func meetingArgFromArgs(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, meetingURIScheme+"://") {
			return a
		}
	}
	return ""
}

// handleLaunchURI delivers a cold-start catchm://meeting/<id> to the
// frontend. OnStartup can fire before the webview has executed any JS,
// and Wails' EventsEmit has no "wait for listeners" semantics (an event
// emitted into the void is dropped — the same structural gap as the
// tray-toggle event, but worse here because the activation arg exists
// exactly once), so poll: the frontend announces readiness by calling
// DesktopBridge.ReadyForEvents (below) as soon as its App:open-meeting
// subscription is in place, and a short overall timeout bounds the wait
// for a webview that never comes up (crash, GPU block) — losing a toast
// click is recoverable (the meeting is still in the list), hanging a
// goroutine forever is not.
func handleLaunchURI(ctx context.Context, meetingID string) {
	const timeout = 15 * time.Second
	deadline := time.Now().Add(timeout)
	for !frontendReady.Load() {
		if time.Now().After(deadline) {
			log.Printf("launch uri: frontend not ready within %s, dropping meeting %s", timeout, meetingID)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	runtime.EventsEmit(ctx, openMeetingEvent, meetingID)
}
