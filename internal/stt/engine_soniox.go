package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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
// binary PCM. The overall shape and the two_way translation object are
// confirmed against Soniox's docs; the one_way translation object below
// is a best guess (the docs only showed two_way).
type sonioxConfig struct {
	APIKey                       string             `json:"api_key"`
	Model                        string             `json:"model"`
	AudioFormat                  string             `json:"audio_format"`
	EnableLanguageIdentification bool               `json:"enable_language_identification"`
	Translation                  *sonioxTranslation `json:"translation,omitempty"`
}

type sonioxTranslation struct {
	Type           string `json:"type"` // "one_way" or "two_way"
	TargetLanguage string `json:"target_language,omitempty"`
	LanguageA      string `json:"language_a,omitempty"`
	LanguageB      string `json:"language_b,omitempty"`
}

// sonioxMessage is the JSON shape of one server message: a recognized
// utterance, optionally carrying a diarized speaker label and, when
// translation was requested, the translated text.
type sonioxMessage struct {
	Text        string `json:"text"`
	Speaker     string `json:"speaker,omitempty"`
	Translation string `json:"translation,omitempty"`
	IsFinal     bool   `json:"is_final"`
}

func (e *SonioxEngine) Stream(ctx context.Context, opts Options) (chan<- PCM, <-chan Result, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+e.apiKey)
	conn, _, err := e.dialer.DialContext(ctx, e.url, header)
	if err != nil {
		return nil, nil, fmt.Errorf("soniox: connect: %w", err)
	}

	cfg := sonioxConfig{
		APIKey:                       e.apiKey,
		Model:                        "stt-rt-v4",
		AudioFormat:                  "auto",
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
			res := Result{Text: msg.Text, Speaker: msg.Speaker, Final: msg.IsFinal}
			if opts.TranslateTo != "" {
				res.Translated = msg.Translation
			}
			select {
			case resultCh <- res:
			case <-ctx.Done():
				return
			}
		}
	}()

	return pcmCh, resultCh, nil
}
