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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/catchm/internal/meeting"
)

// Gladia v2 (docs/plans/completed/cloud-mode-providers.md#gladia): live
// sessions are created over REST and stream over the WebSocket URL it
// returns; files go through upload + pre-recorded jobs. Live has no
// speaker separation (Gladia does that for files only). Its translation
// add-on isn't used: the Translation module covers every engine.
const (
	GladiaAPIURL = "https://api.gladia.io/v2"

	gladiaPoll     = 3 * time.Second
	gladiaMaxVocab = 1000
)

// GladiaEngine implements StreamEngine and BatchEngine.
type GladiaEngine struct {
	apiKey string
	apiURL string
	dialer *websocket.Dialer
	http   *http.Client
	poll   time.Duration
	status atomic.Pointer[engineStatus]
}

func NewGladiaEngine(apiKey string) *GladiaEngine { return newGladiaEngine(apiKey, GladiaAPIURL) }

func newGladiaEngine(apiKey, apiURL string) *GladiaEngine {
	return &GladiaEngine{apiKey: apiKey, apiURL: apiURL, dialer: websocket.DefaultDialer, http: http.DefaultClient, poll: gladiaPoll}
}

// SupportsDiarization is false: live sessions can't separate speakers.
func (e *GladiaEngine) SupportsDiarization() bool { return false }
func (e *GladiaEngine) SupportsTranslation() bool { return false }

func (e *GladiaEngine) EngineStatus() (EngineState, string) {
	if s := e.status.Load(); s != nil {
		return s.state, s.err
	}
	return "", ""
}

func (e *GladiaEngine) setStatus(state EngineState, err string) {
	e.status.Store(&engineStatus{state: state, err: err})
}

// gladiaLanguages: one language → that; none (auto) or two-way → auto
// detection with code switching (an empty list means detect).
func gladiaLanguages(opts Options) map[string]any {
	switch {
	case opts.TwoWay && opts.Lang != "" && opts.LanguageB != "":
		return map[string]any{"languages": []string{opts.Lang, opts.LanguageB}, "code_switching": true}
	case opts.Lang != "":
		return map[string]any{"languages": []string{opts.Lang}, "code_switching": false}
	default:
		return map[string]any{"languages": []string{}, "code_switching": true}
	}
}

func gladiaVocab(terms []string) []string {
	var out []string
	for _, t := range terms {
		if t = strings.TrimSpace(t); t != "" && len(out) < gladiaMaxVocab {
			out = append(out, t)
		}
	}
	return out
}

// post sends JSON (or a prepared body) to the REST API and decodes the
// reply, turning error statuses into plain-language messages.
func (e *GladiaEngine) do(ctx context.Context, method, path, contentType string, body io.Reader, want int, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, e.apiURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("x-gladia-key", e.apiKey)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return fmt.Errorf("gladia: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode != want && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gladia: %s", httpErrorText(resp.StatusCode, raw, "Gladia"))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

type gladiaLiveMessage struct {
	Type string `json:"type"`
	Data struct {
		ID        string `json:"id"`
		IsFinal   bool   `json:"is_final"`
		Utterance struct {
			Text     string `json:"text"`
			Language string `json:"language"`
		} `json:"utterance"`
	} `json:"data"`
}

// Stream implements StreamEngine.
func (e *GladiaEngine) Stream(ctx context.Context, opts Options) (chan<- PCM, <-chan Result, error) {
	init := map[string]any{
		"encoding": "wav/pcm", "bit_depth": 16, "sample_rate": 16000, "channels": 1,
		"model":           "solaria-1",
		"language_config": gladiaLanguages(opts),
		"messages_config": map[string]any{"receive_partial_transcripts": false, "receive_final_transcripts": true},
	}
	if v := gladiaVocab(opts.ContextTerms); len(v) > 0 {
		init["realtime_processing"] = map[string]any{"custom_vocabulary": true, "custom_vocabulary_config": map[string]any{"vocabulary": v}}
	}
	body, _ := json.Marshal(init)
	var session struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := e.do(ctx, http.MethodPost, "/live", "application/json", bytes.NewReader(body), http.StatusCreated, &session); err != nil {
		return nil, nil, err
	}
	if session.URL == "" {
		return nil, nil, fmt.Errorf("gladia: the live session has no URL")
	}
	conn, resp, err := e.dialer.DialContext(ctx, session.URL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("gladia: connect: %s", handshakeError(resp, err, "Gladia"))
	}
	e.setStatus(EngineStreaming, "")

	pcmCh := make(chan PCM)
	resultCh := make(chan Result)
	var closeOnce sync.Once
	closeConn := func() { closeOnce.Do(func() { conn.Close() }) }
	var endSent atomic.Bool

	go func() {
		for chunk := range pcmCh {
			if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				e.setStatus(EngineError, fmt.Sprintf("gladia: send audio: %v", err))
				return
			}
		}
		endSent.Store(true)
		// Gladia flushes, post-processes, then closes with 1000.
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"stop_recording"}`))
	}()

	readerDone := make(chan struct{})
	go func() {
		<-ctx.Done()
		select {
		case <-time.After(3 * time.Second):
			closeConn()
		case <-readerDone:
		}
	}()

	go func() {
		defer close(readerDone)
		defer close(resultCh)
		defer closeConn()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				if !endSent.Load() && !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					e.setStatus(EngineError, "gladia: connection closed: "+closeReason(err))
				}
				return
			}
			var msg gladiaLiveMessage
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch msg.Type {
			case "transcript":
				text := strings.TrimSpace(msg.Data.Utterance.Text)
				if !msg.Data.IsFinal || text == "" {
					continue
				}
				select {
				case resultCh <- Result{Text: text, Final: true}:
				case <-ctx.Done():
					return
				}
			case "end_session":
				return
			}
		}
	}()

	return pcmCh, resultCh, nil
}

type gladiaJob struct {
	Status    string `json:"status"`
	ErrorCode any    `json:"error_code"`
	Result    struct {
		Transcription struct {
			Utterances []struct {
				Text     string  `json:"text"`
				Speaker  *int    `json:"speaker"`
				Start    float64 `json:"start"`
				End      float64 `json:"end"`
				Language string  `json:"language"`
			} `json:"utterances"`
		} `json:"transcription"`
	} `json:"result"`
}

// TranscribeFile implements BatchEngine: upload, create a pre-recorded
// job with diarization, poll it, then delete it.
func (e *GladiaEngine) TranscribeFile(ctx context.Context, audioPath string, opts Options) ([]meeting.Utterance, error) {
	audioURL, err := e.upload(ctx, audioPath)
	if err != nil {
		return nil, err
	}
	job := map[string]any{"audio_url": audioURL, "language_config": gladiaLanguages(opts), "diarization": true}
	if v := gladiaVocab(opts.ContextTerms); len(v) > 0 {
		job["custom_vocabulary"] = true
		job["custom_vocabulary_config"] = map[string]any{"vocabulary": v}
	}
	body, _ := json.Marshal(job)
	var created struct {
		ID string `json:"id"`
	}
	if err := e.do(ctx, http.MethodPost, "/pre-recorded", "application/json", bytes.NewReader(body), http.StatusCreated, &created); err != nil {
		return nil, err
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = e.do(cctx, http.MethodDelete, "/pre-recorded/"+created.ID, "", nil, http.StatusAccepted, nil)
	}()
	for {
		var j gladiaJob
		if err := e.do(ctx, http.MethodGet, "/pre-recorded/"+created.ID, "", nil, http.StatusOK, &j); err != nil {
			return nil, err
		}
		switch j.Status {
		case "done":
			var out []meeting.Utterance
			for _, u := range j.Result.Transcription.Utterances {
				text := strings.TrimSpace(u.Text)
				if text == "" {
					continue
				}
				at := time.Time{}.Add(secondsDuration(u.Start))
				end := time.Time{}.Add(secondsDuration(u.End))
				ut := meeting.Utterance{Seq: int64(len(out) + 1), At: at, End: &end, Text: text, Lang: u.Language}
				if u.Speaker != nil {
					ut.Speaker = fmt.Sprintf("Speaker %d", *u.Speaker+1)
				}
				out = append(out, ut)
			}
			return out, nil
		case "error":
			return nil, fmt.Errorf("gladia: transcription failed (%v)", j.ErrorCode)
		}
		select {
		case <-time.After(e.poll):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (e *GladiaEngine) upload(ctx context.Context, audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("audio", filepath.Base(audioPath))
		if err == nil {
			_, err = io.Copy(part, f)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	var out struct {
		AudioURL string `json:"audio_url"`
	}
	if err := e.do(ctx, http.MethodPost, "/upload", mw.FormDataContentType(), pr, http.StatusOK, &out); err != nil {
		return "", err
	}
	if out.AudioURL == "" {
		return "", fmt.Errorf("gladia: upload returned no audio_url")
	}
	return out.AudioURL, nil
}

// VerifyKey lists at most one pre-recorded job: free, read-only.
func (e *GladiaEngine) VerifyKey(ctx context.Context) error {
	err := e.do(ctx, http.MethodGet, "/pre-recorded?limit=1", "", nil, http.StatusOK, nil)
	if err != nil {
		return fmt.Errorf("%s", strings.TrimPrefix(err.Error(), "gladia: "))
	}
	return nil
}
