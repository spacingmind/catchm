package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

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
}

// NewReleaseClient returns a ReleaseClient for the update API at
// baseURL (the ldflags-stamped cloud origin + "/update").
func NewReleaseClient(baseURL string) *ReleaseClient {
	return &ReleaseClient{
		BaseURL: baseURL,
		Client:  &http.Client{Timeout: 30 * time.Second},
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
func (c *ReleaseClient) Download(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download asset: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download asset: status %d", resp.StatusCode)
	}
	return resp, nil
}

func (c *ReleaseClient) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}
