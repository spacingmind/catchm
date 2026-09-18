// Package stt defines the realtime speech-to-text engine interface and its
// implementations: whisper.cpp server (default, local, free) and Soniox
// (cloud, paid, diarization + translation).
package stt

import "context"

// PCM is one chunk of raw 16-bit little-endian, 16kHz mono audio samples.
type PCM []byte

// Options configures a streaming session.
type Options struct {
	Lang        string // source language hint, engine-specific; empty means auto-detect
	TranslateTo string // target language for engines that support translation; empty disables it
}

// Result is one recognized segment of speech, partial or final.
type Result struct {
	Text       string
	Speaker    string // empty when the engine has no diarization
	Translated string // empty when translation was not requested or not supported
	Final      bool
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
