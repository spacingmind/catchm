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

// Speechmatics (docs/plans/completed/cloud-mode-providers.md#speechmatics):
// realtime over a WebSocket, files through the batch jobs API. Its
// translation add-on ($0.65/h, English pairs only) isn't used: the
// Translation module covers every engine.
const (
	SpeechmaticsStreamURL = "wss://eu.rt.speechmatics.com/v2"
	SpeechmaticsBatchURL  = "https://eu1.asr.api.speechmatics.com/v2"

	speechmaticsMaxVocab = 1000
	// Finals arrive in pieces every max_delay seconds; a line is emitted
	// at a sentence end or speaker change, or after this much quiet.
	speechmaticsLineIdle = 2 * time.Second
	speechmaticsPoll     = 3 * time.Second
)

// SpeechmaticsEngine implements StreamEngine and BatchEngine.
type SpeechmaticsEngine struct {
	apiKey    string
	streamURL string
	batchURL  string
	dialer    *websocket.Dialer
	http      *http.Client
	poll      time.Duration
	status    atomic.Pointer[engineStatus]
}

func NewSpeechmaticsEngine(apiKey string) *SpeechmaticsEngine {
	return newSpeechmaticsEngine(apiKey, SpeechmaticsStreamURL, SpeechmaticsBatchURL)
}

func newSpeechmaticsEngine(apiKey, streamURL, batchURL string) *SpeechmaticsEngine {
	return &SpeechmaticsEngine{apiKey: apiKey, streamURL: streamURL, batchURL: batchURL, dialer: websocket.DefaultDialer, http: http.DefaultClient, poll: speechmaticsPoll}
}

func (e *SpeechmaticsEngine) SupportsDiarization() bool { return true }
func (e *SpeechmaticsEngine) SupportsTranslation() bool { return false }

func (e *SpeechmaticsEngine) EngineStatus() (EngineState, string) {
	if s := e.status.Load(); s != nil {
		return s.state, s.err
	}
	return "", ""
}

func (e *SpeechmaticsEngine) setStatus(state EngineState, err string) {
	e.status.Store(&engineStatus{state: state, err: err})
}

func (e *SpeechmaticsEngine) header() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+e.apiKey)
	return h
}

type smVocab struct {
	Content string `json:"content"`
}

type smTranscriptionConfig struct {
	Language       string    `json:"language"`
	Model          string    `json:"model,omitempty"`           // melia-1 only
	OperatingPoint string    `json:"operating_point,omitempty"` // "enhanced": long-standing, still accepted
	EnablePartials bool      `json:"enable_partials"`
	MaxDelay       float64   `json:"max_delay,omitempty"`
	Diarization    string    `json:"diarization"`
	AdditionalVoc  []smVocab `json:"additional_vocab,omitempty"`
}

// smTranscription picks the model: "enhanced" for a known language;
// with none (auto) or two languages, melia-1's per-word "multi" mode —
// realtime has no "auto" (batch does). melia-1 takes no custom
// vocabulary or max_delay.
func smTranscription(opts Options) smTranscriptionConfig {
	if opts.Lang == "" || opts.TwoWay {
		return smTranscriptionConfig{Language: "multi", Model: "melia-1", Diarization: "speaker"}
	}
	c := smTranscriptionConfig{Language: opts.Lang, OperatingPoint: "enhanced", MaxDelay: 2, Diarization: "speaker"}
	for _, t := range opts.ContextTerms {
		if t = strings.TrimSpace(t); t != "" && len(c.AdditionalVoc) < speechmaticsMaxVocab {
			c.AdditionalVoc = append(c.AdditionalVoc, smVocab{Content: t})
		}
	}
	return c
}

// smResult is one word or punctuation mark (shared by realtime
// AddTranscript and batch json-v2).
type smResult struct {
	Type         string  `json:"type"` // word | punctuation | entity
	StartTime    float64 `json:"start_time"`
	EndTime      float64 `json:"end_time"`
	AttachesTo   string  `json:"attaches_to"` // previous | next | both | none
	IsEOS        bool    `json:"is_eos"`
	Alternatives []struct {
		Content  string `json:"content"`
		Speaker  string `json:"speaker"`
		Language string `json:"language"`
	} `json:"alternatives"`
}

func (r smResult) content() string {
	if len(r.Alternatives) == 0 {
		return ""
	}
	return r.Alternatives[0].Content
}

func (r smResult) speaker() string {
	if len(r.Alternatives) == 0 || r.Alternatives[0].Speaker == "UU" {
		return ""
	}
	return r.Alternatives[0].Speaker
}

// smLine accumulates results into text, following attaches_to so
// punctuation sits against its word.
type smLine struct {
	b        strings.Builder
	noSpace  bool // the previous result attaches to the next one
	speaker  string
	start    float64
	end      float64
	lang     string
	hasWords bool
}

func (l *smLine) add(r smResult) {
	c := r.content()
	if c == "" {
		return
	}
	if l.b.Len() > 0 && !l.noSpace && r.AttachesTo != "previous" && r.AttachesTo != "both" {
		l.b.WriteByte(' ')
	}
	l.b.WriteString(c)
	l.noSpace = r.AttachesTo == "next" || r.AttachesTo == "both"
	if !l.hasWords {
		l.start = r.StartTime
		l.hasWords = true
	}
	l.end = r.EndTime
	if len(r.Alternatives) > 0 && r.Alternatives[0].Language != "" && l.lang == "" {
		l.lang = r.Alternatives[0].Language
	}
}

func (l *smLine) text() string { return strings.TrimSpace(l.b.String()) }

// smSpeakerLabel: Speechmatics labels speakers S1, S2… → "1", "2".
func smSpeakerLabel(s string) string { return strings.TrimPrefix(s, "S") }

type smMessage struct {
	Message string     `json:"message"`
	Results []smResult `json:"results"`
	Type    string     `json:"type"`
	Reason  string     `json:"reason"`
}

// Stream implements StreamEngine.
func (e *SpeechmaticsEngine) Stream(ctx context.Context, opts Options) (chan<- PCM, <-chan Result, error) {
	conn, resp, err := e.dialer.DialContext(ctx, e.streamURL, e.header())
	if err != nil {
		return nil, nil, fmt.Errorf("speechmatics: connect: %s", handshakeError(resp, err, "Speechmatics"))
	}
	start, _ := json.Marshal(map[string]any{
		"message":              "StartRecognition",
		"audio_format":         map[string]any{"type": "raw", "encoding": "pcm_s16le", "sample_rate": 16000},
		"transcription_config": smTranscription(opts),
	})
	if err := conn.WriteMessage(websocket.TextMessage, start); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("speechmatics: start: %w", err)
	}
	if err := smAwaitStart(ctx, conn); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("speechmatics: %w", err)
	}
	e.setStatus(EngineStreaming, "")

	pcmCh := make(chan PCM)
	resultCh := make(chan Result)
	var closeOnce sync.Once
	closeConn := func() { closeOnce.Do(func() { conn.Close() }) }
	var endSent atomic.Bool

	go func() {
		seq := 0
		for chunk := range pcmCh {
			if err := conn.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				e.setStatus(EngineError, fmt.Sprintf("speechmatics: send audio: %v", err))
				return
			}
			seq++ // the server counts every binary frame as one AddAudio
		}
		endSent.Store(true)
		end, _ := json.Marshal(map[string]any{"message": "EndOfStream", "last_seq_no": seq})
		_ = conn.WriteMessage(websocket.TextMessage, end)
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
		type frame struct {
			data []byte
			err  error
		}
		frames := make(chan frame)
		go func() {
			for {
				_, data, err := conn.ReadMessage()
				select {
				case frames <- frame{data, err}:
				case <-readerDone:
					return
				}
				if err != nil {
					return
				}
			}
		}()
		var line smLine
		lastAdd := time.Now()
		flush := func() bool {
			t := line.text()
			sp := line.speaker
			line = smLine{}
			if t == "" {
				return true
			}
			select {
			case resultCh <- Result{Text: t, Speaker: smSpeakerLabel(sp), Final: true}:
				return true
			case <-ctx.Done():
				return false
			}
		}
		tick := time.NewTicker(speechmaticsLineIdle / 4)
		defer tick.Stop()
		for {
			var fr frame
			select {
			case fr = <-frames:
			case <-tick.C:
				if line.hasWords && time.Since(lastAdd) > speechmaticsLineIdle && !flush() {
					return
				}
				continue
			}
			if fr.err != nil {
				if !endSent.Load() {
					e.setStatus(EngineError, "speechmatics: connection closed: "+closeReason(fr.err))
				}
				flush()
				return
			}
			var msg smMessage
			if json.Unmarshal(fr.data, &msg) != nil {
				continue
			}
			switch msg.Message {
			case "AddTranscript":
				for _, r := range msg.Results {
					if sp := r.speaker(); sp != "" && line.hasWords && sp != line.speaker {
						if !flush() {
							return
						}
					}
					if sp := r.speaker(); sp != "" {
						line.speaker = sp
					}
					line.add(r)
					lastAdd = time.Now()
					if r.IsEOS && !flush() {
						return
					}
				}
			case "EndOfTranscript":
				flush()
				return
			case "Error":
				e.setStatus(EngineError, "speechmatics: "+smErrorText(msg))
			}
		}
	}()

	return pcmCh, resultCh, nil
}

func smErrorText(msg smMessage) string {
	lead := ""
	switch msg.Type {
	case "not_authorised":
		lead = "Speechmatics rejected the API key. "
	case "quota_exceeded":
		lead = "Speechmatics says the account is out of quota. "
	case "invalid_language":
		lead = "Speechmatics doesn't support this language live. "
	}
	return strings.TrimSpace(fmt.Sprintf("%s%s: %s", lead, msg.Type, msg.Reason))
}

func smAwaitStart(ctx context.Context, conn *websocket.Conn) error {
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	defer conn.SetReadDeadline(time.Time{})
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if ce, ok := err.(*websocket.CloseError); ok && ce.Code == 4001 {
				return fmt.Errorf("Speechmatics rejected the API key (%s)", closeReason(err))
			}
			return fmt.Errorf("recognition didn't start: %s", closeReason(err))
		}
		var msg smMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Message {
		case "RecognitionStarted":
			return nil
		case "Error":
			return fmt.Errorf("%s", smErrorText(msg))
		}
	}
}

// TranscribeFile implements BatchEngine: create a job, poll it, fetch
// the json-v2 transcript, then delete the job (Speechmatics otherwise
// keeps it 7 days).
func (e *SpeechmaticsEngine) TranscribeFile(ctx context.Context, audioPath string, opts Options) ([]meeting.Utterance, error) {
	tc := map[string]any{"language": "auto", "operating_point": "enhanced", "diarization": "speaker"}
	if opts.Lang != "" && !opts.TwoWay {
		tc["language"] = opts.Lang
	}
	var vocab []smVocab
	for _, t := range opts.ContextTerms {
		if t = strings.TrimSpace(t); t != "" && len(vocab) < speechmaticsMaxVocab {
			vocab = append(vocab, smVocab{Content: t})
		}
	}
	if len(vocab) > 0 {
		tc["additional_vocab"] = vocab
	}
	cfg, _ := json.Marshal(map[string]any{"type": "transcription", "transcription_config": tc})

	id, err := e.createJob(ctx, audioPath, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(cctx, http.MethodDelete, e.batchURL+"/jobs/"+id+"?force=true", nil)
		req.Header = e.header()
		if resp, err := e.http.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	for {
		var st struct {
			Job struct {
				Status string `json:"status"`
				Errors []struct {
					Message string `json:"message"`
				} `json:"errors"`
			} `json:"job"`
		}
		if err := e.getJSON(ctx, "/jobs/"+id, &st); err != nil {
			return nil, err
		}
		switch st.Job.Status {
		case "done":
			var tr struct {
				Results []smResult `json:"results"`
			}
			if err := e.getJSON(ctx, "/jobs/"+id+"/transcript?format=json-v2", &tr); err != nil {
				return nil, err
			}
			return smUtterances(tr.Results), nil
		case "rejected", "deleted", "expired":
			msg := st.Job.Status
			if len(st.Job.Errors) > 0 {
				msg += ": " + st.Job.Errors[0].Message
			}
			return nil, fmt.Errorf("speechmatics: job %s", msg)
		}
		select {
		case <-time.After(e.poll):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (e *SpeechmaticsEngine) createJob(ctx context.Context, audioPath string, cfg []byte) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := mw.WriteField("config", string(cfg))
		if err == nil {
			var part io.Writer
			part, err = mw.CreateFormFile("data_file", filepath.Base(audioPath))
			if err == nil {
				_, err = io.Copy(part, f)
			}
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.batchURL+"/jobs", pr)
	if err != nil {
		return "", err
	}
	req.Header = e.header()
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := e.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("speechmatics: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("speechmatics: %s", httpErrorText(resp.StatusCode, raw, "Speechmatics"))
	}
	var out struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &out) != nil || out.ID == "" {
		return "", fmt.Errorf("speechmatics: no job id in %s", bytes.TrimSpace(raw))
	}
	return out.ID, nil
}

func (e *SpeechmaticsEngine) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.batchURL+path, nil)
	if err != nil {
		return err
	}
	req.Header = e.header()
	resp, err := e.http.Do(req)
	if err != nil {
		return fmt.Errorf("speechmatics: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("speechmatics: %s", httpErrorText(resp.StatusCode, raw, "Speechmatics"))
	}
	return json.Unmarshal(raw, out)
}

// smUtterances groups batch results into lines: a new one at a speaker
// change, or at a sentence end followed by a pause.
func smUtterances(results []smResult) []meeting.Utterance {
	var out []meeting.Utterance
	var line smLine
	flush := func() {
		if t := line.text(); t != "" {
			at := time.Time{}.Add(secondsDuration(line.start))
			end := time.Time{}.Add(secondsDuration(line.end))
			u := meeting.Utterance{Seq: int64(len(out) + 1), At: at, End: &end, Text: t, Lang: line.lang}
			if line.speaker != "" {
				u.Speaker = "Speaker " + smSpeakerLabel(line.speaker)
			}
			out = append(out, u)
		}
		line = smLine{}
	}
	pendingEOS := false
	for _, r := range results {
		sp := r.speaker()
		if line.hasWords && r.Type == "word" && ((sp != "" && sp != line.speaker) || (pendingEOS && r.StartTime-line.end > 0.8)) {
			flush()
		}
		if r.Type == "word" {
			pendingEOS = false
		}
		if sp != "" && !line.hasWords {
			line.speaker = sp
		}
		line.add(r)
		if r.IsEOS {
			pendingEOS = true
		}
	}
	flush()
	return out
}

// VerifyKey checks the key by listing (at most one) batch job.
func (e *SpeechmaticsEngine) VerifyKey(ctx context.Context) error {
	var out map[string]any
	err := e.getJSON(ctx, "/jobs?limit=1", &out)
	if err != nil {
		return fmt.Errorf("%s", strings.TrimPrefix(err.Error(), "speechmatics: "))
	}
	return nil
}
