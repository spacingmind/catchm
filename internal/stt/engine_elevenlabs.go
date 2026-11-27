package stt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
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

// ElevenLabs Scribe (docs/plans/active/cloud-mode-providers.md#elevenlabs):
// realtime with scribe_v3_live (the only Scribe model that separates
// speakers live), files with scribe_v3. No translation.
const (
	ElevenLabsStreamURL = "wss://api.elevenlabs.io/v1/speech-to-text/realtime"
	ElevenLabsRESTURL   = "https://api.elevenlabs.io/v1"

	elevenLabsLiveModel  = "scribe_v3_live"
	elevenLabsBatchModel = "scribe_v3"
	// The realtime API wants 0.1–1 s per message; the pipeline sends
	// 20 ms frames, so ten are sent together.
	elevenLabsFramesPerChunk = 10
	elevenLabsMaxKeyterms    = 100
	elevenLabsKeytermChars   = 1000
	// A diarized_transcript follows its committed_transcript; if it
	// hasn't come by then, the line goes out without a speaker.
	elevenLabsDiarizeWait = 2 * time.Second
)

// ElevenLabsEngine implements StreamEngine and BatchEngine for Scribe.
type ElevenLabsEngine struct {
	apiKey    string
	streamURL string
	restURL   string
	dialer    *websocket.Dialer
	http      *http.Client
	status    atomic.Pointer[engineStatus]
}

func NewElevenLabsEngine(apiKey string) *ElevenLabsEngine {
	return newElevenLabsEngine(apiKey, ElevenLabsStreamURL, ElevenLabsRESTURL)
}

func newElevenLabsEngine(apiKey, streamURL, restURL string) *ElevenLabsEngine {
	return &ElevenLabsEngine{apiKey: apiKey, streamURL: streamURL, restURL: restURL, dialer: websocket.DefaultDialer, http: http.DefaultClient}
}

func (e *ElevenLabsEngine) SupportsDiarization() bool { return true }
func (e *ElevenLabsEngine) SupportsTranslation() bool { return false }

func (e *ElevenLabsEngine) EngineStatus() (EngineState, string) {
	if s := e.status.Load(); s != nil {
		return s.state, s.err
	}
	return "", ""
}

func (e *ElevenLabsEngine) setStatus(state EngineState, err string) {
	e.status.Store(&engineStatus{state: state, err: err})
}

func (e *ElevenLabsEngine) header() http.Header {
	h := http.Header{}
	h.Set("xi-api-key", e.apiKey)
	return h
}

// elevenLabsLang maps CatchM's ISO 639-1 codes to ISO 639-3, which the
// API accepts everywhere (its language list uses 639-3, e.g. "vie").
func elevenLabsLang(code string) string {
	if c, ok := iso6393[code]; ok {
		return c
	}
	return code
}

var iso6393 = map[string]string{
	"en": "eng", "vi": "vie", "es": "spa", "fr": "fra", "de": "deu", "it": "ita", "pt": "por",
	"nl": "nld", "ru": "rus", "uk": "ukr", "pl": "pol", "tr": "tur", "ar": "ara", "fa": "fas",
	"hi": "hin", "bn": "ben", "th": "tha", "id": "ind", "ms": "msa", "zh": "zho", "ja": "jpn", "ko": "kor",
}

// iso6391 maps a 639-3 code back to CatchM's 639-1 (unknown codes pass
// through), so stored utterances use one code style across engines.
func iso6391(code string) string {
	for two, three := range iso6393 {
		if three == code {
			return two
		}
	}
	return code
}

func elevenLabsKeyterms(add func(string), terms []string) {
	n, chars := 0, 0
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" || chars+len(t) > elevenLabsKeytermChars {
			continue
		}
		add(t)
		chars += len(t)
		if n++; n == elevenLabsMaxKeyterms {
			return
		}
	}
}

type elevenLabsWord struct {
	Text      string  `json:"text"`
	Type      string  `json:"type"` // word | spacing | audio_event
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	SpeakerID *string `json:"speaker_id"`
}

type elevenLabsMessage struct {
	MessageType  string           `json:"message_type"`
	Text         string           `json:"text"`
	SegmentIndex *int             `json:"segment_index"`
	Words        []elevenLabsWord `json:"words"`
	From         string           `json:"from_speaker_id"`
	To           string           `json:"to_speaker_id"`
	Error        string           `json:"error"`
}

// elevenLabsErrorTypes are the message_types that carry an error and
// precede the server closing the socket.
var elevenLabsErrorTypes = map[string]bool{
	"error": true, "auth_error": true, "quota_exceeded": true, "rate_limited": true,
	"commit_throttled": true, "unaccepted_terms": true, "queue_overflow": true,
	"resource_exhausted": true, "session_time_limit_exceeded": true, "input_error": true,
	"invalid_request": true, "chunk_size_exceeded": true, "insufficient_audio_activity": true,
	"transcriber_error": true,
}

type elevenLabsChunk struct {
	MessageType string `json:"message_type"`
	Audio       string `json:"audio_base_64"`
	Commit      bool   `json:"commit"`
	SampleRate  int    `json:"sample_rate"`
}

// Stream implements StreamEngine. The server commits a transcript at
// each pause (commit_strategy=vad); committed text is final. With
// diarization, each committed_transcript is held until the
// diarized_transcript with the same segment_index names its speakers.
func (e *ElevenLabsEngine) Stream(ctx context.Context, opts Options) (chan<- PCM, <-chan Result, error) {
	q := url.Values{}
	q.Set("model_id", elevenLabsLiveModel)
	q.Set("audio_format", "pcm_16000")
	q.Set("commit_strategy", "vad")
	q.Set("diarize", "true")
	if opts.Lang != "" && !opts.TwoWay {
		q.Set("language_code", elevenLabsLang(opts.Lang))
	}
	elevenLabsKeyterms(func(t string) { q.Add("keyterms", t) }, opts.ContextTerms)

	conn, resp, err := e.dialer.DialContext(ctx, e.streamURL+"?"+q.Encode(), e.header())
	if err != nil {
		return nil, nil, fmt.Errorf("elevenlabs: connect: %s", handshakeError(resp, err, "ElevenLabs"))
	}
	// The session must start before audio is accepted; an auth or quota
	// problem shows up here as an error message instead.
	if err := elevenLabsAwaitStart(ctx, conn); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("elevenlabs: %w", err)
	}
	e.setStatus(EngineStreaming, "")

	pcmCh := make(chan PCM)
	resultCh := make(chan Result)
	var closeOnce sync.Once
	closeConn := func() { closeOnce.Do(func() { conn.Close() }) }
	var endSent atomic.Bool
	lastCommitted := make(chan struct{}) // closed by the reader after the final commit's transcript

	go func() {
		var buf bytes.Buffer
		frames := 0
		send := func(commit bool) error {
			msg := elevenLabsChunk{MessageType: "input_audio_chunk", Audio: base64.StdEncoding.EncodeToString(buf.Bytes()), Commit: commit, SampleRate: 16000}
			buf.Reset()
			frames = 0
			b, _ := json.Marshal(msg)
			return conn.WriteMessage(websocket.TextMessage, b)
		}
		for chunk := range pcmCh {
			buf.Write(chunk)
			if frames++; frames < elevenLabsFramesPerChunk {
				continue
			}
			if err := send(false); err != nil {
				e.setStatus(EngineError, fmt.Sprintf("elevenlabs: send audio: %v", err))
				return
			}
		}
		endSent.Store(true)
		// Flush what's buffered and commit, so the last words come back
		// before the socket closes.
		_ = send(true)
		select {
		case <-lastCommitted:
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
		}
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		time.AfterFunc(time.Second, closeConn)
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
		var signalOnce sync.Once
		signalLast := func() { signalOnce.Do(func() { close(lastCommitted) }) }
		defer signalLast()

		merged := map[string]string{} // speakers_merged: from → to
		type held struct {
			text  string
			since time.Time
		}
		pending := map[int]held{} // committed segments awaiting their speakers
		emit := func(rs ...Result) bool {
			for _, r := range rs {
				select {
				case resultCh <- r:
				case <-ctx.Done():
					return false
				}
			}
			return true
		}
		// flushStale releases committed lines whose diarization never came.
		flushStale := func(all bool) bool {
			for idx, h := range pending {
				if all || time.Since(h.since) > elevenLabsDiarizeWait {
					delete(pending, idx)
					if !emit(Result{Text: h.text, Final: true}) {
						return false
					}
				}
			}
			return true
		}
		// Reads go through a channel so held lines can be released on a
		// timer: a read deadline would break gorilla's connection.
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
		tick := time.NewTicker(elevenLabsDiarizeWait / 4)
		defer tick.Stop()
		for {
			var fr frame
			select {
			case fr = <-frames:
			case <-tick.C:
				if !flushStale(false) {
					return
				}
				continue
			}
			data, err := fr.data, fr.err
			if err != nil {
				if !endSent.Load() {
					e.setStatus(EngineError, "elevenlabs: connection closed: "+closeReason(err))
				}
				flushStale(true)
				return
			}
			var msg elevenLabsMessage
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			switch {
			case elevenLabsErrorTypes[msg.MessageType]:
				e.setStatus(EngineError, "elevenlabs: "+msg.MessageType+": "+msg.Error)
			case msg.MessageType == "committed_transcript" || msg.MessageType == "committed_transcript_with_timestamps":
				text := strings.TrimSpace(msg.Text)
				if text != "" {
					if msg.SegmentIndex != nil {
						pending[*msg.SegmentIndex] = held{text: text, since: time.Now()}
					} else if !emit(Result{Text: text, Final: true}) {
						return
					}
				}
				if endSent.Load() {
					signalLast()
				}
			case msg.MessageType == "diarized_transcript" && msg.SegmentIndex != nil:
				h, ok := pending[*msg.SegmentIndex]
				if !ok {
					continue
				}
				delete(pending, *msg.SegmentIndex)
				rs := elevenLabsSpeakerRuns(msg.Words, merged)
				if len(rs) == 0 {
					rs = []Result{{Text: h.text, Final: true}}
				}
				if !emit(rs...) {
					return
				}
			case msg.MessageType == "speakers_merged" && msg.From != "":
				merged[msg.From] = msg.To
			}
			if !flushStale(false) {
				return
			}
		}
	}()

	return pcmCh, resultCh, nil
}

func elevenLabsAwaitStart(ctx context.Context, conn *websocket.Conn) error {
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	defer conn.SetReadDeadline(time.Time{})
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("session didn't start: %s", closeReason(err))
		}
		var msg elevenLabsMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.MessageType == "session_started" {
			return nil
		}
		if elevenLabsErrorTypes[msg.MessageType] {
			lead := ""
			switch msg.MessageType {
			case "auth_error":
				lead = "ElevenLabs rejected the API key. "
			case "quota_exceeded":
				lead = "ElevenLabs says the account is out of credit. "
			case "rate_limited", "resource_exhausted":
				lead = "ElevenLabs is limiting requests (too many at once). "
			}
			return fmt.Errorf("%s%s: %s", lead, msg.MessageType, msg.Error)
		}
	}
}

// elevenLabsSpeakerRuns turns a diarized segment's words into one Result
// per run of the same speaker ("speaker_0" → "1"), following merges.
func elevenLabsSpeakerRuns(words []elevenLabsWord, merged map[string]string) []Result {
	var out []Result
	var cur strings.Builder
	speaker := ""
	emit := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, Result{Text: t, Speaker: elevenLabsSpeakerLabel(speaker), Final: true})
		}
		cur.Reset()
	}
	for _, w := range words {
		if w.Type == "audio_event" {
			continue
		}
		if w.Type == "word" && w.SpeakerID != nil {
			s := resolveMerge(*w.SpeakerID, merged)
			if s != speaker && strings.TrimSpace(cur.String()) != "" {
				emit()
			}
			speaker = s
		}
		cur.WriteString(w.Text)
	}
	emit()
	return out
}

func resolveMerge(id string, merged map[string]string) string {
	for i := 0; i < 32; i++ { // merges chain; bounded against a cycle
		to, ok := merged[id]
		if !ok {
			return id
		}
		id = to
	}
	return id
}

// elevenLabsSpeakerLabel: "speaker_0" → "1", matching Soniox/Deepgram.
func elevenLabsSpeakerLabel(id string) string {
	var n int
	if _, err := fmt.Sscanf(id, "speaker_%d", &n); err == nil {
		return fmt.Sprint(n + 1)
	}
	return id
}

type elevenLabsBatchResponse struct {
	LanguageCode string           `json:"language_code"`
	Words        []elevenLabsWord `json:"words"`
}

// TranscribeFile implements BatchEngine: one multipart POST.
func (e *ElevenLabsEngine) TranscribeFile(ctx context.Context, audioPath string, opts Options) ([]meeting.Utterance, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		fields := map[string]string{"model_id": elevenLabsBatchModel, "diarize": "true", "timestamps_granularity": "word", "tag_audio_events": "false"}
		if opts.Lang != "" && !opts.TwoWay {
			fields["language_code"] = elevenLabsLang(opts.Lang)
		}
		for k, v := range fields {
			_ = mw.WriteField(k, v)
		}
		elevenLabsKeyterms(func(t string) { _ = mw.WriteField("keyterms", t) }, opts.ContextTerms)
		part, err := mw.CreateFormFile("file", filepath.Base(audioPath))
		if err == nil {
			_, err = io.Copy(part, f)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.restURL+"/speech-to-text", pr)
	if err != nil {
		return nil, err
	}
	req.Header = e.header()
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("elevenlabs: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("elevenlabs: %s", httpErrorText(resp.StatusCode, raw, "ElevenLabs"))
	}
	var br elevenLabsBatchResponse
	if err := json.Unmarshal(raw, &br); err != nil {
		return nil, fmt.Errorf("elevenlabs: decode response: %w", err)
	}
	return groupWords(br.Words, iso6391(br.LanguageCode)), nil
}

// groupWords builds utterances from word timings: a new one starts when
// the speaker changes, or after a sentence end followed by a pause.
func groupWords(words []elevenLabsWord, lang string) []meeting.Utterance {
	var out []meeting.Utterance
	var text strings.Builder
	var start, end float64
	speaker := ""
	open := false
	flush := func() {
		if t := strings.TrimSpace(text.String()); t != "" {
			at := time.Time{}.Add(secondsDuration(start))
			stop := time.Time{}.Add(secondsDuration(end))
			u := meeting.Utterance{Seq: int64(len(out) + 1), At: at, End: &stop, Text: t, Lang: lang}
			if speaker != "" {
				u.Speaker = "Speaker " + elevenLabsSpeakerLabel(speaker)
			}
			out = append(out, u)
		}
		text.Reset()
		open = false
	}
	for _, w := range words {
		switch w.Type {
		case "audio_event":
			continue
		case "spacing":
			if open {
				text.WriteString(w.Text)
			}
			continue
		}
		s := ""
		if w.SpeakerID != nil {
			s = *w.SpeakerID
		}
		sentenceEnd := strings.HasSuffix(strings.TrimSpace(text.String()), ".") || strings.HasSuffix(strings.TrimSpace(text.String()), "?") || strings.HasSuffix(strings.TrimSpace(text.String()), "!")
		if open && (s != speaker || (sentenceEnd && w.Start-end > 0.8)) {
			flush()
		}
		if !open {
			start, speaker, open = w.Start, s, true
		}
		text.WriteString(w.Text)
		end = w.End
	}
	flush()
	return out
}

// VerifyKey checks the key with the free account lookup. A key scoped
// without the user-read permission fails here too; the message says so.
func (e *ElevenLabsEngine) VerifyKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.restURL+"/user", nil)
	if err != nil {
		return err
	}
	req.Header = e.header()
	resp, err := e.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	return fmt.Errorf("%s", httpErrorText(resp.StatusCode, raw, "ElevenLabs"))
}
