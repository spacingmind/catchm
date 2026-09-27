package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// latestJSON is a release manifest response shape (subset
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

	c := &ReleaseClient{BaseURL: srv.URL, Client: srv.Client()}
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

	c := &ReleaseClient{BaseURL: srv.URL, Client: srv.Client()}
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

	c := &ReleaseClient{BaseURL: srv.URL, Client: srv.Client()}
	if _, err := c.Latest(context.Background()); err == nil || err == ErrNoRelease {
		t.Fatalf("Latest error = %v, want a decode error", err)
	}
}

func TestLatestServerError(t *testing.T) {
	srv := newFixtureServer(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	defer srv.Close()

	c := &ReleaseClient{BaseURL: srv.URL, Client: srv.Client()}
	if _, err := c.Latest(context.Background()); err == nil || err == ErrNoRelease {
		t.Fatalf("Latest error = %v, want a status error", err)
	}
}

// Short stand-ins for DefaultRequestTimeout / DefaultStallTimeout.
const (
	testRequestTimeout = 60 * time.Millisecond
	testStallTimeout   = 80 * time.Millisecond
)

func shortTimeoutClient(srv *httptest.Server) *ReleaseClient {
	return &ReleaseClient{
		BaseURL:        srv.URL,
		Client:         srv.Client(),
		RequestTimeout: testRequestTimeout,
		StallTimeout:   testStallTimeout,
	}
}

// A slow download that keeps making progress must not time out, even
// though it takes several times longer than RequestTimeout (the old
// whole-request http.Client.Timeout would have killed it) and longer
// than StallTimeout in total.
func TestDownloadSlowButProgressingSucceeds(t *testing.T) {
	const chunks = 12
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < chunks; i++ {
			fmt.Fprint(w, "chunk;")
			w.(http.Flusher).Flush()
			time.Sleep(testRequestTimeout / 3)
		}
	}))
	defer srv.Close()

	start := time.Now()
	resp, err := shortTimeoutClient(srv).Download(context.Background(), srv.URL+"/dl")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read slow body: %v", err)
	}
	if got := string(body); got != strings.Repeat("chunk;", chunks) {
		t.Fatalf("body = %q", got)
	}
	if elapsed := time.Since(start); elapsed < 3*testRequestTimeout || elapsed < 2*testStallTimeout {
		t.Fatalf("download took %v — too fast to prove the old overall timeout is gone", elapsed)
	}
}

// A connection that stops sending body bytes fails with ErrStalled
// (not a hang, and not a cancel).
func TestDownloadStalledFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		fmt.Fprint(w, "partial")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	resp, err := shortTimeoutClient(srv).Download(context.Background(), srv.URL+"/dl")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer resp.Body.Close()
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(resp.Body)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrStalled) {
			t.Fatalf("read error = %v, want ErrStalled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stalled download never failed")
	}
}

// No response headers within RequestTimeout: fail, don't wait forever.
func TestDownloadNoHeadersTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	_, err := shortTimeoutClient(srv).Download(context.Background(), srv.URL+"/dl")
	if !errors.Is(err, errHeaderTimeout) {
		t.Fatalf("error = %v, want errHeaderTimeout", err)
	}
}

// The caller's own cancel stays a cancel — never reported as a stall.
func TestDownloadCallerCancelIsNotStall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "partial")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := shortTimeoutClient(srv)
	c.StallTimeout = time.Hour
	resp, err := c.Download(ctx, srv.URL+"/dl")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer resp.Body.Close()
	cancel()
	_, err = io.ReadAll(resp.Body)
	if errors.Is(err, ErrStalled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("read error = %v, want context.Canceled", err)
	}
}

// Latest keeps an overall deadline: its response is a few KB.
func TestLatestTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	if _, err := shortTimeoutClient(srv).Latest(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}
