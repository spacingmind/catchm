package batch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
)

// fakeTranslator is a translate.Translator stand-in that returns a fixed
// output and records how many times it was called.
type fakeTranslator struct {
	out   string
	calls int
}

func (f *fakeTranslator) Translate(_ context.Context, _, _ string) (string, error) {
	f.calls++
	return f.out, nil
}

func newTestStore(t *testing.T) *meeting.Store {
	t.Helper()
	s, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type fakeBatchEngine struct {
	us  []meeting.Utterance
	err error
}

func (f fakeBatchEngine) TranscribeFile(_ context.Context, _ string, _ stt.Options) ([]meeting.Utterance, error) {
	return f.us, f.err
}

func TestRefineRequiresEndedMeetingWithAudio(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	m, err := s.Start("Standup", start, meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if err := Refine(context.Background(), s, m.ID, fakeBatchEngine{}, nil); !errors.Is(err, ErrMeetingActive) {
		t.Fatalf("err = %v, want ErrMeetingActive", err)
	}

	if err := s.End(m.ID, start.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := Refine(context.Background(), s, m.ID, fakeBatchEngine{}, nil); !errors.Is(err, ErrNoAudio) {
		t.Fatalf("err = %v, want ErrNoAudio", err)
	}

	audioPath := filepath.Join(s.Root(), m.ID, "audio.wav")
	if err := os.WriteFile(audioPath, []byte("fake wav bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	engine := fakeBatchEngine{us: []meeting.Utterance{
		{Text: "hello", At: time.Time{}.Add(2 * time.Second)},
	}}
	if err := Refine(context.Background(), s, m.ID, engine, nil); err != nil {
		t.Fatal(err)
	}

	got, err := s.ReadBatchTranscript(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "hello" {
		t.Fatalf("batch transcript = %+v", got)
	}
	want := start.Add(2 * time.Second)
	if !got[0].At.Equal(want) {
		t.Fatalf("At = %v, want %v", got[0].At, want)
	}
}

func TestRefineUnknownMeeting(t *testing.T) {
	s := newTestStore(t)
	if err := Refine(context.Background(), s, "nope", fakeBatchEngine{}, nil); err != meeting.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestRefineTranslatesUtterances proves Refine fills in Translated for
// utterances the batch engine left untranslated, while leaving an
// utterance the engine already translated (a hypothetical future batch
// engine with native translation) untouched.
func TestRefineTranslatesUtterances(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	m, err := s.Start("Standup", start, meeting.StartOptions{TranslateTo: "vi"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.End(m.ID, start.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(s.Root(), m.ID, "audio.wav")
	if err := os.WriteFile(audioPath, []byte("fake wav bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	engine := fakeBatchEngine{us: []meeting.Utterance{
		{Text: "hello", At: time.Time{}.Add(1 * time.Second)},
		{Text: "world", Translated: "already translated", At: time.Time{}.Add(2 * time.Second)},
	}}
	translator := &fakeTranslator{out: "xin chao"}
	if err := Refine(context.Background(), s, m.ID, engine, translator); err != nil {
		t.Fatal(err)
	}

	got, err := s.ReadBatchTranscript(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Text != "hello" || got[0].Translated != "xin chao" {
		t.Fatalf("first utterance = %+v", got[0])
	}
	if got[1].Text != "world" || got[1].Translated != "already translated" {
		t.Fatalf("second utterance = %+v", got[1])
	}
	if translator.calls != 1 {
		t.Fatalf("translator called %d times, want 1", translator.calls)
	}
}
