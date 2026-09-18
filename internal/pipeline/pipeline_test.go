package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/tools"
	"github.com/spacingmind/catchm/internal/translate"
)

// scriptedEngine is a stt.StreamEngine stand-in that emits result for every
// pcm chunk it receives: no network, no audio hardware, fully controllable
// output for exercising the pipeline's translation wiring.
type scriptedEngine struct {
	result stt.Result
}

func (scriptedEngine) SupportsDiarization() bool { return false }
func (scriptedEngine) SupportsTranslation() bool { return false }

func (e scriptedEngine) Stream(_ context.Context, _ stt.Options) (chan<- stt.PCM, <-chan stt.Result, error) {
	pcmCh := make(chan stt.PCM)
	resultCh := make(chan stt.Result, 1)
	go func() {
		defer close(resultCh)
		for range pcmCh {
			select {
			case resultCh <- e.result:
			default:
			}
		}
	}()
	return pcmCh, resultCh, nil
}

// fakeTranslator is a translate.Translator stand-in that returns a fixed
// output or error and records how many times it was called.
type fakeTranslator struct {
	out   string
	err   error
	calls int
}

func (f *fakeTranslator) Translate(_ context.Context, _, _ string) (string, error) {
	f.calls++
	return f.out, f.err
}

func startScripted(t *testing.T, store *meeting.Store, id string, engine scriptedEngine, opts stt.Options, translator *fakeTranslator) *Pipeline {
	t.Helper()
	src, err := capture.NewFileSource(fixtureWAV(t, 3200), 800)
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(store.Root(), id, "audio.wav")
	// translator must stay a true nil interface (not a nil *fakeTranslator
	// wrapped in a non-nil interface) so Start's "translator != nil" check
	// behaves as callers expect.
	var tr translate.Translator
	if translator != nil {
		tr = translator
	}
	p, err := Start(store, id, src, engine, opts, audioPath, tr)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func fakeWhisperServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"text": "hello world"})
	}))
}

func fixtureWAV(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.wav")
	w, err := audio.NewWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	samples := make([]byte, n)
	for i := range samples {
		samples[i] = byte(i)
	}
	if _, err := w.Write(samples); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForUtterances(t *testing.T, store *meeting.Store, id string, min int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		us, _, err := store.ReadSince(id, -1)
		if err != nil {
			t.Fatal(err)
		}
		if len(us) >= min {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("transcript never reached %d utterances", min)
}

// TestPipelineLifecycle drives a file-source capture through a fake
// whisper engine into a meeting's transcript, then verifies clean shutdown:
// utterances.jsonl grew, audio.wav is a valid WAV with the replayed byte
// count, and stopping leaves no goroutines running.
func TestPipelineLifecycle(t *testing.T) {
	srv := fakeWhisperServer(t)
	defer srv.Close()

	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{Engine: "whisper"})
	if err != nil {
		t.Fatal(err)
	}

	const sampleBytes = 3200 // 100ms of 16kHz mono 16-bit PCM
	fixturePath := fixtureWAV(t, sampleBytes)
	audioPath := filepath.Join(store.Root(), m.ID, "audio.wav")

	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	before := runtime.NumGoroutine()

	src, err := capture.NewFileSource(fixturePath, 800)
	if err != nil {
		t.Fatal(err)
	}
	engine := stt.NewWhisperEngine(srv.URL)

	p, err := Start(store, m.ID, src, engine, stt.Options{}, audioPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	waitForUtterances(t, store, m.ID, 1)

	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}

	pcm, err := audio.ReadFile(audioPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(pcm) != sampleBytes {
		t.Fatalf("audio.wav has %d PCM bytes, want %d", len(pcm), sampleBytes)
	}

	us, _, err := store.ReadSince(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) == 0 {
		t.Fatal("no utterances written")
	}
	for _, u := range us {
		if u.Text != "hello world" {
			t.Fatalf("utterance = %+v", u)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	var after int
	for time.Now().Before(deadline) {
		runtime.GC()
		after = runtime.NumGoroutine()
		if after <= before {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if after > before {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}

// TestEngineUtterancesCompatWithReadLiveTranscript proves M1's cursor-based
// read_live_transcript path (internal/tools.Surface.ReadLiveTranscript)
// works unchanged over a transcript mixing an M1-style fixture utterance
// (appended directly) with utterances the M2 pipeline produced from a
// streaming engine: same seq numbering, same cursor semantics.
func TestEngineUtterancesCompatWithReadLiveTranscript(t *testing.T) {
	srv := fakeWhisperServer(t)
	defer srv.Close()

	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{Engine: "whisper"})
	if err != nil {
		t.Fatal(err)
	}
	surface := tools.New(store)

	// M1-style fixture utterance, appended the same way M1 tests do.
	fixture, err := store.Append(m.ID, meeting.Utterance{Speaker: "alex", Text: "kicking off standup"})
	if err != nil {
		t.Fatal(err)
	}

	us, cursor, err := surface.ReadLiveTranscript(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) != 1 || us[0].Seq != fixture.Seq || cursor != fixture.Seq {
		t.Fatalf("pre-engine read = %+v cursor=%d", us, cursor)
	}

	src, err := capture.NewFileSource(fixtureWAV(t, 3200), 800)
	if err != nil {
		t.Fatal(err)
	}
	engine := stt.NewWhisperEngine(srv.URL)
	audioPath := filepath.Join(store.Root(), m.ID, "audio.wav")
	p, err := Start(store, m.ID, src, engine, stt.Options{}, audioPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForUtterances(t, store, m.ID, 2)
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}

	delta, newCursor, err := surface.ReadLiveTranscript(m.ID, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta) == 0 {
		t.Fatal("no engine-produced utterances read after the fixture's cursor")
	}
	for i, u := range delta {
		if u.Seq <= cursor {
			t.Fatalf("delta[%d] seq %d not greater than prior cursor %d", i, u.Seq, cursor)
		}
		if u.Text != "hello world" {
			t.Fatalf("delta[%d] = %+v", i, u)
		}
	}
	if newCursor != delta[len(delta)-1].Seq {
		t.Fatalf("cursor = %d, want %d", newCursor, delta[len(delta)-1].Seq)
	}

	all, _, err := surface.ReadLiveTranscript(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1+len(delta) || all[0].Text != "kicking off standup" {
		t.Fatalf("full transcript = %+v", all)
	}
}

// TestPipelineTranslatesWhenEngineLacksNativeTranslation drives a fake
// engine (like whisper.cpp: no native translation, Translated always
// empty) through a fake translator; the appended utterance must carry the
// translator's output.
func TestPipelineTranslatesWhenEngineLacksNativeTranslation(t *testing.T) {
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{TranslateTo: "vi"})
	if err != nil {
		t.Fatal(err)
	}

	engine := scriptedEngine{result: stt.Result{Text: "hello", Final: true}}
	translator := &fakeTranslator{out: "xin chao"}
	p := startScripted(t, store, m.ID, engine, stt.Options{TranslateTo: "vi"}, translator)

	waitForUtterances(t, store, m.ID, 1)
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}

	us, _, err := store.ReadSince(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) == 0 {
		t.Fatal("no utterances written")
	}
	for _, u := range us {
		if u.Text != "hello" || u.Translated != "xin chao" {
			t.Fatalf("utterance = %+v", u)
		}
	}
	if translator.calls == 0 {
		t.Fatal("translator was never called")
	}
}

// TestPipelineDoesNotDoubleTranslate proves an engine that already fills
// Result.Translated (like Soniox's native translation) is never overridden
// by the translation layer, and the translator is not even called.
func TestPipelineDoesNotDoubleTranslate(t *testing.T) {
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{TranslateTo: "vi"})
	if err != nil {
		t.Fatal(err)
	}

	engine := scriptedEngine{result: stt.Result{Text: "hello", Translated: "already translated", Final: true}}
	translator := &fakeTranslator{out: "should never see this"}
	p := startScripted(t, store, m.ID, engine, stt.Options{TranslateTo: "vi"}, translator)

	waitForUtterances(t, store, m.ID, 1)
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}

	us, _, err := store.ReadSince(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) == 0 {
		t.Fatal("no utterances written")
	}
	for _, u := range us {
		if u.Translated != "already translated" {
			t.Fatalf("utterance = %+v", u)
		}
	}
	if translator.calls != 0 {
		t.Fatalf("translator called %d times, want 0", translator.calls)
	}
}

// TestPipelineNilTranslatorUnaffected proves Start with a nil translator
// (M2/M2.5 behavior) never attempts translation and never panics, even
// when TranslateTo is set.
func TestPipelineNilTranslatorUnaffected(t *testing.T) {
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{TranslateTo: "vi"})
	if err != nil {
		t.Fatal(err)
	}

	engine := scriptedEngine{result: stt.Result{Text: "hello", Final: true}}
	p := startScripted(t, store, m.ID, engine, stt.Options{TranslateTo: "vi"}, nil)

	waitForUtterances(t, store, m.ID, 1)
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}

	us, _, err := store.ReadSince(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) == 0 {
		t.Fatal("no utterances written")
	}
	for _, u := range us {
		if u.Text != "hello" || u.Translated != "" {
			t.Fatalf("utterance = %+v", u)
		}
	}
}

// TestPipelineTranslationErrorKeepsUtterance proves a translation error is
// swallowed: the utterance is still appended untranslated, with no panic
// and no leaked goroutines.
func TestPipelineTranslationErrorKeepsUtterance(t *testing.T) {
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{TranslateTo: "vi"})
	if err != nil {
		t.Fatal(err)
	}

	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	before := runtime.NumGoroutine()

	engine := scriptedEngine{result: stt.Result{Text: "hello", Final: true}}
	translator := &fakeTranslator{err: errors.New("ollama unreachable")}
	p := startScripted(t, store, m.ID, engine, stt.Options{TranslateTo: "vi"}, translator)

	waitForUtterances(t, store, m.ID, 1)
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}

	us, _, err := store.ReadSince(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(us) == 0 {
		t.Fatal("no utterances written")
	}
	for _, u := range us {
		if u.Text != "hello" || u.Translated != "" {
			t.Fatalf("utterance = %+v", u)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	var after int
	for time.Now().Before(deadline) {
		runtime.GC()
		after = runtime.NumGoroutine()
		if after <= before {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if after > before {
		t.Fatalf("goroutine leak: before=%d after=%d", before, after)
	}
}
