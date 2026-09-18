// Package batch runs post-meeting STT refinement: transcribing a meeting's
// complete audio.wav with a BatchEngine once the meeting has ended, and
// storing the result as the meeting's batch transcript.
package batch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
)

// ErrMeetingActive is returned when Refine is called on a meeting that
// has not ended yet: there is no complete audio.wav to transcribe.
var ErrMeetingActive = errors.New("meeting is still active")

// ErrNoAudio is returned when a meeting has no recorded audio.wav.
var ErrNoAudio = errors.New("meeting has no audio.wav")

// Refine transcribes a meeting's full audio.wav with engine and writes the
// result as the meeting's batch transcript (see meeting.WriteBatchTranscript).
// The meeting must have ended and have a recorded audio.wav.
func Refine(ctx context.Context, store *meeting.Store, id string, engine stt.BatchEngine) error {
	m, err := store.Get(id)
	if err != nil {
		return err
	}
	if m.EndedAt == nil {
		return ErrMeetingActive
	}
	audioPath := filepath.Join(store.Root(), id, "audio.wav")
	if _, err := os.Stat(audioPath); err != nil {
		if os.IsNotExist(err) {
			return ErrNoAudio
		}
		return err
	}

	us, err := engine.TranscribeFile(ctx, audioPath, stt.Options{Lang: m.Lang, TranslateTo: m.TranslateTo})
	if err != nil {
		return err
	}
	// TranscribeFile returns utterances with At relative to the zero
	// time.Time value; shift them into the meeting's absolute wall-clock
	// time before persisting.
	for i := range us {
		us[i].At = m.StartedAt.Add(us[i].At.Sub(time.Time{}))
	}
	return store.WriteBatchTranscript(id, us)
}
