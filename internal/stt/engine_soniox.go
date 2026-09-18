package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

// SonioxEngine streams PCM to Soniox's realtime WebSocket API: paid,
// cloud, with built-in diarization and translation. Requires an API key,
// sent as a bearer Authorization header on connect.
type SonioxEngine struct {
	url    string
	apiKey string
	dialer *websocket.Dialer
}

// NewSonioxEngine returns an engine that connects to wsURL (e.g.
// "wss://api.soniox.com/transcribe-websocket") authenticating with apiKey.
func NewSonioxEngine(wsURL, apiKey string) *SonioxEngine {
	return &SonioxEngine{url: wsURL, apiKey: apiKey, dialer: websocket.DefaultDialer}
}

func (e *SonioxEngine) SupportsDiarization() bool { return true }
func (e *SonioxEngine) SupportsTranslation() bool { return true }

// sonioxConfig is the JSON shape of the first message Soniox's protocol
// requires after the WebSocket handshake: a text frame sent before any
// binary PCM. Confirmed against Soniox's WebSocket API reference
// (https://soniox.com/docs/api-reference/stt/websocket-api): raw PCM
// audio formats require sample_rate/num_channels alongside
// audio_format, and both the one_way and two_way translation shapes
// are documented (previously only two_way had been verified).
type sonioxConfig struct {
	APIKey                       string             `json:"api_key"`
	Model                        string             `json:"model"`
	AudioFormat                  string             `json:"audio_format"`
	SampleRate                   int                `json:"sample_rate"`
	NumChannels                  int                `json:"num_channels"`
	EnableLanguageIdentification bool               `json:"enable_language_identification"`
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
		Model:  "stt-rt-v4",
		// The pipeline streams raw headerless 16-bit LE mono PCM
		// (see stt.PCM's doc comment), so audio_format must be the
		// explicit pcm_s16le format rather than "auto" — Soniox
		// requires sample_rate/num_channels alongside it for any
		// raw PCM format.
		AudioFormat:                  "pcm_s16le",
		SampleRate:                   16000,
		NumChannels:                  1,
		EnableLanguageIdentification: true,
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

	pcmCh := make(chan PCM)
	resultCh := make(chan Result)

	go func() {
		defer conn.Close()
		for chunk := range pcmCh {
			if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				return
			}
		}
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}()

	go func() {
		defer close(resultCh)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
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
			select {
			case resultCh <- res:
			case <-ctx.Done():
				return
			}
		}
	}()

	return pcmCh, resultCh, nil
}

// buildResult reduces one server message's tokens into a single Result:
// "original"/"none"/unset tokens concatenate into Text, "translation"
// tokens concatenate into Translated, and the message counts as final
// only once every one of its tokens has been finalized.
func buildResult(tokens []sonioxToken) Result {
	var text, translated strings.Builder
	var speaker string
	final := true
	for _, tok := range tokens {
		if tok.Speaker != "" {
			speaker = tok.Speaker
		}
		if !tok.IsFinal {
			final = false
		}
		if tok.TranslationStatus == "translation" {
			translated.WriteString(tok.Text)
		} else {
			text.WriteString(tok.Text)
		}
	}
	return Result{Text: text.String(), Translated: translated.String(), Speaker: speaker, Final: final}
}
