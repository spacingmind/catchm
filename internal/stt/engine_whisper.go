package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"

	"github.com/spacingmind/catchm/internal/audio"
)

// WhisperEngine talks to a whisper.cpp server (the `server` example built
// from whisper.cpp) over HTTP. It needs no API key: the default, free,
// local engine. Each PCM chunk is sent as a standalone WAV file to the
// server's /inference endpoint; whisper.cpp has no native partial/final
// concept over HTTP, so a response is treated as partial only when it
// carries an explicit "partial": true field, and final otherwise.
type WhisperEngine struct {
	baseURL string
	client  *http.Client
}

// NewWhisperEngine returns an engine that calls the whisper.cpp server
// rooted at baseURL (e.g. "http://127.0.0.1:8080").
func NewWhisperEngine(baseURL string) *WhisperEngine {
	return &WhisperEngine{baseURL: baseURL, client: http.DefaultClient}
}

func (e *WhisperEngine) SupportsDiarization() bool { return false }
func (e *WhisperEngine) SupportsTranslation() bool { return false }

func (e *WhisperEngine) Stream(ctx context.Context, opts Options) (chan<- PCM, <-chan Result, error) {
	pcmCh := make(chan PCM)
	resultCh := make(chan Result)
	go func() {
		defer close(resultCh)
		for {
			select {
			case <-ctx.Done():
				return
			case chunk, ok := <-pcmCh:
				if !ok {
					return
				}
				res, err := e.transcribe(ctx, chunk, opts.Lang)
				if err != nil {
					continue // drop failed chunks; the stream keeps going
				}
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

type whisperResponse struct {
	Text    string `json:"text"`
	Partial bool   `json:"partial"`
}

func (e *WhisperEngine) transcribe(ctx context.Context, chunk PCM, lang string) (Result, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "chunk.wav")
	if err != nil {
		return Result{}, err
	}
	if err := audio.EncodeWAV(fw, chunk); err != nil {
		return Result{}, err
	}
	if lang != "" {
		if err := mw.WriteField("language", lang); err != nil {
			return Result{}, err
		}
	}
	if err := mw.Close(); err != nil {
		return Result{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/inference", &body)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := e.client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("whisper server: status %d", resp.StatusCode)
	}
	var wr whisperResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return Result{}, fmt.Errorf("whisper server: decode response: %w", err)
	}
	return Result{Text: wr.Text, Final: !wr.Partial}, nil
}
