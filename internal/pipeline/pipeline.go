// Package pipeline wires a capture source through an STT engine into a
// meeting's live transcript, while recording the raw audio to disk.
package pipeline

import (
	"context"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/translate"
)

// Pipeline runs for the lifetime of one active meeting: capture -> engine
// -> transcript, plus a raw audio.wav recording.
type Pipeline struct {
	cancel      context.CancelFunc
	src         capture.Source
	wav         *audio.Writer
	feederDone  chan struct{}
	resultsDone chan struct{}
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
		wav:         wav,
		feederDone:  make(chan struct{}),
		resultsDone: make(chan struct{}),
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
			_, _ = store.Append(id, meeting.Utterance{
				Speaker:    res.Speaker,
				Text:       res.Text,
				Translated: translated,
				Lang:       lang,
			})
		}
	}()

	return p, nil
}

// Stop closes the capture source, lets the engine drain and close, waits
// for both pipeline goroutines to exit, and finalizes audio.wav. It is
// safe to call once; calling it leaves no goroutines running.
//
// cancel MUST run before either wait, not after: the feeder's only way to
// give up on a stuck engine (one not currently reading pcmCh — e.g. an
// http.Client.Do to a whisper server that never responds, blocked inside
// engine.Stream's goroutine) is its own `case <-ctx.Done()`. Waiting for
// the feeder to finish before cancelling made that the feeder's ONLY exit
// ever depend on a signal Stop() hadn't sent yet — a real, reproduced
// deadlock (§0, found ending a whisper+mic meeting whose whisper server
// wasn't reachable: goroutine dump showed Stop() parked on
// <-p.feederDone, the feeder parked on its send-or-done select, and the
// engine goroutine parked inside http.Client.Do — nothing left to move
// any of them). Every engine already wires this same ctx into its
// blocking work (http.NewRequestWithContext for Whisper; Soniox dials
// with DialContext and force-closes its websocket conn shortly after
// ctx.Done if the server hasn't already closed it), so cancelling first
// aborts whatever the engine is stuck in and unblocks the whole chain.
func (p *Pipeline) Stop() error {
	// The source is always a Mixer since M19; its Close closes the
	// installed input devices (displaced ones were closed at swap time).
	srcErr := p.src.Close()
	p.cancel()
	<-p.feederDone
	<-p.resultsDone
	wavErr := p.wav.Close()
	if srcErr != nil {
		return srcErr
	}
	return wavErr
}
