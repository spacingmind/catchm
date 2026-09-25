package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// GitHubClient fetches release metadata from a GitHub-Releases-shaped
// API. BaseURL and HTTPClient are injectable so tests (and local
// end-to-end update trials) point at an httptest.Server instead of the
// real network.
type GitHubClient struct {
	// BaseURL is the API root, e.g. "https://api.github.com/repos/spacingmind/catchm".
	// No trailing slash.
	BaseURL string
	Client  *http.Client
}

func NewGitHubClient(repo string) *GitHubClient {
	return &GitHubClient{
		BaseURL: "https://api.github.com/repos/" + repo,
		Client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Release is the subset of GitHub's release JSON this package needs.
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
func (c *GitHubClient) Latest(ctx context.Context) (*Release, error) {
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
// GitHub serves straight from storage; no API auth either way).
func (c *GitHubClient) Download(ctx context.Context, url string) (*http.Response, error) {
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

func (c *GitHubClient) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return http.DefaultClient
}
