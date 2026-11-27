package stt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/catchm/internal/meeting"
)

// Deepgram Nova-3 (docs/plans/active/cloud-mode-providers.md#deepgram):
// realtime over a WebSocket with diarization, files over one REST call.
// No translation — the pipeline's translation provider handles that.
const (
	DeepgramStreamURL = "wss://api.deepgram.com/v1/listen"
	DeepgramRESTURL   = "https://api.deepgram.com/v1"

	// deepgramMaxKeyterms keeps keyterm prompting inside Deepgram's 500
	// token budget; its docs recommend 20–50 terms.
	deepgramMaxKeyterms = 50
	// deepgramKeepAlive: Deepgram closes a socket that has received
	// nothing for 10 s (NET-0001). The mixer normally streams zeros
	// through silence, so this only matters if the audio stalls.
	deepgramKeepAlive = 5 * time.Second
)

// DeepgramEngine implements StreamEngine and BatchEngine for Deepgram.
type DeepgramEngine struct {
	apiKey    string
	streamURL string
	restURL   string
	dialer    *websocket.Dialer
	http      *http.Client
	status    atomic.Pointer[engineStatus]
}

// engineStatus is the immutable EngineStatus snapshot (see sonioxStatus
// for why it's an atomic pointer rather than a mutex).
type engineStatus struct {
	state EngineState
	err   string
}

// NewDeepgramEngine returns an engine for apiKey against Deepgram's
// production endpoints.
func NewDeepgramEngine(apiKey string) *DeepgramEngine {
	return newDeepgramEngine(apiKey, DeepgramStreamURL, DeepgramRESTURL)
}

// newDeepgramEngine lets tests point both endpoints at fakes.
func newDeepgramEngine(apiKey, streamURL, restURL string) *DeepgramEngine {
	return &DeepgramEngine{apiKey: apiKey, streamURL: streamURL, restURL: restURL, dialer: websocket.DefaultDialer, http: http.DefaultClient}
}

func (e *DeepgramEngine) SupportsDiarization() bool { return true }
func (e *DeepgramEngine) SupportsTranslation() bool { return false }

// EngineStatus implements EngineStatusReporter.
func (e *DeepgramEngine) EngineStatus() (EngineState, string) {
	if s := e.status.Load(); s != nil {
		return s.state, s.err
	}
	return "", ""
}

func (e *DeepgramEngine) setStatus(state EngineState, err string) {
	e.status.Store(&engineStatus{state: state, err: err})
}

func (e *DeepgramEngine) authHeader() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Token "+e.apiKey)
	return h
}

// deepgramLanguage picks the language parameter. Streaming has no
// language detection, so an unset language (auto) and two-way meetings
// use Nova-3's code-switching "multi" model instead.
func deepgramLanguage(opts Options) string {
	if opts.Lang == "" || opts.TwoWay {
		return "multi"
	}
	return opts.Lang
}

func deepgramKeyterms(q url.Values, terms []string) {
	n := 0
	for _, t := range terms {
		t = strings.TrimSpace(strings.ReplaceAll(t, ",", " "))
		if t == "" {
			continue
		}
		q.Add("keyterm", t)
		if n++; n == deepgramMaxKeyterms {
			return
		}
	}
}

// deepgramWord is one recognized word; Speaker is present with
// diarize=true (0-based).
type deepgramWord struct {
	Word           string  `json:"word"`
	PunctuatedWord string  `json:"punctuated_word"`
	Start          float64 `json:"start"`
	End            float64 `json:"end"`
	Speaker        *int    `json:"speaker"`
	Language       string  `json:"language"`
}

type deepgramAlternative struct {
	Transcript string         `json:"transcript"`
	Words      []deepgramWord `json:"words"`
}

// deepgramEnvelope reads just the type: the messages disagree on
// "channel" (an object in Results, an array in UtteranceEnd and
// SpeechStarted), so each type is decoded on its own.
type deepgramEnvelope struct {
	Type string `json:"type"`
}

type deepgramResults struct {
	IsFinal     bool `json:"is_final"`
	SpeechFinal bool `json:"speech_final"`
	Channel     struct {
		Alternatives []deepgramAlternative `json:"alternatives"`
	} `json:"channel"`
}

type deepgramError struct {
	Description string `json:"description"`
	Message     string `json:"message"`
}

// Stream implements StreamEngine. Deepgram sends interim and final
// segments; the pipeline only keeps finals, so this collects is_final
// segments into an utterance and emits it (split by speaker) on
// speech_final or UtteranceEnd — Deepgram's docs warn that
// speech_final alone misses utterances that end without a pause.
func (e *DeepgramEngine) Stream(ctx context.Context, opts Options) (chan<- PCM, <-chan Result, error) {
	q := url.Values{}
	q.Set("model", "nova-3")
	q.Set("language", deepgramLanguage(opts))
	q.Set("encoding", "linear16")
	q.Set("sample_rate", "16000")
	q.Set("channels", "1")
	q.Set("punctuate", "true")
	q.Set("smart_format", "true")
	q.Set("diarize", "true")
	q.Set("interim_results", "true") // required by utterance_end_ms
	q.Set("utterance_end_ms", "1000")
	q.Set("endpointing", "300")
	deepgramKeyterms(q, opts.ContextTerms)

	conn, resp, err := e.dialer.DialContext(ctx, e.streamURL+"?"+q.Encode(), e.authHeader())
	if err != nil {
		return nil, nil, fmt.Errorf("deepgram: connect: %s", handshakeError(resp, err, "Deepgram"))
	}
	e.setStatus(EngineStreaming, "")

	pcmCh := make(chan PCM)
	resultCh := make(chan Result)
	var writeMu sync.Mutex // the writer and keepalive share the socket
	write := func(kind int, b []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(kind, b)
	}
	var closeOnce sync.Once
	closeConn := func() { closeOnce.Do(func() { conn.Close() }) }
	var endSent atomic.Bool

	go func() {
		keepalive := time.NewTicker(deepgramKeepAlive)
		defer keepalive.Stop()
		lastAudio := time.Now()
		for {
			select {
			case chunk, ok := <-pcmCh:
				if !ok {
					endSent.Store(true)
					// Deepgram finishes the buffered audio, sends the last
					// Results and Metadata, then closes the socket itself.
					_ = write(websocket.TextMessage, []byte(`{"type":"CloseStream"}`))
					return
				}
				if err := write(websocket.BinaryMessage, chunk); err != nil {
					e.setStatus(EngineError, fmt.Sprintf("deepgram: send audio: %v", err))
					return
				}
				lastAudio = time.Now()
			case <-keepalive.C:
				if time.Since(lastAudio) >= deepgramKeepAlive {
					_ = write(websocket.TextMessage, []byte(`{"type":"KeepAlive"}`))
				}
			}
		}
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
		var pending []deepgramWord
		var pendingText []string // transcripts, used when words carry no speakers
		flush := func() bool {
			for _, r := range deepgramUtterances(pending, pendingText) {
				select {
				case resultCh <- r:
				case <-ctx.Done():
					return false
				}
			}
			pending, pendingText = nil, nil
			return true
		}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				if !endSent.Load() {
					e.setStatus(EngineError, "deepgram: connection closed: "+closeReason(err))
				}
				flush()
				return
			}
			var env deepgramEnvelope
			if json.Unmarshal(data, &env) != nil {
				continue
			}
			switch env.Type {
			case "Results":
				var msg deepgramResults
				if json.Unmarshal(data, &msg) != nil {
					continue
				}
				if msg.IsFinal && len(msg.Channel.Alternatives) > 0 {
					alt := msg.Channel.Alternatives[0]
					if strings.TrimSpace(alt.Transcript) != "" {
						pending = append(pending, alt.Words...)
						pendingText = append(pendingText, alt.Transcript)
					}
				}
				if msg.SpeechFinal && !flush() {
					return
				}
			case "UtteranceEnd":
				if !flush() {
					return
				}
			case "Error":
				var msg deepgramError
				_ = json.Unmarshal(data, &msg)
				reason := msg.Description
				if reason == "" {
					reason = msg.Message
				}
				e.setStatus(EngineError, "deepgram: "+reason)
			}
		}
	}()

	return pcmCh, resultCh, nil
}

// deepgramUtterances turns one utterance's final words into Results,
// one per run of the same speaker. Speakers are numbered from 1, like
// Soniox's. Without speaker labels the formatted transcripts are joined
// as-is (they keep Deepgram's smart formatting).
func deepgramUtterances(words []deepgramWord, transcripts []string) []Result {
	if len(transcripts) == 0 {
		return nil
	}
	diarized := false
	for _, w := range words {
		if w.Speaker != nil {
			diarized = true
			break
		}
	}
	if !diarized {
		text := strings.TrimSpace(strings.Join(transcripts, " "))
		if text == "" {
			return nil
		}
		return []Result{{Text: text, Final: true}}
	}
	var out []Result
	var cur strings.Builder
	speaker := -1
	emit := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, Result{Text: t, Speaker: fmt.Sprint(speaker + 1), Final: true})
		}
		cur.Reset()
	}
	for _, w := range words {
		s := speaker
		if w.Speaker != nil {
			s = *w.Speaker
		}
		if s != speaker && cur.Len() > 0 {
			emit()
		}
		speaker = s
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		if w.PunctuatedWord != "" {
			cur.WriteString(w.PunctuatedWord)
		} else {
			cur.WriteString(w.Word)
		}
	}
	emit()
	return out
}

// deepgramPrerecorded is the part of a pre-recorded response we use.
type deepgramPrerecorded struct {
	Results struct {
		Channels []struct {
			DetectedLanguage string `json:"detected_language"`
		} `json:"channels"`
		Utterances []struct {
			Start      float64 `json:"start"`
			End        float64 `json:"end"`
			Transcript string  `json:"transcript"`
			Speaker    *int    `json:"speaker"`
		} `json:"utterances"`
	} `json:"results"`
}

// TranscribeFile implements BatchEngine: one POST with the file bytes.
// Deepgram detects the format; requests that take over 10 minutes to
// process fail with 504, which shows up as the error.
func (e *DeepgramEngine) TranscribeFile(ctx context.Context, audioPath string, opts Options) ([]meeting.Utterance, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	q := url.Values{}
	q.Set("model", "nova-3")
	if opts.Lang != "" && !opts.TwoWay {
		q.Set("language", opts.Lang)
	} else {
		q.Set("detect_language", "true")
	}
	q.Set("punctuate", "true")
	q.Set("smart_format", "true")
	q.Set("diarize", "true")
	q.Set("utterances", "true")
	deepgramKeyterms(q, opts.ContextTerms)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.restURL+"/listen?"+q.Encode(), f)
	if err != nil {
		return nil, err
	}
	req.Header = e.authHeader()
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(audioPath)))
	if ct == "" {
		ct = "application/octet-stream"
	}
	req.Header.Set("Content-Type", ct)
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepgram: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepgram: %s", httpErrorText(resp.StatusCode, raw, "Deepgram"))
	}
	var pr deepgramPrerecorded
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, fmt.Errorf("deepgram: decode response: %w", err)
	}
	lang := ""
	if len(pr.Results.Channels) > 0 {
		lang = pr.Results.Channels[0].DetectedLanguage
	}
	var out []meeting.Utterance
	for _, u := range pr.Results.Utterances {
		text := strings.TrimSpace(u.Transcript)
		if text == "" {
			continue
		}
		at := time.Time{}.Add(secondsDuration(u.Start))
		end := time.Time{}.Add(secondsDuration(u.End))
		ut := meeting.Utterance{Seq: int64(len(out) + 1), At: at, End: &end, Text: text, Lang: lang}
		if u.Speaker != nil {
			ut.Speaker = fmt.Sprintf("Speaker %d", *u.Speaker+1)
		}
		out = append(out, ut)
	}
	return out, nil
}

// VerifyKey checks the key with a free, read-only call.
func (e *DeepgramEngine) VerifyKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.restURL+"/projects", nil)
	if err != nil {
		return err
	}
	req.Header = e.authHeader()
	resp, err := e.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return fmt.Errorf("%s", httpErrorText(resp.StatusCode, raw, "Deepgram"))
}

func secondsDuration(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// handshakeError explains a failed WebSocket upgrade: the provider's own
// message when it sent one (401 bad key, 402 out of credit, 429 too
// many sessions), else the dial error.
func handshakeError(resp *http.Response, err error, provider string) string {
	if resp == nil {
		return err.Error()
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return httpErrorText(resp.StatusCode, raw, provider)
}

// httpErrorText renders a provider's error response: a plain-language
// lead for the statuses users can act on, then the provider's message.
func httpErrorText(status int, raw []byte, provider string) string {
	var body struct {
		ErrMsg  string `json:"err_msg"`
		Message string `json:"message"`
		Detail  any    `json:"detail"`
		Error   any    `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	msg := body.ErrMsg
	if msg == "" {
		msg = body.Message
	}
	if msg == "" {
		if s, ok := body.Error.(string); ok {
			msg = s
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
	}
	lead := ""
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		lead = provider + " rejected the API key. "
	case http.StatusPaymentRequired:
		lead = provider + " says the account is out of credit. "
	case http.StatusTooManyRequests:
		lead = provider + " is limiting requests (too many at once). "
	}
	if msg == "" {
		return fmt.Sprintf("%sstatus %d", lead, status)
	}
	return fmt.Sprintf("%sstatus %d: %s", lead, status, msg)
}

// closeReason renders a read error, keeping a WebSocket close frame's
// code and reason (e.g. Deepgram's "NET-0001").
func closeReason(err error) string {
	if ce, ok := err.(*websocket.CloseError); ok {
		if ce.Text != "" {
			return fmt.Sprintf("%d %s", ce.Code, ce.Text)
		}
		return fmt.Sprint(ce.Code)
	}
	return err.Error()
}
