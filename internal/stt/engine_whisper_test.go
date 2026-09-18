package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestWhisperEngineMapping feeds two chunks through a fake whisper.cpp
// server that answers partial then final; the engine must surface both,
// with only the second one marked Final.
func TestWhisperEngineMapping(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/inference" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if _, _, err := r.FormFile("file"); err != nil {
			t.Fatalf("missing file field: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			json.NewEncoder(w).Encode(map[string]any{"text": "hel", "partial": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"text": "hello world"})
	}))
	defer srv.Close()

	engine := NewWhisperEngine(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pcmCh, resultCh, err := engine.Stream(ctx, Options{})
	if err != nil {
		t.Fatal(err)
	}

	pcmCh <- PCM{1, 2, 3, 4}
	first := recvResult(t, resultCh)
	if first.Final || first.Text != "hel" {
		t.Fatalf("first result = %+v, want partial %q", first, "hel")
	}

	pcmCh <- PCM{5, 6, 7, 8}
	second := recvResult(t, resultCh)
	if !second.Final || second.Text != "hello world" {
		t.Fatalf("second result = %+v, want final %q", second, "hello world")
	}

	close(pcmCh)
	select {
	case _, ok := <-resultCh:
		if ok {
			t.Fatal("expected result channel to close after pcm channel closes")
		}
	case <-time.After(time.Second):
		t.Fatal("result channel did not close")
	}

	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func recvResult(t *testing.T, ch <-chan Result) Result {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for result")
		return Result{}
	}
}

func TestWhisperEngineCapabilities(t *testing.T) {
	e := NewWhisperEngine("http://example.invalid")
	if e.SupportsDiarization() {
		t.Fatal("whisper engine should not claim diarization support")
	}
	if e.SupportsTranslation() {
		t.Fatal("whisper engine should not claim translation support")
	}
}
