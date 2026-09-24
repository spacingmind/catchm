package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/spacingmind/catchm/internal/cloudsync"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/e2ee"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/tools"
)

func newServerTestEnv(t *testing.T, baseURL string) *Server {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(tools.New(store), &config.Config{Cloud: config.CloudConfig{APIBaseURL: baseURL}})
}

func putCloudServer(t *testing.T, s *Server, body string) (int, map[string]any) {
	t.Helper()
	return doCloud(t, s, http.MethodPut, "/api/cloud/server", body)
}

func TestCloudServerURLValidation(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want string
	}{
		{"https://cloud.example.com", true, "https://cloud.example.com"},
		{"https://cloud.example.com/", true, "https://cloud.example.com"}, // trailing slash trimmed
		{"https://cloud.example.com/sub", true, "https://cloud.example.com/sub"},
		{"http://127.0.0.1:8787", true, "http://127.0.0.1:8787"},
		{"http://localhost:8787", true, "http://localhost:8787"},
		{"http://[::1]:8787", true, "http://[::1]:8787"},
		{"http://example.com", false, ""},                  // non-loopback http
		{"https://user:pass@cloud.example.com", false, ""}, // userinfo
		{"https://cloud.example.com?x=1", false, ""},       // query
		{"https://cloud.example.com#frag", false, ""},      // fragment
		{"ftp://cloud.example.com", false, ""},             // scheme
		{"not a url", false, ""},                           // garbage
		{"cloud.example.com", false, ""},                   // not absolute
		{"", false, ""},                                    // empty
	}
	for _, tc := range cases {
		s := newServerTestEnv(t, "http://127.0.0.1:8787")
		code, body := putCloudServer(t, s, fmt.Sprintf(`{"api_base_url":%q}`, tc.in))
		if tc.ok {
			if code != http.StatusOK {
				t.Errorf("%q: code = %d, body = %v; want 200", tc.in, code, body)
			}
			if got, _ := body["api_base_url"].(string); got != tc.want {
				t.Errorf("%q: normalized to %q, want %q", tc.in, got, tc.want)
			}
		} else if code != http.StatusBadRequest {
			t.Errorf("%q: code = %d, want 400", tc.in, code)
		}
	}
}

func TestCloudServerChangeWhileLoggedInIs409(t *testing.T) {
	s, _ := newCloudTestServer(t)
	code, _ := doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"x@example.com","password":"correct horse battery staple"}`)
	if code != http.StatusOK {
		t.Fatalf("signup code = %d", code)
	}
	code, body := putCloudServer(t, s, `{"api_base_url":"https://cloud.example.com"}`)
	if code != http.StatusConflict {
		t.Fatalf("code = %d, body = %v, want 409", code, body)
	}
	// Config unchanged.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud.APIBaseURL == "https://cloud.example.com" {
		t.Fatal("api_base_url changed despite 409")
	}
}

func TestCloudServerChangeClearsAccountParamsAndPersists(t *testing.T) {
	s, _ := newCloudTestServer(t)
	doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"old@example.com","password":"correct horse battery staple"}`)
	doCloud(t, s, http.MethodPost, "/api/cloud/logout", "")

	code, _ := putCloudServer(t, s, `{"api_base_url":"https://cloud.example.net"}`)
	if code != http.StatusOK {
		t.Fatal("server change rejected while logged out")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud.APIBaseURL != "https://cloud.example.net" {
		t.Fatalf("api_base_url = %q", cfg.Cloud.APIBaseURL)
	}
	if cfg.Cloud.Email != "" || cfg.Cloud.AuthSalt != "" || cfg.Cloud.KDFParams != "" {
		t.Fatalf("old server's account params not cleared: %+v", cfg.Cloud)
	}

	// Status surfaces the new URL (logged out).
	_, body := doCloud(t, s, http.MethodGet, "/api/cloud/status", "")
	if got, _ := body["api_base_url"].(string); got != "https://cloud.example.net" {
		t.Fatalf("status api_base_url = %v", body)
	}
}

// A→B→A manifest isolation: each server gets its own manifest file, so
// switching never leaks remote versions across servers and never
// forgets state when switching back.
func TestCloudSyncManifestIsolationPerServer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)

	serverURL := func(fake *cloudFakeWorker) string { return fake.url }
	fakeA, fakeB := newCloudTestWorkerOnly(t), newCloudTestWorkerOnly(t)
	clientA, clientB := cloudsync.NewClient(serverURL(fakeA)), cloudsync.NewClient(serverURL(fakeB))
	token := cloudTestSignupOn(t, fakeA, "iso@example.com")
	masterKey := e2ee.GenerateMasterKey()

	store, err := meeting.NewStore(filepath.Join(home, "meetings"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start("Isolation", timeNowUTC(), meeting.StartOptions{}); err != nil {
		t.Fatal(err)
	}

	// Sync against A: pushes at version 0.
	if _, err := cloudsync.Sync(store, clientA, token, masterKey); err != nil {
		t.Fatal(err)
	}
	manifestA := manifestPathFor(t, serverURL(fakeA))
	if _, err := os.Stat(manifestA); err != nil {
		t.Fatalf("A's manifest missing: %v", err)
	}

	// Sync the same store against B (different server, fresh account):
	// must push with expectedVersion 0, not A's versions.
	tokenB := cloudTestSignupOn(t, fakeB, "iso@example.com")
	if _, err := cloudsync.Sync(store, clientB, tokenB, masterKey); err != nil {
		t.Fatal(err)
	}
	manifestB := manifestPathFor(t, serverURL(fakeB))
	if _, err := os.Stat(manifestB); err != nil {
		t.Fatalf("B's manifest missing: %v", err)
	}
	if manifestA == manifestB {
		t.Fatal("A and B share one manifest file")
	}

	// Switch back to A: its manifest is intact — the unchanged meeting
	// is a no-op push (would fail if A's versions were lost or mixed).
	result, err := cloudsync.Sync(store, clientA, token, masterKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pushed) != 0 {
		t.Fatalf("switching back to A re-pushed unchanged meetings: %v", result.Pushed)
	}
}
