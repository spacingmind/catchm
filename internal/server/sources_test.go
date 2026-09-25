package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/spacingmind/catchm/internal/capture"
)

// A source-open failure surfaces as capture_error while the meeting is
// still created (existing behavior), and audio_source is recorded.
func TestStartMeetingCaptureErrorSurfaces(t *testing.T) {
	s, store := newTestServer(t)
	s.newSource = func(device string) (capture.Source, error) {
		return nil, errors.New("device busy")
	}

	res := do(t, s, http.MethodPost, "/api/meetings",
		`{"title":"No mic","engine":"whisper"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", res.Code, res.Body.String())
	}
	var body struct {
		ID           string `json:"id"`
		AudioSource  string `json:"audio_source"`
		CaptureError string `json:"capture_error"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ID == "" {
		t.Fatal("meeting not created")
	}
	if body.AudioSource != "mic" {
		t.Fatalf("audio_source = %q, want mic (default)", body.AudioSource)
	}
	if !strings.Contains(body.CaptureError, "device busy") {
		t.Fatalf("capture_error = %q, want microphone failure surfaced", body.CaptureError)
	}

	// audio_source persisted in the meeting doc.
	m, err := store.Get(body.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.AudioSource != "mic" {
		t.Fatalf("stored audio_source = %q", m.AudioSource)
	}
}

// The source field validates; a bad value is a 400 before anything.
func TestStartMeetingRejectsBadSource(t *testing.T) {
	s, _ := newTestServer(t)
	res := do(t, s, http.MethodPost, "/api/meetings",
		`{"title":"X","engine":"whisper","source":"teevo"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", res.Code)
	}
}

// Success path: no capture_error key at all.
func TestStartMeetingNoCaptureErrorOnSuccess(t *testing.T) {
	s, _ := newTestServer(t)
	res := do(t, s, http.MethodPost, "/api/meetings",
		`{"title":"Fine","engine":"whisper","source":"mic"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "capture_error") {
		t.Fatalf("capture_error present on success: %s", res.Body.String())
	}
	var body struct {
		AudioSource string `json:"audio_source"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.AudioSource != "mic" {
		t.Fatalf("audio_source = %q", body.AudioSource)
	}
}
