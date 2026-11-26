//go:build darwin

package capture

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
)

// Hermetic: runs in CI on macos-latest (M25 criterion 9).
func TestSystemAudioSupportedDarwin(t *testing.T) {
	ok, reason := SystemAudioSupported()
	if !ok && reason == "" {
		t.Fatal("unsupported without a reason")
	}
	t.Logf("supported=%v reason=%q", ok, reason)
}

// TestSystemSourceCapturesPlayback is the real-device check (criteria
// 2, 5): it plays speech with `say` and expects non-silent 16 kHz audio
// from the tap. Needs a Mac on 14.2+ with "System Audio Recording"
// granted to whatever runs the test (the terminal app), so it's opt-in:
//
//	CATCHM_TAP_TEST=1 go test -run TestSystemSourceCapturesPlayback -v ./internal/capture/
//
// The capture is kept at $CATCHM_TAP_WAV (default: a temp file) for
// listening.
func TestSystemSourceCapturesPlayback(t *testing.T) {
	if os.Getenv("CATCHM_TAP_TEST") != "1" {
		t.Skip("set CATCHM_TAP_TEST=1 for the real-device capture check")
	}
	if ok, reason := SystemAudioSupported(); !ok {
		t.Skip(reason)
	}
	t0 := time.Now()
	src, err := NewSystemSource()
	t.Logf("start took %v", time.Since(t0))
	if err != nil {
		t.Fatal(err)
	}
	say := exec.Command("say", "CatchM system audio test. One, two, three, four, five.")
	if err := say.Start(); err != nil {
		_ = src.Close()
		t.Fatal(err)
	}

	var pcm []byte
	var first time.Duration
	var dropped uint64
	t1 := time.Now()
	deadline := time.After(5 * time.Second)
collect:
	for {
		select {
		case c := <-src.Chunks():
			if first == 0 {
				first = time.Since(t1)
			}
			pcm = append(pcm, c...)
		case <-deadline:
			// Snapshot now: once the test stops reading, the source
			// rightly drops chunks until Close.
			dropped = src.DroppedChunks()
			break collect
		}
	}
	_ = say.Wait()
	noSignal := src.NoSignal()
	t.Logf("first chunk after %v", first)
	t2 := time.Now()
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("close took %v", time.Since(t2))
	if n := waitNoLeftovers(); n != 0 {
		t.Errorf("%d taps/aggregate devices left after Close", n)
	}
	_ = src.Close() // idempotent

	out := os.Getenv("CATCHM_TAP_WAV")
	if out == "" {
		out = filepath.Join(t.TempDir(), "tap.wav")
	}
	f, err := os.Create(out)
	if err == nil {
		_ = audio.EncodeWAV(f, pcm)
		_ = f.Close()
	}

	var peak int
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int(int16(binary.LittleEndian.Uint16(pcm[i:])))
		if v < 0 {
			v = -v
		}
		peak = max(peak, v)
	}
	secs := float64(len(pcm)) / 2 / 16000
	t.Logf("captured %.2fs, peak %d, dropped %d, noSignal %v, wav %s", secs, peak, dropped, noSignal, out)
	// The tap needs ~0.5–1 s to deliver its first buffer; from then on
	// it must keep real time, or the 16 kHz conversion is off.
	if want := (5*time.Second - first).Seconds(); secs < want-0.3 || secs > want+0.3 {
		t.Errorf("captured %.2fs in the %.2fs after the first chunk: rate/conversion is off", secs, want)
	}
	if dropped != 0 {
		t.Errorf("%d chunks dropped while the test was reading", dropped)
	}
	if peak < 300 {
		t.Errorf("peak %d: tap delivered (near) silence — is System Audio Recording granted?", peak)
	}
}

// TestSystemSourceSurvivesRebuild covers criterion 4 on a Mac with a
// single output: it forces the rebuild the default-output listener runs
// and expects capture to continue with a gap ≤ 1 s. Swapping real
// devices (headphones in/out) stays a manual check.
//
//	CATCHM_TAP_TEST=1 go test -run TestSystemSourceSurvivesRebuild -v ./internal/capture/
func TestSystemSourceSurvivesRebuild(t *testing.T) {
	if os.Getenv("CATCHM_TAP_TEST") != "1" {
		t.Skip("set CATCHM_TAP_TEST=1 for the real-device capture check")
	}
	if ok, reason := SystemAudioSupported(); !ok {
		t.Skip(reason)
	}
	src, err := NewSystemSource()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = src.Close()
		if n := waitNoLeftovers(); n != 0 {
			t.Errorf("%d taps/aggregate devices left after Close (rebuild leaked?)", n)
		}
	}()
	if n := leftovers(); n != 2 {
		t.Logf("while running: %d taps/aggregate devices visible (expect 2: one of each)", n)
	}

	// Silence probe: does the tap deliver anything while nothing plays?
	quiet := 0
	for end := time.After(2 * time.Second); ; {
		select {
		case c := <-src.Chunks():
			quiet += len(c) / 2
			continue
		case <-end:
		}
		break
	}
	t.Logf("while silent: %d samples in 2s", quiet)

	// The tap only runs while something plays.
	say := exec.Command("say", "-r", "140", "Rebuild test. Six, seven, eight, nine, ten, eleven, twelve, thirteen.")
	if err := say.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = say.Wait() }()

	// Wait for the tap to flow, then rebuild mid-stream.
	select {
	case <-src.Chunks():
	case <-time.After(3 * time.Second):
		t.Fatal("no audio within 3s")
	}
	rebuilt := make(chan time.Duration, 1)
	go func() {
		t0 := time.Now()
		src.rebuild()
		rebuilt <- time.Since(t0)
	}()

	var maxGap time.Duration
	last := time.Now()
	deadline := time.After(3 * time.Second)
	n := 0
loop:
	for {
		select {
		case <-src.Chunks():
			n++
			if g := time.Since(last); g > maxGap {
				maxGap = g
			}
			last = time.Now()
		case <-deadline:
			break loop
		}
	}
	t.Logf("rebuild took %v; %d chunks after it; largest gap %v", <-rebuilt, n, maxGap)
	if time.Since(last) > time.Second {
		t.Fatalf("capture stopped after the rebuild (last chunk %v ago)", time.Since(last))
	}
	if maxGap > time.Second {
		t.Errorf("gap %v across the rebuild, want ≤ 1s", maxGap)
	}
}

// waitNoLeftovers polls for up to 1 s: the HAL drops a destroyed
// aggregate device from its device list asynchronously (measured: gone
// within 100 ms; the tap disappears immediately).
func waitNoLeftovers() int {
	n := leftovers()
	for end := time.Now().Add(time.Second); n != 0 && time.Now().Before(end); n = leftovers() {
		time.Sleep(20 * time.Millisecond)
	}
	return n
}
