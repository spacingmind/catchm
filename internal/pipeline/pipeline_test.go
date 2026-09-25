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
	result            stt.Result
	nativeTranslation bool
}

func (scriptedEngine) SupportsDiarization() bool   { return false }
func (e scriptedEngine) SupportsTranslation() bool { return e.nativeTranslation }

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

	// FileSource now paces chunks to real time (see its doc comment), so
	// Stop() must wait for every chunk (sampleBytes/800 = 4) to have
	// actually been sent before asserting audio.wav has the full
	// fixture — stopping right after the first chunk's utterance, as
	// this used to, would now correctly capture only what had been
	// streamed so far, not the whole file.
	waitForUtterances(t, store, m.ID, sampleBytes/800)

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

// stuckEngine is a stt.StreamEngine stand-in for an engine goroutine
// wedged inside blocking work that doesn't read pcmCh at all — e.g. the
// real WhisperEngine blocked in http.Client.Do against a whisper server
// that accepted the TCP connection but never responds (a silent black
// hole, not a fast connection-refused). It only gives up when ctx is
// cancelled, exactly like the real engines' blocking calls do.
type stuckEngine struct{}

func (stuckEngine) SupportsDiarization() bool { return false }
func (stuckEngine) SupportsTranslation() bool { return false }

func (stuckEngine) Stream(ctx context.Context, _ stt.Options) (chan<- stt.PCM, <-chan stt.Result, error) {
	pcmCh := make(chan stt.PCM)
	resultCh := make(chan stt.Result)
	go func() {
		defer close(resultCh)
		<-ctx.Done() // never drains pcmCh until told to stop
	}()
	return pcmCh, resultCh, nil
}

// TestPipelineStopUnblocksStuckEngine is the §0 regression test: a
// meeting must always be endable, even when the engine goroutine is
// wedged in blocking work and never reads pcmCh. Before the fix, Stop
// cancelled ctx only after waiting for the feeder to finish — but the
// feeder's only way to give up on a non-draining pcmCh *was* ctx
// cancellation, so Stop() never returned. Uses the internal stop(grace)
// seam with a short grace so the test itself stays fast; production
// Stop() wires the same method to stopGraceDeadline. Bounded by a
// deadline well past grace: this test fails (rather than hangs the test
// suite) if the regression comes back, and the tight bound also proves
// stop() doesn't overshoot grace by much when it does have to force a
// cancel.
func TestPipelineStopUnblocksStuckEngine(t *testing.T) {
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{Engine: "whisper"})
	if err != nil {
		t.Fatal(err)
	}

	// A real (fast-paced) source, so the feeder actually has a chunk to
	// try handing to the engine — an idle mixer producing nothing would
	// never exercise the blocked send this test is about.
	src, err := capture.NewFileSource(fixtureWAV(t, 3200), 800)
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(store.Root(), m.ID, "audio.wav")
	p, err := Start(store, m.ID, src, stuckEngine{}, stt.Options{}, audioPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Let the feeder actually reach (and block on) its send-or-done
	// select before we ask it to stop.
	time.Sleep(50 * time.Millisecond)

	const grace = 100 * time.Millisecond
	start := time.Now()
	stopDone := make(chan error, 1)
	go func() { stopDone <- p.stop(grace) }()

	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed > grace+500*time.Millisecond {
			t.Fatalf("stop(grace=%s) took %s — grace deadline is not actually bounding it", grace, elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop() did not return within 2s against a stuck engine — the §0 deadlock regressed")
	}
}

// delayedFinalEngine is a stt.StreamEngine stand-in for the ordinary
// "flush the tail after end-of-audio" pattern real engines use: Soniox
// sends an empty end-of-audio frame once pcmCh closes and then waits for
// the server to send back final tokens before closing resultCh; Whisper
// transcribes whatever chunk was still in flight. It drains pcmCh
// normally, then — after pcmCh closes — waits delay (standing in for
// that server round-trip) before emitting one last Result.
type delayedFinalEngine struct {
	delay  time.Duration
	result stt.Result
}

func (delayedFinalEngine) SupportsDiarization() bool { return false }
func (delayedFinalEngine) SupportsTranslation() bool { return false }

func (e delayedFinalEngine) Stream(ctx context.Context, _ stt.Options) (chan<- stt.PCM, <-chan stt.Result, error) {
	pcmCh := make(chan stt.PCM)
	resultCh := make(chan stt.Result)
	go func() {
		defer close(resultCh)
		for range pcmCh {
			// Drain and discard — this test is about the post-close tail,
			// not the chunks themselves.
		}
		select {
		case <-time.After(e.delay):
		case <-ctx.Done():
			return
		}
		select {
		case resultCh <- e.result:
		case <-ctx.Done():
		}
	}()
	return pcmCh, resultCh, nil
}

// TestPipelineStopDrainsTailBeforeCancelling is the companion regression
// test to TestPipelineStopUnblocksStuckEngine: a normal end (nothing
// stuck) must not race an already-cancelled ctx against the engine's
// legitimate post-end-of-audio tail and drop it. Before this fix, stop()
// cancelled ctx immediately after closing the source, so this exact
// scenario — an engine that finishes draining and then, shortly after,
// sends its last result — would non-deterministically lose that result
// (or, with Soniox specifically, get its whole grace-close timer started
// too early). A short grace here still comfortably covers delay, proving
// the drain is awaited rather than raced.
func TestPipelineStopDrainsTailBeforeCancelling(t *testing.T) {
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{Engine: "whisper"})
	if err != nil {
		t.Fatal(err)
	}

	src, err := capture.NewFileSource(fixtureWAV(t, 3200), 800)
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(store.Root(), m.ID, "audio.wav")
	const delay = 150 * time.Millisecond
	engine := delayedFinalEngine{delay: delay, result: stt.Result{Text: "final words", Final: true}}
	p, err := Start(store, m.ID, src, engine, stt.Options{}, audioPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Let the file source finish its (fast) playback so pcmCh has
	// already closed and the engine is inside its post-close delay
	// before Stop is even called — the scenario under test.
	time.Sleep(100 * time.Millisecond)

	if err := p.stop(500 * time.Millisecond); err != nil {
		t.Fatal(err)
	}

	us, _, err := store.ReadSince(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range us {
		if u.Text == "final words" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tail result dropped by stop(); utterances = %+v", us)
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

// TestPipelineTwoWayNativeEngineLeavesLangUnset proves a two-way meeting
// on an engine that already resolves translation direction itself (like
// Soniox) doesn't stamp a fixed, potentially-wrong Lang on every
// utterance — since the pipeline has no signal for which of the two
// configured languages a given utterance actually ended up in, Lang
// must be left empty rather than guessed.
func TestPipelineTwoWayNativeEngineLeavesLangUnset(t *testing.T) {
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{TwoWay: true})
	if err != nil {
		t.Fatal(err)
	}

	engine := scriptedEngine{
		result:            stt.Result{Text: "hello", Translated: "xin chao", Final: true},
		nativeTranslation: true,
	}
	p := startScripted(t, store, m.ID, engine, stt.Options{Lang: "en", LanguageB: "vi", TwoWay: true}, nil)

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
		if u.Lang != "" {
			t.Fatalf("utterance = %+v, want Lang unset for a native-translating two-way engine", u)
		}
		if u.Translated != "xin chao" {
			t.Fatalf("utterance = %+v, want the engine's own translation untouched", u)
		}
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

// twoUtteranceEngine is a stt.StreamEngine stand-in with no native
// translation that alternates between two fixed results — one English, one
// Vietnamese — one per received pcm chunk, for exercising two-way wiring.
type twoUtteranceEngine struct{}

func (twoUtteranceEngine) SupportsDiarization() bool { return false }
func (twoUtteranceEngine) SupportsTranslation() bool { return false }

func (twoUtteranceEngine) Stream(_ context.Context, _ stt.Options) (chan<- stt.PCM, <-chan stt.Result, error) {
	pcmCh := make(chan stt.PCM)
	resultCh := make(chan stt.Result, 2)
	results := []stt.Result{
		{Text: "Could you walk me through the deployment plan one more time", Final: true},
		{Text: "Tôi muốn hỏi về kế hoạch triển khai của dự án", Final: true},
	}
	go func() {
		defer close(resultCh)
		i := 0
		for range pcmCh {
			select {
			case resultCh <- results[i%len(results)]:
			default:
			}
			i++
		}
	}()
	return pcmCh, resultCh, nil
}

// directionAwareTranslator is a translate.Translator stand-in that records
// every target language it was asked for and answers with "→" + target, so
// tests can assert which direction each utterance was translated in.
type directionAwareTranslator struct {
	targets []string
}

func (d *directionAwareTranslator) Translate(_ context.Context, _, target string) (string, error) {
	d.targets = append(d.targets, target)
	return "→" + target, nil
}

// TestPipelineTwoWayTranslatesBothDirections drives a fake no-native-
// translation engine emitting one English and one Vietnamese utterance
// through a two-way pipeline: each utterance must be translated into the
// OTHER language of the pair (detected per utterance), never the same
// direction twice.
func TestPipelineTwoWayTranslatesBothDirections(t *testing.T) {
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{Engine: "whisper"})
	if err != nil {
		t.Fatal(err)
	}

	src, err := capture.NewFileSource(fixtureWAV(t, 3200), 800)
	if err != nil {
		t.Fatal(err)
	}
	translator := &directionAwareTranslator{}
	audioPath := filepath.Join(store.Root(), m.ID, "audio.wav")
	p, err := Start(store, m.ID, src, twoUtteranceEngine{}, stt.Options{Lang: "en", LanguageB: "vi", TwoWay: true}, audioPath, translator)
	if err != nil {
		t.Fatal(err)
	}

	waitForUtterances(t, store, m.ID, 2)
	if err := p.Stop(); err != nil {
		t.Fatal(err)
	}

	const enText = "Could you walk me through the deployment plan one more time"
	const viText = "Tôi muốn hỏi về kế hoạch triển khai của dự án"
	us, _, err := store.ReadSince(m.ID, -1)
	if err != nil {
		t.Fatal(err)
	}
	sawToVi, sawToEn := false, false
	for _, u := range us {
		switch u.Text {
		case enText:
			if u.Translated != "→vi" || u.Lang != "vi" {
				t.Fatalf("english utterance = %+v, want translated to vi", u)
			}
			sawToVi = true
		case viText:
			if u.Translated != "→en" || u.Lang != "en" {
				t.Fatalf("vietnamese utterance = %+v, want translated to en", u)
			}
			sawToEn = true
		default:
			t.Fatalf("unexpected utterance text %q", u.Text)
		}
	}
	if !sawToVi || !sawToEn {
		t.Fatalf("both directions required: sawToVi=%v sawToEn=%v", sawToVi, sawToEn)
	}
	for _, target := range translator.targets {
		if target != "en" && target != "vi" {
			t.Fatalf("translator asked for target %q, want only en or vi", target)
		}
	}
}
