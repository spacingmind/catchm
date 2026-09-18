package capture

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
)

func writeFixtureWAV(t *testing.T, samples []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.wav")
	w, err := audio.NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(samples); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFileSourceReplaysChunks(t *testing.T) {
	samples := make([]byte, 100)
	for i := range samples {
		samples[i] = byte(i)
	}
	path := writeFixtureWAV(t, samples)

	src, err := NewFileSource(path, 30)
	if err != nil {
		t.Fatal(err)
	}

	var got []byte
loop:
	for {
		select {
		case chunk, ok := <-src.Chunks():
			if !ok {
				break loop
			}
			got = append(got, chunk...)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for replay to finish")
		}
	}
	if string(got) != string(samples) {
		t.Fatalf("replayed %d bytes, want %d", len(got), len(samples))
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileSourceCloseStopsEarly(t *testing.T) {
	samples := make([]byte, 100)
	path := writeFixtureWAV(t, samples)

	src, err := NewFileSource(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-src.Chunks():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first chunk")
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	// Channel must end up closed even though replay was cut short.
	select {
	case _, ok := <-src.Chunks():
		if ok {
			// Draining leftover buffered chunk is fine; keep draining until closed.
			for {
				if _, ok := <-src.Chunks(); !ok {
					break
				}
			}
		}
	case <-time.After(time.Second):
		t.Fatal("channel never closed after Close")
	}
}

func TestDevicesEnumerates(t *testing.T) {
	devices, err := Devices()
	if err != nil {
		t.Skipf("no audio backend available in this environment: %v", err)
	}
	t.Logf("found %d capture device(s)", len(devices))
}
