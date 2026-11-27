package batch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/levels"
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

// optionsBatchEngine records the Options and audio path it was given.
type optionsBatchEngine struct {
	us      []meeting.Utterance
	gotOpts stt.Options
	gotPath string
}

func (f *optionsBatchEngine) TranscribeFile(_ context.Context, path string, opts stt.Options) ([]meeting.Utterance, error) {
	f.gotOpts = opts
	f.gotPath = path
	return f.us, nil
}

// TestRefineNilEngineIsNonePath proves a nil engine skips the
// re-transcription entirely: no batch transcript is written.
func TestRefineNilEngineIsNonePath(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	m, err := s.Start("Standup", start, meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.End(m.ID, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root(), m.ID, "audio.wav"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Refine(context.Background(), s, m.ID, nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadBatchTranscript(m.ID)
	if err != nil || len(got) != 0 {
		t.Fatalf("none path wrote a batch transcript: %+v (%v)", got, err)
	}
}

// TestRefinePassesFullTranslationModeAndContext proves Lang,
// TranslateTo, TwoWay, LanguageB (two-way meetings store language B in
// translate_to), Title and ContextTerms (participants) all reach the
// engine, and that non-WAV audio files are discovered.
func TestRefinePassesFullTranslationModeAndContext(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	m, err := s.Start("Bilingual 1:1", start, meeting.StartOptions{
		Lang: "en", TranslateTo: "vi", TwoWay: true, Engine: "soniox",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDoc(m.ID, func(mm *meeting.Meeting) error {
		mm.Participants = []string{"alex", "sam"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.End(m.ID, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root(), m.ID, "audio.m4a"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	engine := &optionsBatchEngine{us: []meeting.Utterance{{Text: "hi", At: time.Time{}.Add(time.Second)}}}
	if err := Refine(context.Background(), s, m.ID, engine, nil); err != nil {
		t.Fatal(err)
	}
	want := stt.Options{
		Lang: "en", TranslateTo: "vi", TwoWay: true, LanguageB: "vi",
		Title: "Bilingual 1:1", ContextTerms: []string{"alex", "sam"},
	}
	got := engine.gotOpts
	if got.Lang != want.Lang || got.TranslateTo != want.TranslateTo || got.TwoWay != want.TwoWay ||
		got.LanguageB != want.LanguageB || got.Title != want.Title ||
		!slices.Equal(got.ContextTerms, want.ContextTerms) {
		t.Fatalf("opts = %+v, want %+v", got, want)
	}
	if !strings.HasSuffix(engine.gotPath, "audio.m4a") {
		t.Fatalf("audio path = %q, want audio.m4a", engine.gotPath)
	}
}

// TestRefineDiscoversAudioByExtension: an imported meeting with only
// audio.mp3 refines; CheckRefinable agrees via the same helper.
func TestRefineDiscoversAudioByExtension(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	m, err := s.Start("Imported", start, meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.End(m.ID, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := CheckRefinable(s, m.ID); !errors.Is(err, ErrNoAudio) {
		t.Fatalf("err = %v, want ErrNoAudio", err)
	}
	if err := os.WriteFile(filepath.Join(s.Root(), m.ID, "audio.mp3"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckRefinable(s, m.ID); err != nil {
		t.Fatal(err)
	}
	engine := &optionsBatchEngine{}
	if err := Refine(context.Background(), s, m.ID, engine, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(engine.gotPath, "audio.mp3") {
		t.Fatalf("audio path = %q", engine.gotPath)
	}
}

// A re-transcribed live recording gets its sources from the loudness
// timeline the live pipeline left next to audio.wav.
func TestRefineAttributesSourcesFromTimeline(t *testing.T) {
	s := newTestStore(t)
	start := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	m, err := s.Start("Call", start, meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.End(m.ID, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Root(), m.ID)
	if err := os.WriteFile(filepath.Join(dir, "audio.wav"), []byte("fake wav bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec, err := levels.Create(filepath.Join(dir, levels.File))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ { // 0–1 s the user, 1–2 s the call
		lv := capture.TickLevels{Mic: 0.2, System: 0.01}
		if i >= 50 {
			lv = capture.TickLevels{Mic: 0.02, System: 0.3}
		}
		rec.Observe(uint64(i), lv)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	at := func(d time.Duration) *time.Time { v := time.Time{}.Add(d); return &v }
	engine := fakeBatchEngine{us: []meeting.Utterance{
		{Text: "mine", At: *at(100 * time.Millisecond), End: at(900 * time.Millisecond)},
		{Text: "theirs", At: *at(1100 * time.Millisecond), End: at(1900 * time.Millisecond)},
		{Text: "no end", At: *at(1200 * time.Millisecond)},
	}}
	if err := Refine(context.Background(), s, m.ID, engine, nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadBatchTranscript(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Source != levels.Mic || got[1].Source != levels.System || got[2].Source != "" {
		t.Fatalf("sources = %+v", got)
	}
	if !got[0].At.Equal(start.Add(100*time.Millisecond)) || !got[1].End.Equal(start.Add(1900*time.Millisecond)) {
		t.Errorf("times = %v, %v", got[0].At, got[1].End)
	}
}
