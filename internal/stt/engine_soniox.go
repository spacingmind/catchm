package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// SonioxEngine streams PCM to Soniox's realtime WebSocket API: paid,
// cloud, with built-in diarization and translation. Requires an API key,
// sent as a bearer Authorization header on connect.
type SonioxEngine struct {
	url    string
	apiKey string
	dialer *websocket.Dialer
	status atomic.Pointer[sonioxStatus]
}

// sonioxStatus is the immutable snapshot swapped in by status.Store —
// atomic.Pointer, not a mutex, so EngineStatus (polled at ~5 Hz by
// GET /api/meetings/{id}/levels) never blocks on the writer/reader
// goroutines below.
type sonioxStatus struct {
	state EngineState
	err   string
}

// EngineStatus implements EngineStatusReporter.
func (e *SonioxEngine) EngineStatus() (EngineState, string) {
	if s := e.status.Load(); s != nil {
		return s.state, s.err
	}
	// Stream hasn't set an initial status yet (Stream always does so
	// before returning on success) — reached only if EngineStatus is
	// somehow called before Stream, which no caller does today.
	return "", ""
}

func (e *SonioxEngine) setStatus(state EngineState, err string) {
	e.status.Store(&sonioxStatus{state: state, err: err})
}

// NewSonioxEngine returns an engine that connects to wsURL (e.g.
// "wss://stt-rt.soniox.com/transcribe-websocket") authenticating with apiKey.
func NewSonioxEngine(wsURL, apiKey string) *SonioxEngine {
	return &SonioxEngine{url: wsURL, apiKey: apiKey, dialer: websocket.DefaultDialer}
}

func (e *SonioxEngine) SupportsDiarization() bool { return true }
func (e *SonioxEngine) SupportsTranslation() bool { return true }

// sonioxConfig is the JSON shape of the first message Soniox's protocol
// requires after the WebSocket handshake: a text frame sent before any
// binary PCM. Confirmed against Soniox's WebSocket API reference
// (https://soniox.com/docs/api-reference/stt/websocket-api) and
// cross-checked against phuc-nt/my-translator's working production
// client (src/js/soniox.js): raw PCM audio formats require
// sample_rate/num_channels alongside audio_format, diarization must be
// explicitly requested (SupportsDiarization()'s claim was previously
// not backed by any config field), and "stt-rt-v5" is the model a real
// client currently uses (v4 was an unverified guess).
type sonioxConfig struct {
	APIKey                       string             `json:"api_key"`
	Model                        string             `json:"model"`
	AudioFormat                  string             `json:"audio_format"`
	SampleRate                   int                `json:"sample_rate"`
	NumChannels                  int                `json:"num_channels"`
	EnableLanguageIdentification bool               `json:"enable_language_identification"`
	EnableSpeakerDiarization     bool               `json:"enable_speaker_diarization"`
	Translation                  *sonioxTranslation `json:"translation,omitempty"`
}

type sonioxTranslation struct {
	Type           string `json:"type"` // "one_way" or "two_way"
	TargetLanguage string `json:"target_language,omitempty"`
	LanguageA      string `json:"language_a,omitempty"`
	LanguageB      string `json:"language_b,omitempty"`
}

// sonioxMessage is the JSON shape of one server response: a batch of
// recognized tokens, per Soniox's documented shape (not the flat
// text/speaker/translation object previously assumed here).
type sonioxMessage struct {
	Tokens []sonioxToken `json:"tokens"`
}

// sonioxToken is one recognized (or translated) token. TranslationStatus
// is "none"/"" for a non-translation session, "original" for the
// spoken-language token, and "translation" for its translated
// counterpart — both kinds interleave in the same token stream when
// translation is enabled.
type sonioxToken struct {
	Text              string `json:"text"`
	IsFinal           bool   `json:"is_final"`
	Speaker           string `json:"speaker,omitempty"`
	TranslationStatus string `json:"translation_status,omitempty"`
	Language          string `json:"language,omitempty"`
	// StartMS/EndMS place original tokens in the audio (realtime and
	// async alike); translated tokens have none.
	StartMS int64 `json:"start_ms"`
	EndMS   int64 `json:"end_ms"`
}

func (e *SonioxEngine) Stream(ctx context.Context, opts Options) (chan<- PCM, <-chan Result, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+e.apiKey)
	conn, _, err := e.dialer.DialContext(ctx, e.url, header)
	if err != nil {
		return nil, nil, fmt.Errorf("soniox: connect: %w", err)
	}

	cfg := sonioxConfig{
		APIKey: e.apiKey,
		Model:  "stt-rt-v5",
		// The pipeline streams raw headerless 16-bit LE mono PCM
		// (see stt.PCM's doc comment), so audio_format must be the
		// explicit pcm_s16le format rather than "auto" — Soniox
		// requires sample_rate/num_channels alongside it for any
		// raw PCM format.
		AudioFormat:                  "pcm_s16le",
		SampleRate:                   16000,
		NumChannels:                  1,
		EnableLanguageIdentification: true,
		// SupportsDiarization() claims speaker labels; without this
		// flag Soniox never populates them.
		EnableSpeakerDiarization: true,
	}
	if opts.TwoWay {
		cfg.Translation = &sonioxTranslation{
			Type:      "two_way",
			LanguageA: opts.Lang,
			LanguageB: opts.LanguageB,
		}
	} else if opts.TranslateTo != "" {
		cfg.Translation = &sonioxTranslation{
			Type:           "one_way",
			TargetLanguage: opts.TranslateTo,
		}
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("soniox: marshal config: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, cfgBytes); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("soniox: send config: %w", err)
	}
	// Reachable here only once the dial and config handshake both
	// succeeded — see EngineState's doc comment for why "connecting" is
	// never observable through Pipeline/levels: a failed connect returns
	// an error above instead of a Pipeline ever existing to report one.
	e.setStatus(EngineStreaming, "")

	pcmCh := make(chan PCM)
	resultCh := make(chan Result)
	var closeOnce sync.Once
	closeConn := func() { closeOnce.Do(func() { conn.Close() }) }

	// Set right before the writer attempts its end-of-audio frame, so
	// the reader can tell "the connection closed because the server is
	// responding to our own shutdown" (expected) apart from "the
	// connection dropped while we were still actively streaming"
	// (EngineError) — ctx.Done() isn't a usable signal for that
	// distinction here: Pipeline.stop's grace period keeps ctx alive
	// throughout a normal end too (§0), so ctx.Err() is nil at the
	// moment of both a normal and an abnormal close.
	var endOfAudioSent atomic.Bool

	go func() {
		for chunk := range pcmCh {
			if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				// The only normal way this loop ends is pcmCh closing,
				// not a write failing — so a write error here is always
				// a genuine, unexpected connection problem.
				e.setStatus(EngineError, fmt.Sprintf("soniox: send audio: %v", err))
				return
			}
		}
		endOfAudioSent.Store(true)
		// Signal end-of-audio the way a real Soniox client does
		// (confirmed against phuc-nt/my-translator's disconnect()): an
		// empty binary frame, not an abrupt WebSocket close. The server
		// then finalizes and sends any remaining buffered results
		// before closing the connection itself — closing conn here
		// immediately, as this used to, discarded whatever the server
		// hadn't yet had a chance to flush back to the reader goroutine.
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{})
	}()

	readerDone := make(chan struct{})
	go func() {
		// If the caller cancels (Pipeline.Stop) before the server has
		// finished flushing and closed the connection on its own,
		// force-close after a grace period instead of leaking a reader
		// goroutine blocked on ReadMessage forever. readerDone (not
		// resultCh) is the wait target so this never races the
		// pipeline's own consumption of real results off resultCh.
		<-ctx.Done()
		select {
		case <-time.After(3 * time.Second):
			closeConn()
		case <-readerDone:
		}
	}()

	// When translation is requested, Soniox delivers an utterance's
	// original text and its translation as two separate consecutive
	// messages (confirmed against a live session), not combined in one
	// — pairPending holds a finalized original Result until either its
	// translation-only follow-up message arrives (merged in) or a new
	// original message supersedes it (flushed untranslated, so a
	// missing/slow translation never drops the transcript text).
	translationExpected := opts.TwoWay || opts.TranslateTo != ""

	go func() {
		defer close(readerDone)
		defer close(resultCh)
		defer closeConn()
		var pending *Result
		flush := func() bool {
			if pending == nil {
				return true
			}
			res := *pending
			pending = nil
			select {
			case resultCh <- res:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				if !endOfAudioSent.Load() {
					// We hadn't asked to end the session yet — the
					// connection closing now is an unexpected drop, not
					// the server's normal response to our end-of-audio.
					e.setStatus(EngineError, fmt.Sprintf("soniox: connection closed: %v", err))
				}
				flush()
				return
			}
			var msg sonioxMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if len(msg.Tokens) == 0 {
				continue
			}
			res := buildResult(msg.Tokens)
			if !translationExpected {
				select {
				case resultCh <- res:
				case <-ctx.Done():
					return
				}
				continue
			}
			switch {
			case res.Text != "" && res.Translated != "":
				if !flush() {
					return
				}
				select {
				case resultCh <- res:
				case <-ctx.Done():
					return
				}
			case res.Text != "":
				if !flush() {
					return
				}
				pending = &res
			case res.Translated != "" && pending != nil:
				pending.Translated = res.Translated
				if !flush() {
					return
				}
			case res.Translated != "":
				// A translation with nothing pending to attach it to
				// (e.g. arrived after a flush); surface it standalone
				// rather than drop it.
				select {
				case resultCh <- res:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return pcmCh, resultCh, nil
}

// buildResult reduces one server message's tokens into a single Result.
// A single message can mix finalized and still-provisional tokens
// (confirmed against phuc-nt/my-translator's working client) — only
// finalized ("is_final") tokens are kept, matching the pipeline's own
// contract of only ever persisting Result.Final results; provisional
// tokens are dropped rather than blocking the finalized text in the
// same batch from being emitted. "original"/"none"/unset-status
// finalized tokens concatenate into Text, "translation"-status ones
// into Translated. The literal "<end>" token some responses include is
// an end-of-turn marker, not transcript text, and is skipped.
func buildResult(tokens []sonioxToken) Result {
	var text, translated strings.Builder
	var speaker string
	var startMS, endMS int64 = -1, 0
	for _, tok := range tokens {
		if tok.Text == "<end>" || !tok.IsFinal {
			continue
		}
		if tok.Speaker != "" && tok.TranslationStatus == "original" {
			speaker = tok.Speaker
		}
		if tok.TranslationStatus == "translation" {
			translated.WriteString(tok.Text)
			continue
		}
		text.WriteString(tok.Text)
		if tok.EndMS > tok.StartMS {
			if startMS < 0 || tok.StartMS < startMS {
				startMS = tok.StartMS
			}
			endMS = max(endMS, tok.EndMS)
		}
	}
	res := Result{Text: text.String(), Translated: translated.String(), Speaker: speaker, Final: text.Len() > 0}
	if startMS >= 0 && endMS > startMS {
		res.Start, res.End = time.Duration(startMS)*time.Millisecond, time.Duration(endMS)*time.Millisecond
	}
	return res
}
