package pipeline

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/levels"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
)

// toneSource is a capture.Source with n chunks of a constant level.
type toneSource struct{ ch chan stt.PCM }

func newToneSource(n int, amp int16) *toneSource {
	s := &toneSource{ch: make(chan stt.PCM, n)}
	for i := 0; i < n; i++ {
		buf := make([]byte, capture.TickSamples*2)
		for j := 0; j < capture.TickSamples; j++ {
			v := amp
			if j%2 == 1 {
				v = -amp
			}
			binary.LittleEndian.PutUint16(buf[j*2:], uint16(v))
		}
		s.ch <- buf
	}
	return s
}

func (s *toneSource) Chunks() <-chan stt.PCM { return s.ch }
func (s *toneSource) Close() error           { return nil }

// timedEngine answers once, after n chunks, with a result placed at
// [0, span) of the audio it was sent.
type timedEngine struct {
	n    int
	span time.Duration
}

func (timedEngine) SupportsDiarization() bool { return true }
func (timedEngine) SupportsTranslation() bool { return false }

func (e timedEngine) Stream(_ context.Context, _ stt.Options) (chan<- stt.PCM, <-chan stt.Result, error) {
	pcmCh := make(chan stt.PCM)
	resultCh := make(chan stt.Result, 1)
	go func() {
		defer close(resultCh)
		got := 0
		for range pcmCh {
			if got++; got == e.n {
				resultCh <- stt.Result{Text: "hello from me", Speaker: "1", Final: true, Start: 0, End: e.span}
			}
		}
	}()
	return pcmCh, resultCh, nil
}

// In "both" mode an utterance the microphone carried is tagged mic, its
// time comes from the engine's offsets, and the loudness timeline is
// left next to audio.wav for re-transcription.
func TestPipelineTagsSourceFromLevels(t *testing.T) {
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Call", time.Now().UTC(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mx, err := capture.NewMixer(capture.RealClock{}, newToneSource(60, 8000), newToneSource(60, 0))
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	audioPath := filepath.Join(store.Root(), m.ID, "audio.wav")
	p, err := Start(store, m.ID, mx, timedEngine{n: 30, span: 400 * time.Millisecond}, stt.Options{}, audioPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForUtterances(t, store, m.ID, 1)
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}
	us, _, err := store.ReadSince(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	u := us[0]
	if u.Source != levels.Mic {
		t.Errorf("source = %q, want mic", u.Source)
	}
	if u.End == nil || u.End.Sub(u.At) != 400*time.Millisecond {
		t.Errorf("span = %v → %v", u.At, u.End)
	}
	// The stream started around Start, not when the result arrived.
	if d := u.At.Sub(before); d < -time.Second || d > 300*time.Millisecond {
		t.Errorf("at is %v after Start", d)
	}
	if _, err := os.Stat(filepath.Join(store.Root(), m.ID, levels.File)); err != nil {
		t.Errorf("timeline: %v", err)
	}
}
