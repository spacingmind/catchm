// Package batch runs post-meeting STT refinement: transcribing a
// meeting's complete audio recording with a BatchEngine once the
// meeting has ended, and storing the result as the meeting's batch
// transcript.
package batch

import (
	"context"
	"errors"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/translate"
)

// ErrMeetingActive is returned when Refine is called on a meeting that
// has not ended yet: there is no complete recording to transcribe.
var ErrMeetingActive = errors.New("meeting is still active")

// ErrNoAudio aliases meeting.ErrNoAudio: callers of this package get
// the same sentinel the store's AudioPath returns.
var ErrNoAudio = meeting.ErrNoAudio

// Refine transcribes a meeting's full audio recording with engine and
// writes the result as the meeting's batch transcript (see
// meeting.WriteBatchTranscript). The meeting must have ended and have a
// recorded audio file. engine may be nil, in which case no
// re-transcription runs (the "none" engine: the caller summarizes the
// live transcript). translator may be nil, in which case no translation
// step runs. When non-nil, it fills in Translated only for utterances
// engine left untranslated.
func Refine(ctx context.Context, store *meeting.Store, id string, engine stt.BatchEngine, translator translate.Translator) error {
	m, err := store.Get(id)
	if err != nil {
		return err
	}
	if err := checkRefinable(store, m); err != nil {
		return err
	}
	if engine == nil {
		return nil // none: no re-transcription, no v2 write
	}
	audioPath, err := store.AudioPath(id)
	if err != nil {
		return err
	}

	languageB := ""
	if m.TwoWay {
		// Two-way meetings store their two conversation languages in
		// lang (A) and translate_to (B); TranslateTo is otherwise unused.
		languageB = m.TranslateTo
	}
	us, err := engine.TranscribeFile(ctx, audioPath, stt.Options{
		Lang:         m.Lang,
		TranslateTo:  m.TranslateTo,
		TwoWay:       m.TwoWay,
		LanguageB:    languageB,
		Title:        m.Title,
		ContextTerms: m.Participants,
	})
	if err != nil {
		return err
	}
	for i := range us {
		// TranscribeFile returns utterances with At relative to the zero
		// time.Time value; shift them into the meeting's absolute
		// wall-clock time before persisting.
		us[i].At = m.StartedAt.Add(us[i].At.Sub(time.Time{}))
		if us[i].End != nil {
			end := m.StartedAt.Add(us[i].End.Sub(time.Time{}))
			us[i].End = &end
		}
		if translator != nil && m.TranslateTo != "" && us[i].Translated == "" {
			if t, err := translator.Translate(ctx, us[i].Text, m.TranslateTo); err == nil {
				us[i].Translated = t
			}
		}
	}
	return store.WriteBatchTranscript(id, us)
}

// CheckRefinable reports whether meeting id can be refined now: it
// exists (meeting.ErrNotFound), has ended (ErrMeetingActive) and has an
// audio recording (ErrNoAudio). Refine runs the same check.
func CheckRefinable(store *meeting.Store, id string) error {
	m, err := store.Get(id)
	if err != nil {
		return err
	}
	return checkRefinable(store, m)
}

func checkRefinable(store *meeting.Store, m *meeting.Meeting) error {
	if m.EndedAt == nil {
		return ErrMeetingActive
	}
	_, err := store.AudioPath(m.ID)
	return err
}
