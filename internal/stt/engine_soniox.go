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
