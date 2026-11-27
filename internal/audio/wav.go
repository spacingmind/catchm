// Package audio provides the minimal WAV encode/decode this app needs:
// 16-bit PCM, 16kHz, mono — the fixed format used throughout CatchM for
// both live capture and stored meeting recordings.
package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	SampleRate    = 16000
	Channels      = 1
	BitsPerSample = 16
	headerLen     = 44
)

// Duration is how long n bytes of this format play.
func Duration(n int) time.Duration {
	return time.Duration(n) * time.Second / time.Duration(SampleRate*Channels*BitsPerSample/8)
}

func writeHeader(w io.Writer, dataLen uint32) error {
	byteRate := uint32(SampleRate * Channels * BitsPerSample / 8)
	blockAlign := uint16(Channels * BitsPerSample / 8)

	if _, err := w.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(36+dataLen)); err != nil {
		return err
	}
	if _, err := w.Write([]byte("WAVE")); err != nil {
		return err
	}
	if _, err := w.Write([]byte("fmt ")); err != nil {
		return err
	}
	fields := []any{
		uint32(16), // fmt chunk size
		uint16(1),  // PCM format
		uint16(Channels),
		uint32(SampleRate),
		byteRate,
		blockAlign,
		uint16(BitsPerSample),
	}
	for _, f := range fields {
		if err := binary.Write(w, binary.LittleEndian, f); err != nil {
			return err
		}
	}
	if _, err := w.Write([]byte("data")); err != nil {
		return err
	}
	return binary.Write(w, binary.LittleEndian, dataLen)
}

// EncodeWAV writes pcm as a complete 16-bit/16kHz/mono WAV file.
func EncodeWAV(w io.Writer, pcm []byte) error {
	if err := writeHeader(w, uint32(len(pcm))); err != nil {
		return err
	}
	_, err := w.Write(pcm)
	return err
}

// ReadFile reads a WAV file written in the same fixed format and returns
// its raw PCM samples.
func ReadFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < headerLen || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, fmt.Errorf("not a WAV file: %s", path)
	}
	return raw[headerLen:], nil
}

// Writer incrementally appends PCM to a WAV file, fixing up the header's
// chunk sizes when Close is called.
type Writer struct {
	f *os.File
	n uint32
}

// NewWriter creates path and reserves space for its header, to be filled in
// by Close once the final data length is known.
func NewWriter(path string) (*Writer, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := writeHeader(f, 0); err != nil {
		f.Close()
		return nil, err
	}
	return &Writer{f: f}, nil
}

// Write appends pcm to the file.
func (w *Writer) Write(pcm []byte) (int, error) {
	n, err := w.f.Write(pcm)
	w.n += uint32(n)
	return n, err
}

// Close rewrites the header with the final data length and closes the file.
func (w *Writer) Close() error {
	if _, err := w.f.Seek(0, io.SeekStart); err != nil {
		w.f.Close()
		return err
	}
	if err := writeHeader(w.f, w.n); err != nil {
		w.f.Close()
		return err
	}
	return w.f.Close()
}
