package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Default timeouts. There is deliberately no overall deadline on an
// asset download: an installer is tens of MB, and a whole-request
// timeout (the old http.Client.Timeout of 30s) failed any download
// slower than ~1 MB/s even while bytes were still arriving.
const (
	// DefaultRequestTimeout caps the manifest request end to end, and
	// the wait for an asset's response headers.
	DefaultRequestTimeout = 30 * time.Second
	// DefaultStallTimeout fails an asset download once no body bytes
	// have arrived for this long — a dead connection, not a slow one.
	DefaultStallTimeout = 60 * time.Second
)

// ErrStalled: an asset download received no bytes for StallTimeout.
// Ordinary network failure — safe to retry.
var ErrStalled = errors.New("update: download stalled")

// errHeaderTimeout: no response headers within RequestTimeout.
var errHeaderTimeout = errors.New("update: no response from server")

// ReleaseClient fetches release metadata from the update API — since
// M24 that's the catchm-cloud Worker's /update route, serving a
// GitHub-Releases-shaped response so this package's decoding stays
// unchanged from M23. BaseURL and HTTPClient are injectable so tests
// (and local end-to-end update trials) point at an httptest.Server
// instead of the real network.
type ReleaseClient struct {
	// BaseURL is the update API root, e.g. "https://cloud.example.com/update"
	// (the ldflags-stamped cloud origin plus the "/update" route
	// prefix). No trailing slash.
	BaseURL string
	Client  *http.Client
	// RequestTimeout and StallTimeout override DefaultRequestTimeout /
	// DefaultStallTimeout when non-zero (tests use short ones).
	RequestTimeout time.Duration
	StallTimeout   time.Duration
}

// NewReleaseClient returns a ReleaseClient for the update API at
// baseURL (the ldflags-stamped cloud origin + "/update").
func NewReleaseClient(baseURL string) *ReleaseClient {
	return &ReleaseClient{
		BaseURL: baseURL,
		// No Client.Timeout: it would also cap the installer body read.
		// Latest and Download apply their own timeouts instead.
		Client: &http.Client{},
	}
}

// Release is the subset of the release JSON this package needs — the
// GitHub-Releases-shaped manifest the update Worker serves (stored
// manifest plus draft/prerelease/html_url and absolute
// browser_download_urls).
type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"` // release notes (markdown)
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	Assets      []Asset   `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Latest fetches the latest published release (GET {base}/releases/latest).
func (c *ReleaseClient) Latest(ctx context.Context) (*Release, error) {
	// The manifest is a few KB: an overall deadline is right here.
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.BaseURL, "/")+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNoRelease
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("fetch latest release: status %d", resp.StatusCode)
	}
	var rel Release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode latest release: %w", err)
	}
	return &rel, nil
}

// Download fetches url as a release asset (the browser_download_url
// the update Worker serves straight from R2; no auth either way).
// Response headers must arrive within RequestTimeout; after that the
// body may take as long as it needs, as long as no StallTimeout-long
// gap passes without a byte — then reads fail with ErrStalled. ctx
// still cancels it at any point. Closing the body releases the timers.
func (c *ReleaseClient) Download(ctx context.Context, url string) (*http.Response, error) {
	dctx, cancel := context.WithCancelCause(ctx)
	req, err := http.NewRequestWithContext(dctx, http.MethodGet, url, nil)
	if err != nil {
		cancel(nil)
		return nil, err
	}
	headerTimer := time.AfterFunc(c.requestTimeout(), func() { cancel(errHeaderTimeout) })
	resp, err := c.client().Do(req)
	headerTimer.Stop()
	if err != nil {
		err = timeoutCause(ctx, dctx, err)
		cancel(nil)
		return nil, fmt.Errorf("download asset: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel(nil)
		return nil, fmt.Errorf("download asset: status %d", resp.StatusCode)
	}
	stall := c.stallTimeout()
	resp.Body = &stallReader{
		body:   resp.Body,
		parent: ctx,
		ctx:    dctx,
		cancel: cancel,
		stall:  stall,
		timer:  time.AfterFunc(stall, func() { cancel(ErrStalled) }),
	}
	return resp, nil
}

// timeoutCause swaps a transport error caused by one of Download's own
// timers for that timer's cause (ErrStalled / errHeaderTimeout), which
// says more than "context canceled". A cancel of the caller's ctx is
// left alone: callers detect that via their own ctx.
func timeoutCause(parent, dctx context.Context, err error) error {
	if parent.Err() == nil && dctx.Err() != nil {
		if cause := context.Cause(dctx); cause != nil && !errors.Is(cause, context.Canceled) {
			return cause
		}
	}
	return err
}

// stallReader re-arms the stall timer on every read that makes
// progress; the timer cancels the request when it fires.
type stallReader struct {
	body   io.ReadCloser
	parent context.Context
	ctx    context.Context
	cancel context.CancelCauseFunc
	stall  time.Duration
	timer  *time.Timer
}

func (r *stallReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		r.timer.Reset(r.stall)
	}
	if err != nil && err != io.EOF {
		err = timeoutCause(r.parent, r.ctx, err)
	}
	return n, err
}

func (r *stallReader) Close() error {
	r.timer.Stop()
	err := r.body.Close()
	r.cancel(nil)
	return err
}

func (c *ReleaseClient) requestTimeout() time.Duration {
	if c.RequestTimeout > 0 {
		return c.RequestTimeout
	}
	return DefaultRequestTimeout
}

func (c *ReleaseClient) stallTimeout() time.Duration {
	if c.StallTimeout > 0 {
		return c.StallTimeout
	}
	return DefaultStallTimeout
}

func (c *ReleaseClient) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}
