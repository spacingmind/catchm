// Package pipeline wires a capture source through an STT engine into a
// meeting's live transcript, while recording the raw audio to disk.
package pipeline

import (
	"context"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/capture"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
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

// Start begins streaming src through engine into store's transcript for
// meeting id, while also writing raw audio to audioPath.
func Start(store *meeting.Store, id string, src capture.Source, engine stt.StreamEngine, opts stt.Options, audioPath string) (*Pipeline, error) {
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
			_, _ = store.Append(id, meeting.Utterance{
				Speaker:    res.Speaker,
				Text:       res.Text,
				Translated: res.Translated,
				Lang:       opts.TranslateTo,
			})
		}
	}()

	return p, nil
}

// Stop closes the capture source, lets the engine drain and close, waits
// for both pipeline goroutines to exit, and finalizes audio.wav. It is
// safe to call once; calling it leaves no goroutines running.
func (p *Pipeline) Stop() error {
	srcErr := p.src.Close()
	<-p.feederDone
	p.cancel()
	<-p.resultsDone
	wavErr := p.wav.Close()
	if srcErr != nil {
		return srcErr
	}
	return wavErr
}
