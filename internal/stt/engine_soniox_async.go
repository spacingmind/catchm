// SonioxAsyncEngine implements BatchEngine against Soniox's async
// (file) transcription REST API.
//
// REST details this engine is coded against (all under the base URL,
// default https://api.soniox.com, authenticated with a single
// "Authorization: Bearer <key>" header on every request):
//
//	POST   /v1/files                                  multipart/form-data, field "file"
//	                                                    (+ optional client_reference_id);
//	                                                    201: {"id","filename","size","created_at","client_reference_id"}
//	                                                    https://soniox.com/docs/stt/api-reference/files/upload_file
//	GET    /v1/files                                  {"files":[{...}],"next_page_cursor"}
//	                                                    https://soniox.com/docs/stt/api-reference/files/get_files
//	DELETE /v1/files/{id}                             https://soniox.com/docs/stt/api-reference/files/delete_file
//	POST   /v1/transcriptions                         JSON body: model, file_id, language_hints,
//	                                                    enable_speaker_diarization, enable_language_identification,
//	                                                    translation {type:one_way|two_way, target_language|language_a+language_b},
//	                                                    context {general:[{key,value}], terms:[...]},
//	                                                    client_reference_id;
//	                                                    201: {"id","status":"queued"|"processing"|"completed"|"error",...}
//	                                                    https://soniox.com/docs/stt/api-reference/transcriptions/create_transcription
//	GET    /v1/transcriptions                         {"transcriptions":[{id,status,created_at,client_reference_id,...}],"next_page_cursor"}
//	                                                    https://soniox.com/docs/stt/api-reference/transcriptions/get_transcriptions
//	DELETE /v1/transcriptions/{id}                    https://soniox.com/docs/stt/api-reference/transcriptions/delete_transcription
//	GET    /v1/transcriptions/{id}/transcript         {"id","text","tokens":[{text,start_ms,end_ms,confidence,
//	                                                    speaker,language,translation_status:"none"|"original"|"translation",...}]}
//	                                                    https://soniox.com/docs/stt/api-reference/transcriptions/get_transcription_transcript
//
// Token field semantics match the realtime engine (engine_soniox.go):
// translation_status "translation" tokens carry the translated text and
// no timestamps; original/none tokens carry the spoken text with
// start_ms/end_ms. Translation config shape:
// https://soniox.com/docs/translation/stt-translation (one_way / two_way).
// Context shape: https://soniox.com/docs/stt/concepts/context
// (general key/value items plus a terms list). Limits: files up to
// 300 minutes, files and transcriptions auto-deleted after 30 days, so
// uploads are deleted as soon as a result is fetched:
// https://soniox.com/docs/stt/async/limits-and-quotas
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spacingmind/catchm/internal/meeting"
)

// sonioxAsyncDefaultBaseURL is the Soniox REST API root used when the
// configured base URL is empty.
const sonioxAsyncDefaultBaseURL = "https://api.soniox.com"

// sonioxAsyncModel is the async STT model requested on every create.
const sonioxAsyncModel = "stt-async-v5"

// sonioxAsyncReferencePrefix tags every upload and transcription this
// engine creates, so CleanupStale can recognize its own leftovers.
const sonioxAsyncReferencePrefix = "catchm-"

// SonioxAsyncEngine transcribes whole audio files through Soniox's async
// REST API: upload, create, poll, fetch, convert, always delete.
type SonioxAsyncEngine struct {
	baseURL          string
	apiKey           string
	client           *http.Client
	pollStart        time.Duration
	pollMax          time.Duration
	now              func() time.Time
	sleepWithContext func(ctx context.Context, d time.Duration) error
}

// SonioxAsyncOption customizes a SonioxAsyncEngine.
type SonioxAsyncOption func(*SonioxAsyncEngine)

// WithSonioxAsyncClient sets the HTTP client used for all REST calls
// (tests inject one pointed at an httptest server).
func WithSonioxAsyncClient(c *http.Client) SonioxAsyncOption {
	return func(e *SonioxAsyncEngine) { e.client = c }
}

// WithSonioxAsyncPollInterval sets the initial poll interval, which
// grows by 1.5x per poll up to 5s.
func WithSonioxAsyncPollInterval(start time.Duration) SonioxAsyncOption {
	return func(e *SonioxAsyncEngine) { e.pollStart = start }
}

// WithSonioxAsyncClock lets tests control "now" (used by CleanupStale's
// age cutoff) and the poll sleep.
func WithSonioxAsyncClock(now func() time.Time, sleep func(ctx context.Context, d time.Duration) error) SonioxAsyncOption {
	return func(e *SonioxAsyncEngine) {
		if now != nil {
			e.now = now
		}
		if sleep != nil {
			e.sleepWithContext = sleep
		}
	}
}

// NewSonioxAsyncEngine returns an engine calling the Soniox REST API at
// baseURL (empty means https://api.soniox.com) with apiKey. It is not
// the realtime WebSocket URL.
func NewSonioxAsyncEngine(apiKey, baseURL string, opts ...SonioxAsyncOption) *SonioxAsyncEngine {
	e := &SonioxAsyncEngine{
		baseURL:          baseURL,
		apiKey:           apiKey,
		client:           http.DefaultClient,
		pollStart:        time.Second,
		pollMax:          5 * time.Second,
		now:              time.Now,
		sleepWithContext: sleepContext,
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.baseURL == "" {
		e.baseURL = sonioxAsyncDefaultBaseURL
	}
	return e
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// sonioxAsyncToken is one transcript token. It embeds the realtime
// engine's token shape (text, speaker, translation_status, language) and
// adds the async transcript's timestamp fields. Translated tokens have
// no timestamps (confirmed by the token-format docs).
type sonioxAsyncToken struct {
	sonioxToken
	StartMS int64 `json:"start_ms"`
	EndMS   int64 `json:"end_ms"`
}

type sonioxAsyncTranscript struct {
	Tokens []sonioxAsyncToken `json:"tokens"`
}

type sonioxAsyncTranscription struct {
	ID                string    `json:"id"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	ClientReferenceID string    `json:"client_reference_id"`
	ErrorType         string    `json:"error_type"`
	ErrorMessage      string    `json:"error_message"`
}

type sonioxAsyncFile struct {
	ID                string    `json:"id"`
	Filename          string    `json:"filename"`
	Size              int64     `json:"size"`
	CreatedAt         time.Time `json:"created_at"`
	ClientReferenceID string    `json:"client_reference_id"`
}

type sonioxAsyncContextGeneralItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type sonioxAsyncContext struct {
	General []sonioxAsyncContextGeneralItem `json:"general,omitempty"`
	Terms   []string                        `json:"terms,omitempty"`
}

type sonioxAsyncCreateRequest struct {
	Model                        string              `json:"model"`
	FileID                       string              `json:"file_id"`
	LanguageHints                []string            `json:"language_hints,omitempty"`
	EnableSpeakerDiarization     bool                `json:"enable_speaker_diarization"`
	EnableLanguageIdentification bool                `json:"enable_language_identification"`
	Translation                  *sonioxTranslation  `json:"translation,omitempty"`
	Context                      *sonioxAsyncContext `json:"context,omitempty"`
	ClientReferenceID            string              `json:"client_reference_id"`
}

// errSonioxAuth is the sentinel behind the 401 message so tests and
// callers can match it without matching prose.
var errSonioxAuth = errors.New("Soniox rejected the API key. Check it in Settings → Transcription.")

// do runs one REST call against the Soniox base URL, sending and
// receiving JSON, and maps non-2xx statuses to user-facing errors that
// never contain the API key.
func (e *SonioxAsyncEngine) do(ctx context.Context, method, path string, contentType string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, e.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("soniox: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("soniox: %w", ctx.Err())
		}
		return fmt.Errorf("soniox: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return errSonioxAuth
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("soniox: unexpected status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("soniox: decode response: %w", err)
	}
	return nil
}

// uploadFile POSTs the audio as multipart to /v1/files and returns the
// new file's id. The file is tagged with the same client_reference_id
// as the transcription, so CleanupStale can find leftovers either way.
func (e *SonioxAsyncEngine) uploadFile(ctx context.Context, audioPath, referenceID string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("open audio: %w", err)
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", err
	}
	if err := mw.WriteField("client_reference_id", referenceID); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	var out sonioxAsyncFile
	if err := e.do(ctx, http.MethodPost, "/v1/files", mw.FormDataContentType(), &body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// createTranscription POSTs /v1/transcriptions with the settings from
// Options: diarization and language identification always on, language
// hints from Lang/LanguageB, one-way or two-way translation, context
// from Title and ContextTerms.
func (e *SonioxAsyncEngine) createTranscription(ctx context.Context, fileID string, o Options, referenceID string) (string, error) {
	req := sonioxAsyncCreateRequest{
		Model:                        sonioxAsyncModel,
		FileID:                       fileID,
		EnableSpeakerDiarization:     true,
		EnableLanguageIdentification: true,
		ClientReferenceID:            referenceID,
	}
	for _, lang := range []string{o.Lang, o.LanguageB} {
		if lang != "" {
			req.LanguageHints = append(req.LanguageHints, lang)
		}
	}
	if o.TwoWay {
		req.Translation = &sonioxTranslation{
			Type:      "two_way",
			LanguageA: o.Lang,
			LanguageB: o.LanguageB,
		}
	} else if o.TranslateTo != "" {
		req.Translation = &sonioxTranslation{
			Type:           "one_way",
			TargetLanguage: o.TranslateTo,
		}
	}
	ctxc := &sonioxAsyncContext{}
	if o.Title != "" {
		ctxc.General = append(ctxc.General, sonioxAsyncContextGeneralItem{Key: "topic", Value: o.Title})
	}
	if len(o.ContextTerms) > 0 {
		ctxc.Terms = append(ctxc.Terms, o.ContextTerms...)
	}
	if len(ctxc.General) > 0 || len(ctxc.Terms) > 0 {
		req.Context = ctxc
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	var out sonioxAsyncTranscription
	if err := e.do(ctx, http.MethodPost, "/v1/transcriptions", "application/json", bytes.NewReader(body), &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// pollTranscription GETs /v1/transcriptions/{id} with a growing backoff
// (1s, ×1.5, capped at 5s) until the status is completed or error. It
// returns the terminal transcription.
func (e *SonioxAsyncEngine) pollTranscription(ctx context.Context, id string) (sonioxAsyncTranscription, error) {
	interval := e.pollStart
	for {
		var tr sonioxAsyncTranscription
		if err := e.do(ctx, http.MethodGet, "/v1/transcriptions/"+id, "", nil, &tr); err != nil {
			return tr, err
		}
		switch tr.Status {
		case "completed":
			return tr, nil
		case "error":
			msg := tr.ErrorMessage
			if msg == "" {
				msg = tr.ErrorType
			}
			if msg == "" {
				msg = "transcription failed"
			}
			return tr, fmt.Errorf("soniox: transcription failed: %s", msg)
		}
		if err := e.sleepWithContext(ctx, interval); err != nil {
			return tr, fmt.Errorf("soniox: %w", err)
		}
		interval = interval * 3 / 2
		if interval > e.pollMax {
			interval = e.pollMax
		}
	}
}

// fetchTranscript GETs /v1/transcriptions/{id}/transcript.
func (e *SonioxAsyncEngine) fetchTranscript(ctx context.Context, id string) (sonioxAsyncTranscript, error) {
	var out sonioxAsyncTranscript
	if err := e.do(ctx, http.MethodGet, "/v1/transcriptions/"+id+"/transcript", "", nil, &out); err != nil {
		return out, err
	}
	return out, nil
}

// cleanup deletes the transcription and then the file. It always runs
// with a fresh short-timeout context, so cleanup still happens when the
// caller's ctx was cancelled. Errors are ignored (best effort), but the
// key never leaks because do() never embeds it in an error.
func (e *SonioxAsyncEngine) cleanup(transcriptionID, fileID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if transcriptionID != "" {
		_ = e.do(ctx, http.MethodDelete, "/v1/transcriptions/"+transcriptionID, "", nil, nil)
	}
	if fileID != "" {
		_ = e.do(ctx, http.MethodDelete, "/v1/files/"+fileID, "", nil, nil)
	}
}

// utteranceGap is the pause between two original tokens that ends the
// current utterance (~1.2s).
const utteranceGap = 1200 * time.Millisecond

// groupTokens converts the async transcript's token stream into
// utterances: a new utterance starts on a speaker change, a
// sentence-ending token (., ?, !), or a >1.2s gap between original
// tokens. Translation-status tokens attach to the open utterance's
// Translated field. Speaker is "Speaker <n>", from the token's speaker
// field with any "spk_" prefix stripped. Lang is the utterance's
// dominant original-token language; At is the zero time.Time plus the
// first original token's start_ms offset.
func groupTokens(tokens []sonioxAsyncToken) []meeting.Utterance {
	type builder struct {
		speakerRaw string
		speaker    string
		text       strings.Builder
		translated strings.Builder
		startMS    int64
		lastEndMS  int64
		langs      map[string]int
		seq        int
	}
	var out []meeting.Utterance
	var cur *builder
	flush := func() {
		if cur == nil {
			return
		}
		u := meeting.Utterance{
			Seq:     int64(cur.seq),
			At:      time.Time{}.Add(time.Duration(cur.startMS) * time.Millisecond),
			Text:    cur.text.String(),
			Speaker: cur.speaker,
		}
		u.Translated = cur.translated.String()
		dominant, best := "", 0
		for lang, n := range cur.langs {
			if n > best {
				dominant, best = lang, n
			}
		}
		u.Lang = dominant
		out = append(out, u)
		cur = nil
	}
	for _, tok := range tokens {
		if tok.Text == "<end>" {
			continue
		}
		if tok.TranslationStatus == "translation" {
			if cur != nil {
				cur.translated.WriteString(tok.Text)
			}
			continue
		}
		speaker := strings.TrimPrefix(tok.Speaker, "spk_")
		newUtterance := cur == nil ||
			(tok.Speaker != "" && tok.Speaker != cur.speakerRaw) ||
			tok.StartMS-cur.lastEndMS > int64(utteranceGap/time.Millisecond)
		if !newUtterance && cur.text.Len() > 0 {
			t := cur.text.String()
			if strings.HasSuffix(t, ".") || strings.HasSuffix(t, "?") || strings.HasSuffix(t, "!") {
				newUtterance = true
			}
		}
		if speaker != "" {
			speaker = "Speaker " + speaker
		}
		if newUtterance {
			flush()
			cur = &builder{speakerRaw: tok.Speaker, speaker: speaker, startMS: tok.StartMS, lastEndMS: tok.EndMS, langs: map[string]int{}, seq: len(out) + 1}
			if tok.Language != "" {
				cur.langs[tok.Language]++
			}
			cur.text.WriteString(tok.Text)
			continue
		}
		cur.text.WriteString(tok.Text)
		if tok.Speaker != "" {
			cur.speakerRaw = tok.Speaker
			cur.speaker = speaker
		}
		if tok.Language != "" {
			cur.langs[tok.Language]++
		}
		if tok.EndMS > cur.lastEndMS {
			cur.lastEndMS = tok.EndMS
		}
	}
	flush()
	return out
}

// TranscribeFile implements BatchEngine: upload the audio, create the
// transcription, poll to completion, fetch the tokens, convert them to
// utterances, then always delete the transcription and the file — on
// success, on error and on ctx cancel (cleanup uses its own fresh
// short-timeout context).
func (e *SonioxAsyncEngine) TranscribeFile(ctx context.Context, audioPath string, o Options) ([]meeting.Utterance, error) {
	referenceID := sonioxAsyncReferencePrefix + filepath.Base(filepath.Dir(audioPath))

	fileID, err := e.uploadFile(ctx, audioPath, referenceID)
	if err != nil {
		return nil, err
	}
	var transcriptionID string
	defer func() { e.cleanup(transcriptionID, fileID) }()

	transcriptionID, err = e.createTranscription(ctx, fileID, o, referenceID)
	if err != nil {
		return nil, err
	}
	if _, err = e.pollTranscription(ctx, transcriptionID); err != nil {
		return nil, err
	}
	transcript, err := e.fetchTranscript(ctx, transcriptionID)
	if err != nil {
		return nil, err
	}
	return groupTokens(transcript.Tokens), nil
}

// CleanupStale lists transcriptions and files and deletes every item
// this engine created (client_reference_id or filename starting with
// "catchm-") older than one hour — leftovers from crashed runs, since
// files and transcriptions are otherwise kept 30 days. Per-item delete
// errors are ignored so one stuck item can't block the rest.
func (e *SonioxAsyncEngine) CleanupStale(ctx context.Context) error {
	cutoff := e.now().Add(-time.Hour)

	cursor := ""
	for {
		path := "/v1/transcriptions?limit=1000"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		var page struct {
			Transcriptions []sonioxAsyncTranscription `json:"transcriptions"`
			NextPageCursor string                     `json:"next_page_cursor"`
		}
		if err := e.do(ctx, http.MethodGet, path, "", nil, &page); err != nil {
			return err
		}
		for _, tr := range page.Transcriptions {
			if strings.HasPrefix(tr.ClientReferenceID, sonioxAsyncReferencePrefix) && tr.CreatedAt.Before(cutoff) {
				cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				_ = e.do(cctx, http.MethodDelete, "/v1/transcriptions/"+tr.ID, "", nil, nil)
				cancel()
			}
		}
		if page.NextPageCursor == "" {
			break
		}
		cursor = page.NextPageCursor
	}

	cursor = ""
	for {
		path := "/v1/files?limit=1000"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		var page struct {
			Files          []sonioxAsyncFile `json:"files"`
			NextPageCursor string            `json:"next_page_cursor"`
		}
		if err := e.do(ctx, http.MethodGet, path, "", nil, &page); err != nil {
			return err
		}
		for _, f := range page.Files {
			catchm := strings.HasPrefix(f.ClientReferenceID, sonioxAsyncReferencePrefix) ||
				strings.HasPrefix(f.Filename, sonioxAsyncReferencePrefix)
			if catchm && f.CreatedAt.Before(cutoff) {
				cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				_ = e.do(cctx, http.MethodDelete, "/v1/files/"+f.ID, "", nil, nil)
				cancel()
			}
		}
		if page.NextPageCursor == "" {
			break
		}
		cursor = page.NextPageCursor
	}
	return nil
}
