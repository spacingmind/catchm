// Package stt defines the realtime speech-to-text engine interface and its
// implementations: whisper.cpp server (default, local, free) and Soniox
// (cloud, paid, diarization + translation).
package stt

import (
	"context"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
)

// PCM is one chunk of raw 16-bit little-endian, 16kHz mono audio samples.
type PCM []byte

// Options configures a streaming session.
type Options struct {
	Lang         string   // source language hint, engine-specific; empty means auto-detect
	TranslateTo  string   // target language for engines that support translation; empty disables it
	LanguageB    string   // two-way mode: the second conversation language, alongside Lang
	TwoWay       bool     // translate each utterance into whichever of Lang/LanguageB it wasn't spoken in; when true, TranslateTo is unused
	Title        string   // meeting title, fed to engines that accept context (Soniox async); empty disables it
	ContextTerms []string // domain terms and participant names to bias recognition (Soniox async); empty disables it
}

// Result is one recognized segment of speech, partial or final.
type Result struct {
	Text       string
	Speaker    string // empty when the engine has no diarization
	Translated string // empty when translation was not requested or not supported
	Final      bool
	// Start and End are the result's position in the audio sent on this
	// stream, measured from its first sample. End == 0 means the engine
	// gave no timing (the pipeline falls back to the arrival time).
	Start, End time.Duration
}

// Timed reports whether the engine placed the result in the audio.
func (r Result) Timed() bool { return r.End > 0 }

// secondsSpan converts an engine's start/end seconds into a Result span;
// a zero or inverted span means no timing.
func secondsSpan(start, end float64) (time.Duration, time.Duration) {
	if end <= start || end <= 0 {
		return 0, 0
	}
	return secondsDuration(start), secondsDuration(end)
}

// StreamEngine streams PCM audio to a realtime STT backend and returns
// recognized results as they arrive. The caller sends PCM chunks on the
// returned send channel and closes it to end the session; the engine closes
// the result channel once it has drained any in-flight work.
type StreamEngine interface {
	Stream(ctx context.Context, opts Options) (chan<- PCM, <-chan Result, error)
	SupportsDiarization() bool
	SupportsTranslation() bool
}

// BatchEngine transcribes a complete audio file in one request, trading
// realtime responsiveness for the full-context accuracy a streaming
// session can't offer. Utterance.At in the result is relative to the
// zero time.Time value, not wall-clock time: callers with a meeting's
// real start time (like internal/batch.Refine) shift it into absolute
// time before persisting.
type BatchEngine interface {
	TranscribeFile(ctx context.Context, audioPath string, opts Options) ([]meeting.Utterance, error)
}
