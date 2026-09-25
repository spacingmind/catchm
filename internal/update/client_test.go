package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// latestJSON is a real GitHub /releases/latest response shape (subset
// this package decodes), served by the fixtures below.
const latestJSON = `{
  "tag_name": "v1.1.0",
  "name": "CatchM v1.1.0",
  "body": "Release notes here",
  "html_url": "https://github.com/spacingmind/catchm/releases/tag/v1.1.0",
  "published_at": "2026-09-20T10:00:00Z",
  "draft": false,
  "prerelease": false,
  "assets": [
    {"name": "catchm-desktop-amd64-installer.exe", "browser_download_url": "/dl/installer.exe", "size": 12345},
    {"name": "catchm-desktop-amd64-installer.exe.sha256", "browser_download_url": "/dl/installer.exe.sha256", "size": 100}
  ]
}`

func newFixtureServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func TestLatestSuccess(t *testing.T) {
	srv := newFixtureServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/releases/latest" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, latestJSON)
	})
	defer srv.Close()

	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel.TagName != "v1.1.0" || rel.HTMLURL == "" || len(rel.Assets) != 2 {
		t.Fatalf("unexpected release: %+v", rel)
	}
	if !rel.PublishedAt.Equal(publishedAtOf(t)) {
		t.Fatalf("PublishedAt = %v", rel.PublishedAt)
	}
}

func TestLatestNoReleasesYet(t *testing.T) {
	srv := newFixtureServer(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	defer srv.Close()

	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}
	_, err := c.Latest(context.Background())
	if err != ErrNoRelease {
		t.Fatalf("Latest error = %v, want ErrNoRelease", err)
	}
}

func TestLatestMalformedJSON(t *testing.T) {
	srv := newFixtureServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html>this is not json</html>`)
	})
	defer srv.Close()

	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}
	if _, err := c.Latest(context.Background()); err == nil || err == ErrNoRelease {
		t.Fatalf("Latest error = %v, want a decode error", err)
	}
}

func TestLatestServerError(t *testing.T) {
	srv := newFixtureServer(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	defer srv.Close()

	c := &GitHubClient{BaseURL: srv.URL, Client: srv.Client()}
	if _, err := c.Latest(context.Background()); err == nil || err == ErrNoRelease {
		t.Fatalf("Latest error = %v, want a status error", err)
	}
}
