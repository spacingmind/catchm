package capture

import (
	"sync/atomic"
	"time"
)

// SystemAudioSupported and SystemSource are per-OS:
// system_malgo.go (Windows WASAPI loopback; Linux reports unsupported)
// and system_darwin.go (Core Audio process taps, macOS 14.2+, M25).
// Both expose the same surface — NewSystemSource, Chunks, Close,
// DroppedChunks — so the Mixer and internal/server don't care which.

// NoSignalAfter is how long a source may deliver nothing but exact
// digital zeros, from its start, before it reports NoSignal. On macOS a
// denied "System Audio Recording" permission doesn't fail the tap: it
// delivers all-zero buffers (M25). Ten seconds is long enough that a
// meeting which starts in a quiet moment doesn't flash the hint, short
// enough that a user who never granted permission finds out early.
const NoSignalAfter = 10 * time.Second

// SignalReporter is implemented by sources that can tell "nothing
// received at all" apart from ordinary silence (SystemSource on macOS).
// The Mixer surfaces it through InputStats.NoSignal.
type SignalReporter interface {
	NoSignal() bool
}

// silenceWatch tracks whether any non-zero sample has arrived since
// start. It latches: once real audio is seen, NoSignal stays false for
// the source's lifetime, so a quiet stretch later in the meeting never
// re-raises the permission hint.
type silenceWatch struct {
	start time.Time
	heard atomic.Bool
}

func newSilenceWatch(now time.Time) *silenceWatch { return &silenceWatch{start: now} }

// observe scans s16le PCM for any non-zero sample. Cheap: it returns at
// the first non-zero byte, and after the first hit it's a single load.
func (w *silenceWatch) observe(pcm []byte) {
	if w.heard.Load() {
		return
	}
	for _, b := range pcm {
		if b != 0 {
			w.heard.Store(true)
			return
		}
	}
}

func (w *silenceWatch) noSignal(now time.Time) bool {
	return !w.heard.Load() && now.Sub(w.start) >= NoSignalAfter
}
