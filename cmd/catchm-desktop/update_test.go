package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/update"
)

// latestReleaseHandler serves the GitHub-Releases-shaped manifest the
// update API contract uses, pinned at v0.1.2.
func latestReleaseHandler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v0.1.2",
			"name":         "v0.1.2",
			"body":         "Fixes",
			"html_url":     "https://example.com/notes",
			"published_at": time.Now().Format(time.RFC3339),
			"assets":       []any{},
		}); err != nil {
			t.Errorf("encode release: %v", err)
		}
	})
	return mux
}

// newTestUpdateManager returns a manager wired to a local release
// server, with the running version pinned to a real semver (a "dev"
// build never sees itself as older — IsNewer treats dev as unknown
// age), restoring both on cleanup.
func newTestUpdateManager(t *testing.T, deferred bool) *updateManager {
	t.Helper()
	srv := httptest.NewServer(latestReleaseHandler(t))
	t.Cleanup(srv.Close)
	m := newUpdateManager()
	m.client = update.NewReleaseClient(srv.URL)
	m.deferred = deferred
	oldVersion := version
	version = "0.1.0"
	t.Cleanup(func() { version = oldVersion })
	return m
}

func TestCheckAndEmitManualEmitsEvenWhenDeferred(t *testing.T) {
	m := newTestUpdateManager(t, true /* deferred: user dismissed the banner */)

	var mu sync.Mutex
	var emitted []UpdateStatus
	emit := func(s UpdateStatus) {
		mu.Lock()
		defer mu.Unlock()
		emitted = append(emitted, s)
	}

	status := m.checkAndEmitWith(context.Background(), true /* manual */, emit)
	if !status.UpdateAvailable {
		t.Fatalf("manual check: UpdateAvailable = false, want true (%+v)", status)
	}
	if status.LatestVersion != "v0.1.2" {
		t.Errorf("manual check: LatestVersion = %q, want v0.1.2", status.LatestVersion)
	}
	mu.Lock()
	n := len(emitted)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("manual check emitted %d events, want 1 — without the emit, Settings reports an available update the banner never shows", n)
	}
}

func TestCheckAndEmitBackgroundSuppressedWhenDeferred(t *testing.T) {
	m := newTestUpdateManager(t, true)

	var emitted int
	emit := func(UpdateStatus) { emitted++ }

	status := m.checkAndEmitWith(context.Background(), false /* background */, emit)
	if !status.UpdateAvailable {
		t.Fatalf("background check: UpdateAvailable = false, want true (%+v)", status)
	}
	if emitted != 0 {
		t.Fatalf("deferred background check emitted %d events, want 0", emitted)
	}
}

func TestCheckAndEmitBackgroundEmitsWhenNotDeferred(t *testing.T) {
	m := newTestUpdateManager(t, false)

	var emitted int
	emit := func(UpdateStatus) { emitted++ }

	if status := m.checkAndEmitWith(context.Background(), false, emit); !status.UpdateAvailable {
		t.Fatalf("background check: UpdateAvailable = false, want true (%+v)", status)
	}
	if emitted != 1 {
		t.Fatalf("background check emitted %d events, want 1", emitted)
	}
}

func TestCheckAndEmitNoUpdateNeverEmits(t *testing.T) {
	srv := httptest.NewServer(latestReleaseHandler(t))
	t.Cleanup(srv.Close)
	m := newUpdateManager()
	m.client = update.NewReleaseClient(srv.URL)
	// Same version as the served release: not newer.
	oldVersion := version
	version = "0.1.2"
	t.Cleanup(func() { version = oldVersion })

	var emitted int
	emit := func(UpdateStatus) { emitted++ }

	status := m.checkAndEmitWith(context.Background(), true, emit)
	if status.UpdateAvailable {
		t.Fatalf("check: UpdateAvailable = true, want false (%+v)", status)
	}
	if emitted != 0 {
		t.Fatalf("up-to-date check emitted %d events, want 0", emitted)
	}
	if status.CheckedAt == "" {
		t.Error("check: CheckedAt empty, want a recorded timestamp")
	}
}

// installerServer serves v0.1.2 with a real installer + checksum. While
// stall is set, the installer sends half its bytes and then hangs until
// the client goes away, closing started once those bytes are out — a
// download the test can cancel mid-stream.
func installerServer(t *testing.T, stall *atomic.Bool) (srv *httptest.Server, started chan struct{}) {
	t.Helper()
	installer := []byte(strings.Repeat("installer bytes ", 512))
	sum := sha256.Sum256(installer)
	started = make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v0.1.2","published_at":"2026-09-20T10:00:00Z","assets":[`+
			`{"name":"catchm-desktop-amd64-installer.exe","browser_download_url":"http://%[1]s/dl/installer.exe"},`+
			`{"name":%[2]q,"browser_download_url":"http://%[1]s/dl/installer.exe.sha256"}]}`,
			r.Host, update.ChecksumFileName)
	})
	mux.HandleFunc("/dl/installer.exe", func(w http.ResponseWriter, r *http.Request) {
		if !stall.Load() {
			w.Write(installer)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(installer)))
		w.Write(installer[:len(installer)/2])
		w.(http.Flusher).Flush()
		started <- struct{}{}
		<-r.Context().Done()
	})
	mux.HandleFunc("/dl/installer.exe.sha256", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  catchm-desktop-amd64-installer.exe\n", hex.EncodeToString(sum[:]))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, started
}

// tempFiles isolates os.CreateTemp in a per-test dir (TMPDIR on Unix,
// TMP/TEMP on Windows) and returns a func counting the files in it.
func tempFiles(t *testing.T) func() int {
	t.Helper()
	dir := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(k, dir)
	}
	return func() int {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
}

// The "Cancel download" button: CancelDownload stops a download
// mid-stream, DownloadUpdate resolves as cancelled (not an error), the
// partial file is gone, nothing is deferred — and a retry right after
// downloads and verifies normally.
func TestCancelDownloadMidDownloadThenRetry(t *testing.T) {
	countTemp := tempFiles(t)
	var stall atomic.Bool
	stall.Store(true)
	srv, started := installerServer(t, &stall)
	m := newUpdateManager()
	m.client = update.NewReleaseClient(srv.URL)
	bridge := DesktopBridge{updates: m}

	type result struct {
		res DownloadResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := bridge.DownloadUpdate()
		done <- result{res, err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started streaming")
	}
	if n := countTemp(); n != 1 {
		t.Fatalf("mid-download: %d temp files, want the 1 partial installer", n)
	}

	bridge.CancelDownload()
	// CancelDownload waits for cleanup, so state is settled on return.
	if n := countTemp(); n != 0 {
		t.Fatalf("after cancel: %d temp files left, want 0 (partial installer not removed)", n)
	}
	m.mu.Lock()
	inFlight, deferred, verified := m.dl != nil, m.deferred, m.verified.Path
	m.mu.Unlock()
	if inFlight || deferred || verified != "" {
		t.Fatalf("after cancel: inFlight=%v deferred=%v verified=%q, want false/false/\"\"", inFlight, deferred, verified)
	}
	select {
	case r := <-done:
		if r.err != nil || !r.res.Cancelled || r.res.SHA256 != "" {
			t.Fatalf("cancelled DownloadUpdate = %+v, %v; want Cancelled with no error", r.res, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DownloadUpdate did not return after CancelDownload")
	}

	stall.Store(false)
	res, err := bridge.DownloadUpdate()
	if err != nil || res.Cancelled || res.SHA256 == "" {
		t.Fatalf("retry after cancel = %+v, %v; want a verified download", res, err)
	}
	m.mu.Lock()
	verified = m.verified.Path
	m.mu.Unlock()
	if _, err := os.Stat(verified); err != nil {
		t.Fatalf("retry: verified installer missing: %v", err)
	}
	if n := countTemp(); n != 1 {
		t.Fatalf("after retry: %d temp files, want just the verified installer", n)
	}
}

// Nothing downloading: CancelDownload returns at once and changes
// nothing (the button can race a download that just finished).
func TestCancelDownloadIdleIsNoop(t *testing.T) {
	m := newTestUpdateManager(t, false)
	m.verified = update.VerifiedInstaller{Path: "kept.exe", SHA256: "abc"}
	bridge := DesktopBridge{updates: m}

	returned := make(chan struct{})
	go func() {
		bridge.CancelDownload()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("CancelDownload blocked with nothing downloading")
	}
	if m.dl != nil || m.deferred || m.verified.Path != "kept.exe" {
		t.Fatalf("idle cancel changed state: dl=%v deferred=%v verified=%+v", m.dl, m.deferred, m.verified)
	}
}

// staleInstaller writes a fake leftover installer under our own temp
// naming pattern into the (tempFiles-isolated) temp dir.
func staleInstaller(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "catchm-update-*.exe")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// A new download replaces a previously verified installer that was
// declined with "Not now": the old file is deleted, not left in temp.
func TestDownloadRemovesPreviousInstaller(t *testing.T) {
	countTemp := tempFiles(t)
	var stall atomic.Bool
	srv, _ := installerServer(t, &stall)
	m := newUpdateManager()
	m.client = update.NewReleaseClient(srv.URL)
	old := staleInstaller(t)
	m.verified = update.VerifiedInstaller{Path: old, SHA256: "old"}

	res, err := DesktopBridge{updates: m}.DownloadUpdate()
	if err != nil || res.SHA256 == "" {
		t.Fatalf("DownloadUpdate = %+v, %v", res, err)
	}
	if exists(old) {
		t.Fatal("previous verified installer survived a new download")
	}
	if !exists(m.verified.Path) || m.verified.Path == old {
		t.Fatalf("verified = %q, want the new installer", m.verified.Path)
	}
	if n := countTemp(); n != 1 {
		t.Fatalf("%d temp files, want just the new installer", n)
	}
}

// On startup, leftover installers from earlier runs are swept — even
// in a build with updates off — keeping the currently verified one.
func TestStartupRemovesStaleInstallers(t *testing.T) {
	countTemp := tempFiles(t)
	stale1, stale2, keep := staleInstaller(t), staleInstaller(t), staleInstaller(t)
	m := &updateManager{verified: update.VerifiedInstaller{Path: keep}}

	m.startBackgroundChecks(context.Background()) // nil client: sweep only, no goroutine
	if exists(stale1) || exists(stale2) {
		t.Fatal("stale installers survived startup")
	}
	if !exists(keep) || countTemp() != 1 {
		t.Fatalf("verified installer removed, or other files left (%d)", countTemp())
	}
}

// With a download in flight the sweep does nothing: that download's
// partial file matches the same pattern.
func TestStaleSweepSkippedWhileDownloading(t *testing.T) {
	tempFiles(t)
	partial := staleInstaller(t)
	m := &updateManager{dl: &downloadRun{cancel: func() {}, done: make(chan struct{})}}

	m.removeStaleInstallers()
	if !exists(partial) {
		t.Fatal("sweep deleted an in-flight download's file")
	}
}
