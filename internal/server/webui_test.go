package server

import (
	"net/http"
	"strings"
	"testing"
)

// containsAppShellMarkup reports whether body looks like the served
// index.html — either the real Vite build or the placeholder committed for
// a fresh checkout (see web/dist/index.html and web/embed.go). Either is
// acceptable here: this test proves the embed+serve wiring works, not the
// content of a real frontend build.
func containsAppShellMarkup(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "<!doctype") || strings.Contains(lower, "<html")
}

func TestWebUIServesAppShellAtRoot(t *testing.T) {
	s, _ := newTestServer(t)

	w := do(t, s, "GET", "/", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}
	if !containsAppShellMarkup(w.Body.String()) {
		t.Fatalf("body doesn't look like an HTML app shell: %q", w.Body.String())
	}
}

func TestWebUISPAFallbackForUnknownPaths(t *testing.T) {
	s, _ := newTestServer(t)

	for _, target := range []string{
		"/meetings/does-not-exist",
		"/some/deeply/nested/client-only/route",
		"/whatever-the-ui-thinks-it-is-showing",
	} {
		w := do(t, s, "GET", target, "")
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %q", target, w.Code, w.Body.String())
		}
		if !containsAppShellMarkup(w.Body.String()) {
			t.Fatalf("%s: body doesn't look like an HTML app shell: %q", target, w.Body.String())
		}
	}
}

// TestWebUIDoesNotShadowAPIRoutes proves the catch-all "/" web UI handler
// never intercepts /healthz or /api/* — both must keep behaving exactly as
// they did before this milestone (JSON responses, existing status codes),
// not fall back to the SPA shell.
func TestWebUIDoesNotShadowAPIRoutes(t *testing.T) {
	s, _ := newTestServer(t)

	w := do(t, s, "GET", "/healthz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("/healthz Content-Type = %q, want application/json", ct)
	}
	var health map[string]string
	decode(t, w, &health)
	if health["status"] != "ok" {
		t.Fatalf("/healthz status field = %v", health["status"])
	}

	w = do(t, s, "GET", "/api/meetings", "")
	if w.Code != http.StatusOK {
		t.Fatalf("/api/meetings status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("/api/meetings Content-Type = %q, want application/json", ct)
	}

	// A meeting ID that doesn't exist must still 404 as JSON from the API
	// handler, not fall back to the SPA shell (which would incorrectly
	// mask a real API error as a 200).
	w = do(t, s, "GET", "/api/meetings/does-not-exist", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("/api/meetings/does-not-exist status = %d, want 404", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("/api/meetings/does-not-exist Content-Type = %q, want application/json", ct)
	}
}
