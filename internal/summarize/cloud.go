package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// cloudChunkChars caps the transcript sent per POST /v1/summarize,
	// in UTF-16 code units (JavaScript string length) — catchm-api's
	// MAX_SUMMARY_CHARS. Each chunk costs one unit of the daily quota,
	// so chunks are as large as the Worker allows.
	cloudChunkChars = 60000
	// cloudRequestTimeout bounds one cloud call; the Worker's fallback
	// model reasons before answering and can take ~90s on its own.
	cloudRequestTimeout = 180 * time.Second
)

// cloudHTTP is the default client for cloud calls.
var cloudHTTP = &http.Client{Timeout: cloudRequestTimeout}

// runeUTF16 costs a rune by its UTF-16 length, as JavaScript measures.
func runeUTF16(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

// errTooLarge is a 413 carrying the Worker's max_chars, so Summarize
// can re-chunk; it unwraps to the summary_failed *Error.
type errTooLarge struct {
	err      *Error
	maxChars int
}

func (e *errTooLarge) Error() string { return e.err.Error() }
func (e *errTooLarge) Unwrap() error { return e.err }

// Cloud summarizes through catchm-api's POST /v1/summarize with the
// cloud-sync session. The transcript leaves the device unencrypted, so
// nothing is sent unless Consent is true.
type Cloud struct {
	BaseURL string
	Token   string
	Consent bool
	// HTTP defaults to a client with cloudRequestTimeout.
	HTTP *http.Client
}

// Quota is GET /v1/summarize/quota's body.
type Quota struct {
	RemainingToday int    `json:"remaining_today"`
	Limit          int    `json:"limit"`
	ResetAt        string `json:"reset_at"`
}

type cloudRequest struct {
	Title      string `json:"title"`
	Transcript string `json:"transcript"`
	Language   string `json:"language,omitempty"`
}

func (c *Cloud) Summarize(ctx context.Context, in Input) (Extraction, error) {
	if !c.Consent {
		return Extraction{}, consentRequired()
	}
	if c.Token == "" {
		return Extraction{}, notReady("Log in to CatchM Cloud in Settings → Cloud sync to use cloud summaries.", nil)
	}
	chunks := splitTranscript(in.Transcript, cloudChunkChars, runeUTF16)
	if len(chunks) > 1 {
		if err := c.checkQuotaFor(ctx, len(chunks)); err != nil {
			return Extraction{}, err
		}
	}
	parts := make([]Extraction, 0, len(chunks))
	for len(chunks) > 0 {
		chunk := chunks[0]
		body := cloudRequest{Title: in.Title, Transcript: chunk, Language: in.Language}
		ex, err := withRetry(func() (string, error) { return c.post(ctx, body) })
		var tl *errTooLarge
		if errors.As(err, &tl) {
			if tl.maxChars > 0 && tl.maxChars < costOf(chunk, runeUTF16) {
				// The Worker's cap is lower than ours: re-cut this chunk
				// to it. The rejected call cost no quota.
				chunks = append(splitTranscript(chunk, tl.maxChars, runeUTF16), chunks[1:]...)
				continue
			}
			err = tl.err
		}
		if err != nil {
			return Extraction{}, err
		}
		parts = append(parts, ex)
		chunks = chunks[1:]
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	// No model-side merge: a merge call would spend another unit of
	// the user's daily quota.
	return mergeExtractions(parts), nil
}

func (c *Cloud) post(ctx context.Context, body cloudRequest) (string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", failed("Couldn't build the summary request.", err)
	}
	status, resp, err := c.do(ctx, http.MethodPost, "/v1/summarize", raw)
	if err != nil {
		return "", err
	}
	if status == http.StatusOK {
		return string(resp), nil
	}
	cause := fmt.Errorf("catchm-api: status %d: %s", status, truncate(strings.TrimSpace(string(resp)), 500))
	switch status {
	case http.StatusUnauthorized:
		return "", notReady("Your CatchM Cloud session has expired. Log in again in Settings → Cloud sync.", cause)
	case http.StatusRequestEntityTooLarge:
		var tl struct {
			MaxChars int `json:"max_chars"`
		}
		_ = json.Unmarshal(resp, &tl)
		return "", &errTooLarge{err: failed("This transcript is too long for CatchM Cloud.", cause), maxChars: tl.MaxChars}
	case http.StatusTooManyRequests:
		var q struct {
			Error   string `json:"error"`
			ResetAt string `json:"reset_at"`
		}
		_ = json.Unmarshal(resp, &q)
		if q.Error != "quota_exceeded" {
			// The per-account rate limit, not the daily quota.
			return "", failed("Too many summaries at once. Wait a minute and try again.", cause)
		}
		return "", &Error{
			Kind:    KindQuotaExceeded,
			Message: "You've used today's free cloud summaries.",
			ResetAt: q.ResetAt,
			Err:     cause,
		}
	case http.StatusServiceUnavailable:
		return "", failed("CatchM Cloud's summarizer is busy right now. Try again later.", cause)
	default:
		return "", failed("CatchM Cloud couldn't write the summary.", cause)
	}
}

// checkQuotaFor fails early when today's remaining quota can't cover
// every chunk, rather than spending it on a summary that can't finish.
// An unreachable quota endpoint doesn't block: the calls themselves
// still answer 429 when the quota runs out.
func (c *Cloud) checkQuotaFor(ctx context.Context, chunks int) error {
	q, err := c.Quota(ctx)
	if err != nil || q.RemainingToday >= chunks {
		return nil
	}
	return &Error{
		Kind: KindQuotaExceeded,
		Message: fmt.Sprintf("This meeting is long enough to need %d cloud summaries, but only %d are left today.",
			chunks, max(q.RemainingToday, 0)),
		ResetAt: q.ResetAt,
	}
}

// Quota fetches today's remaining cloud summaries. It sends no meeting
// data, so it needs a session but not consent.
func (c *Cloud) Quota(ctx context.Context) (Quota, error) {
	status, raw, err := c.do(ctx, http.MethodGet, "/v1/summarize/quota", nil)
	if err != nil {
		return Quota{}, err
	}
	if status != http.StatusOK {
		return Quota{}, fmt.Errorf("catchm-api quota: status %d: %s", status, truncate(strings.TrimSpace(string(raw)), 200))
	}
	var q Quota
	if err := json.Unmarshal(raw, &q); err != nil {
		return Quota{}, fmt.Errorf("catchm-api quota: %w", err)
	}
	return q, nil
}

func (c *Cloud) do(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, reader)
	if err != nil {
		return 0, nil, notReady("The CatchM Cloud address isn't valid. Check it in Settings → Cloud sync.", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	client := c.HTTP
	if client == nil {
		client = cloudHTTP
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, failed("The summary was cancelled.", err)
		}
		return 0, nil, failed("Can't reach CatchM Cloud. Check your internet connection and try again.", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, failed("Lost the connection to CatchM Cloud.", err)
	}
	return resp.StatusCode, raw, nil
}
