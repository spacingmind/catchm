// Package pipeline wires a capture source through an STT engine into a
// meeting's live transcript, while recording the raw audio to disk.
package pipeline

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/levels"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/translate"
)

// Pipeline runs for the lifetime of one active meeting: capture -> engine
// -> transcript, plus a raw audio.wav recording.
type Pipeline struct {
	cancel      context.CancelFunc
	src         capture.Source
	engine      stt.StreamEngine
	wav         *audio.Writer
	levels      *levels.Recorder // nil unless the source is a Mixer
	feederDone  chan struct{}
	resultsDone chan struct{}
}

// EngineState reports the underlying engine's connection state (M21
// §2), when the engine tracks one — see stt.EngineStatusReporter. ok is
// false for an engine with nothing truthful to report; callers should
// omit any engine-state field entirely rather than show a made-up value.
func (p *Pipeline) EngineState() (state stt.EngineState, errMsg string, ok bool) {
	r, ok := p.engine.(stt.EngineStatusReporter)
	if !ok {
		return "", "", false
	}
	state, errMsg = r.EngineStatus()
	return state, errMsg, true
}

// Mixer returns the pipeline's capture source when it is a *capture.Mixer
// (it is, for every mode since M19: single-input mixers keep one code
// path). Nil otherwise. The server uses it for mid-session source
// switching and level metering.
func (p *Pipeline) Mixer() *capture.Mixer {
	if m, ok := p.src.(*capture.Mixer); ok {
		return m
	}
	return nil
}

// Start begins streaming src through engine into store's transcript for
// meeting id, while also writing raw audio to audioPath. translator may be
// nil, in which case no translation step runs. When non-nil, it fills in
// Translated only for results the engine left untranslated (an engine like
// Soniox that translates natively is never overridden). In two-way mode
// with an engine that has no native translation, the per-utterance
// translation target is the other of the two configured languages, chosen
// by best-effort language detection on the transcribed text.
func Start(store *meeting.Store, id string, src capture.Source, engine stt.StreamEngine, opts stt.Options, audioPath string, translator translate.Translator) (*Pipeline, error) {
	wav, err := audio.NewWriter(audioPath)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	pcmCh, resultCh, err := engine.Stream(ctx, opts)
	if err != nil {
		cancel()
		wav.Close()
		return nil, err
	}

	p := &Pipeline{
		cancel:      cancel,
		src:         src,
		engine:      engine,
		wav:         wav,
		feederDone:  make(chan struct{}),
		resultsDone: make(chan struct{}),
	}

	// The wall-clock time of the stream's first sample, so an engine's
	// audio offsets become utterance times. Estimated from the first
	// tick the mixer reports; Start's own time until then.
	var base atomic.Int64
	base.Store(time.Now().UnixNano())
	if mx, ok := src.(*capture.Mixer); ok {
		// The timeline is best-effort: without it utterances just get
		// no source.
		if rec, err := levels.Create(filepath.Join(filepath.Dir(audioPath), levels.File)); err == nil {
			p.levels = rec
		}
		var first atomic.Bool
		mx.SetTickObserver(func(index uint64, lv capture.TickLevels) {
			if first.CompareAndSwap(false, true) {
				base.Store(time.Now().Add(-tickDuration * time.Duration(index+1)).UnixNano())
			}
			if p.levels != nil {
				p.levels.Observe(index, lv)
			}
		})
	}

	go func() {
		defer close(p.feederDone)
		defer close(pcmCh)
		for chunk := range src.Chunks() {
			_, _ = p.wav.Write(chunk)
			select {
			case pcmCh <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer close(p.resultsDone)
		for res := range resultCh {
			if !res.Final || res.Text == "" {
				continue
			}
			translated := res.Translated
			target := opts.TranslateTo
			if opts.TwoWay && !engine.SupportsTranslation() {
				if translate.DetectLanguage(res.Text, opts.Lang, opts.LanguageB) == opts.Lang {
					target = opts.LanguageB
				} else {
					target = opts.Lang
				}
			}
			if translator != nil && target != "" && translated == "" {
				if t, err := translator.Translate(ctx, res.Text, target); err == nil {
					translated = t
				}
			}
			lang := target
			if opts.TwoWay && engine.SupportsTranslation() {
				// A natively-translating engine (e.g. Soniox) resolves
				// each utterance's direction itself; target is only a
				// fixed fallback here (see the guard above), not a
				// per-utterance signal, so stamping it as Lang would
				// misrepresent whichever of the two languages this
				// utterance actually ended up in.
				lang = ""
			}
			u := meeting.Utterance{
				Speaker:    res.Speaker,
				Text:       res.Text,
				Translated: translated,
				Lang:       lang,
			}
			if res.Timed() {
				start := time.Unix(0, base.Load()).UTC()
				u.At = start.Add(res.Start)
				end := start.Add(res.End)
				u.End = &end
				if p.levels != nil {
					u.Source = p.levels.Source(res.Start, res.End)
				}
			}
			_, _ = store.Append(id, u)
		}
	}()

	return p, nil
}

// stopGraceDeadline bounds how long Stop gives the feeder and engine to
// drain naturally (deliver whatever was still buffered, let the engine
// send its end-of-audio signal and receive any trailing finals) before
// forcing a hard stop. Ending a meeting must never hang (§0), but a
// normal end must also not race an already-cancelled ctx against
// genuinely in-flight sends: every `select { case ch <- x: ; case
// <-ctx.Done(): }` on the drain path picks pseudo-randomly once both
// cases are ready, so cancelling ctx before the drain finishes can drop
// the last chunk or utterance even when nothing is actually stuck.
const stopGraceDeadline = 5 * time.Second

// tickDuration is one mixer tick's audio.
var tickDuration = audio.Duration(capture.TickSamples * 2)

// Stop closes the capture source, lets the engine drain and close, waits
// for both pipeline goroutines to exit, and finalizes audio.wav. It is
// safe to call once; calling it leaves no goroutines running.
func (p *Pipeline) Stop() error {
	return p.stop(stopGraceDeadline)
}

// stop is Stop's implementation, taking the grace deadline as a
// parameter so tests can exercise both the normal-drain and
// forced-cancel paths without waiting on the production deadline.
//
// Ordering, and why: close the source, then give the feeder+engine up to
// grace to finish on their own — ctx is NOT cancelled during this
// window, so every drain-path select's `case <-ctx.Done()` stays
// unready and the corresponding send/receive always wins, exactly as
// before §0's fix existed. Only if the drain doesn't finish within grace
// (a stuck engine — see §0's Decisions for the reproduced case: an
// engine goroutine blocked in synchronous work that never reads pcmCh)
// do we cancel ctx to force it: every engine already wires this same ctx
// into its blocking work (http.NewRequestWithContext for Whisper; Soniox
// dials with DialContext and force-closes its websocket conn on a grace
// timer after ctx.Done, which now starts only once we've actually given
// up — not on every ordinary end), so cancelling unblocks whatever the
// engine is stuck in. cancel is always called exactly once, on every
// path, to release ctx's resources (a no-op if the drain already
// finished on its own).
func (p *Pipeline) stop(grace time.Duration) error {
	// The source is always a Mixer since M19; its Close closes the
	// installed input devices (displaced ones were closed at swap time).
	srcErr := p.src.Close()

	drained := make(chan struct{})
	go func() {
		<-p.feederDone
		<-p.resultsDone
		close(drained)
	}()

	select {
	case <-drained:
		// Normal end: every buffered chunk was fed through, the engine
		// sent its end-of-audio signal and returned any trailing finals
		// on its own.
	case <-time.After(grace):
		p.cancel()
		<-drained
	}
	p.cancel()

	if mx, ok := p.src.(*capture.Mixer); ok {
		mx.SetTickObserver(nil)
	}
	if p.levels != nil {
		_ = p.levels.Close()
	}
	wavErr := p.wav.Close()
	if srcErr != nil {
		return srcErr
	}
	return wavErr
}
