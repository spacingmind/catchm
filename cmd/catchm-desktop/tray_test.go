package main

import (
	"path/filepath"
	"testing"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/server"
	"github.com/spacingmind/catchm/internal/tools"
)

// newTestHandler builds a real internal/server.Server (the same
// handler catchm-desktop's window and tray both use in production),
// backed by a tempdir store. cfg.Engine is left empty so
// startMeeting's meetings are title-only (no live capture, no audio
// backend needed) — enough to exercise the tray's HTTP round trips
// without a real STT engine.
func newTestHandler(t *testing.T) (*server.Server, *config.Config) {
	t.Helper()
	store, err := meeting.NewStore(filepath.Join(t.TempDir(), "meetings"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	return server.New(tools.New(store), cfg), cfg
}

func TestActiveMeetingNoneIsIdle(t *testing.T) {
	handler, _ := newTestHandler(t)
	id, active, err := activeMeeting(handler)
	if err != nil {
		t.Fatalf("activeMeeting() error = %v", err)
	}
	if active || id != "" {
		t.Fatalf("activeMeeting() = (%q, %v), want (\"\", false)", id, active)
	}
}

func TestCurrentTrayStateNoMeetingsIsIdle(t *testing.T) {
	handler, _ := newTestHandler(t)
	s := currentTrayState(handler)
	if s.label != "idle" {
		t.Fatalf("currentTrayState().label = %q, want %q", s.label, "idle")
	}
}

// startMeeting creates a meeting the same way the tray's "Start
// meeting" click does; activeMeeting must then find it, and endMeeting
// must clear it back to idle — the exact cycle the tray menu item
// drives.
func TestStartMeetingThenEndMeetingRoundTrip(t *testing.T) {
	handler, cfg := newTestHandler(t)

	startMeeting(handler, cfg)
	id, active, err := activeMeeting(handler)
	if err != nil {
		t.Fatalf("activeMeeting() error = %v", err)
	}
	if !active || id == "" {
		t.Fatalf("activeMeeting() after startMeeting = (%q, %v), want (non-empty, true)", id, active)
	}
	if s := currentTrayState(handler); s.label != "recording" {
		t.Fatalf("currentTrayState() after startMeeting = %q, want %q", s.label, "recording")
	}

	endMeeting(handler, id)
	id2, active2, err := activeMeeting(handler)
	if err != nil {
		t.Fatalf("activeMeeting() error = %v", err)
	}
	if active2 || id2 != "" {
		t.Fatalf("activeMeeting() after endMeeting = (%q, %v), want (\"\", false)", id2, active2)
	}
}

// onTrayStartStopClick is the single click handler for the tray's
// start/stop item: idle -> starts, active -> ends. Exercises it
// directly (not just its two halves) since that's what's actually
// wired to the menu item's Click callback.
func TestOnTrayStartStopClickTogglesMeeting(t *testing.T) {
	handler, cfg := newTestHandler(t)

	onTrayStartStopClick(handler, cfg)
	if _, active, _ := activeMeeting(handler); !active {
		t.Fatal("onTrayStartStopClick() from idle did not start a meeting")
	}

	onTrayStartStopClick(handler, cfg)
	if _, active, _ := activeMeeting(handler); active {
		t.Fatal("onTrayStartStopClick() from active did not end the meeting")
	}
}

func TestEngineStateNoPipelineIsNotOK(t *testing.T) {
	handler, _ := newTestHandler(t)
	_, _, ok := engineState(handler, "does-not-exist")
	if ok {
		t.Fatal("engineState() for a nonexistent meeting = ok, want !ok")
	}
}
