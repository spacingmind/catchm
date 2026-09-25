package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/meeting"
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
// rooted at baseURL (e.g. "http://127.0.0.1:8080"). Keep-alives are
// disabled: chunks are infrequent enough that pooling connections just
// leaves idle goroutines around after the session ends.
func NewWhisperEngine(baseURL string) *WhisperEngine {
	return &WhisperEngine{
		baseURL: baseURL,
		client:  &http.Client{Transport: &http.Transport{DisableKeepAlives: true}},
	}
}

func (e *WhisperEngine) SupportsDiarization() bool { return false }
func (e *WhisperEngine) SupportsTranslation() bool { return false }

// EngineStatus implements EngineStatusReporter. Whisper has no
// persistent connection and no chunk failure that stops the stream —
// Stream's loop drops a failed chunk and keeps trying the next one
// (see the comment there) — so "streaming" (the loop is alive and
// willing to process audio) is the only truthful, ever-changing-never
// state to report; there is no honest "error" to surface here (a
// server that's been unreachable for the whole meeting still looks
// like this from Whisper's side, since it never gives up).
func (e *WhisperEngine) EngineStatus() (EngineState, string) {
	return EngineStreaming, ""
}

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

type whisperFileResponse struct {
	Segments []struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Text  string  `json:"text"`
	} `json:"segments"`
}

// TranscribeFile implements BatchEngine by POSTing the whole audio file to
// the whisper.cpp server's /inference endpoint in one request, asking for
// segment-level offsets via response_format=verbose_json.
func (e *WhisperEngine) TranscribeFile(ctx context.Context, audioPath string, opts Options) ([]meeting.Utterance, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, err
	}
	if opts.Lang != "" {
		if err := mw.WriteField("language", opts.Lang); err != nil {
			return nil, err
		}
	}
	if err := mw.WriteField("response_format", "verbose_json"); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/inference", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("whisper server: status %d", resp.StatusCode)
	}
	var wr whisperFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&wr); err != nil {
		return nil, fmt.Errorf("whisper server: decode response: %w", err)
	}

	us := make([]meeting.Utterance, len(wr.Segments))
	for i, seg := range wr.Segments {
		us[i] = meeting.Utterance{
			Seq:  int64(i + 1),
			At:   time.Time{}.Add(time.Duration(seg.Start * float64(time.Second))),
			Text: seg.Text,
		}
	}
	return us, nil
}
