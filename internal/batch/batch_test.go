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

	if err := Refine(context.Background(), s, m.ID, fakeBatchEngine{}); !errors.Is(err, ErrMeetingActive) {
		t.Fatalf("err = %v, want ErrMeetingActive", err)
	}

	if err := s.End(m.ID, start.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if err := Refine(context.Background(), s, m.ID, fakeBatchEngine{}); !errors.Is(err, ErrNoAudio) {
		t.Fatalf("err = %v, want ErrNoAudio", err)
	}

	audioPath := filepath.Join(s.Root(), m.ID, "audio.wav")
	if err := os.WriteFile(audioPath, []byte("fake wav bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	engine := fakeBatchEngine{us: []meeting.Utterance{
		{Text: "hello", At: time.Time{}.Add(2 * time.Second)},
	}}
	if err := Refine(context.Background(), s, m.ID, engine); err != nil {
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
	if err := Refine(context.Background(), s, "nope", fakeBatchEngine{}); err != meeting.ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
