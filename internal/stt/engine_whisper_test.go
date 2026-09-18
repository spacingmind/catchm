package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
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

// TestBatchEngineTranscribesFile feeds a fixture audio.wav through a fake
// whisper.cpp server returning multiple segments with offsets; the engine
// must map them to utterances in order, with At spaced by the offsets.
func TestBatchEngineTranscribesFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inference" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if _, _, err := r.FormFile("file"); err != nil {
			t.Fatalf("missing file field: %v", err)
		}
		if got := r.FormValue("response_format"); got != "verbose_json" {
			t.Fatalf("response_format = %q, want verbose_json", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"segments": []map[string]any{
				{"start": 0.0, "end": 1.2, "text": "hello"},
				{"start": 1.5, "end": 3.0, "text": "there"},
			},
		})
	}))
	defer srv.Close()

	audioPath := filepath.Join(t.TempDir(), "audio.wav")
	w, err := audio.NewWriter(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(make([]byte, 1600)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	engine := NewWhisperEngine(srv.URL)
	us, err := engine.TranscribeFile(context.Background(), audioPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 2 {
		t.Fatalf("len = %d, want 2", len(us))
	}
	if us[0].Text != "hello" || us[0].Seq != 1 {
		t.Fatalf("first utterance = %+v", us[0])
	}
	if us[1].Text != "there" || us[1].Seq != 2 {
		t.Fatalf("second utterance = %+v", us[1])
	}
	if got := us[1].At.Sub(us[0].At); got != 1500*time.Millisecond {
		t.Fatalf("offset delta = %v, want 1.5s", got)
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
