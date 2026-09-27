package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/spacingmind/catchm/internal/update"
	"time"
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
