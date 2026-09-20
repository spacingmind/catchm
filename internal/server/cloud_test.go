package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spacingmind/catchm/internal/cloudsync"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/tools"
)

// newCloudTestServer builds a Server whose cloud client hits the shared
// fake Worker, with CATCHM_HOME isolated to a temp dir so config.yaml
// persistence is real and observable.
func newCloudTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)

	server := httptestNewCloudWorker(t)
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(tools.New(store), &config.Config{Cloud: config.CloudConfig{APIBaseURL: server.URL}})
	s.newCloudClient = func() *cloudsync.Client { return cloudsync.NewClient(server.URL) }
	return s, server
}

func doCloud(t *testing.T, s *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	var req *http.Request
	var err error
	if body == "" {
		req, err = http.NewRequest(method, path, nil)
	} else {
		req, err = http.NewRequest(method, path, strings.NewReader(body))
	}
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("non-JSON response %q: %v", rec.Body.String(), err)
	}
	return rec.Code, out
}

func TestCloudSignupPersistsConfigAndReturnsRecoveryCode(t *testing.T) {
	s, _ := newCloudTestServer(t)

	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"user@example.com","password":"correct horse battery staple"}`)
	if code != http.StatusOK {
		t.Fatalf("signup code = %d, body = %v", code, body)
	}
	recoveryCode, _ := body["recovery_code"].(string)
	if recoveryCode == "" {
		t.Fatalf("no recovery_code in response: %v", body)
	}

	// Non-secret fields persisted to config.yaml…
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud.Email != "user@example.com" {
		t.Fatalf("config email = %q", cfg.Cloud.Email)
	}
	if cfg.Cloud.AuthSalt == "" || cfg.Cloud.KDFParams == "" {
		t.Fatalf("config cloud fields not persisted: %+v", cfg.Cloud)
	}
	// …and nothing secret: no master key, session token, recovery code,
	// or password anywhere in the file.
	raw := readFile(t, mustConfigPath(t))
	for _, secret := range []string{recoveryCode, "correct horse"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("secret material %q leaked into config.yaml:\n%s", secret, raw)
		}
	}

	// Status reports logged-in with the email.
	code, body = doCloud(t, s, http.MethodGet, "/api/cloud/status", "")
	if code != http.StatusOK || body["logged_in"] != true || body["email"] != "user@example.com" {
		t.Fatalf("status = %d %v", code, body)
	}
}

func TestCloudSignupDuplicateEmailIs409(t *testing.T) {
	s, _ := newCloudTestServer(t)
	for i := 0; i < 2; i++ {
		code, body := doCloud(t, s, http.MethodPost, "/api/cloud/signup",
			`{"email":"dup@example.com","password":"correct horse battery staple"}`)
		want := http.StatusOK
		if i == 1 {
			want = http.StatusConflict
		}
		if code != want {
			t.Fatalf("signup %d code = %d (want %d), body = %v", i, code, want, body)
		}
	}
}

func TestCloudLoginRoundTrip(t *testing.T) {
	s, _ := newCloudTestServer(t)
	if _, body := doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"login@example.com","password":"correct horse battery staple"}`); body["recovery_code"] == "" {
		t.Fatalf("signup failed: %v", body)
	}

	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"login@example.com","password":"correct horse battery staple"}`)
	if code != http.StatusOK {
		t.Fatalf("login code = %d, body = %v", code, body)
	}
	if body["recovery_code"] != nil {
		t.Fatalf("login must not return a recovery code: %v", body)
	}
}

func TestCloudLoginWrongPasswordFails(t *testing.T) {
	s, _ := newCloudTestServer(t)

	// Unknown email: fake auth-params mean the derived keys can't match
	// anything, so the Worker's 401 surfaces.
	code, _ := doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"ghost@example.com","password":"whatever"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("unknown-email login code = %d, want 401", code)
	}
	code, body := doCloud(t, s, http.MethodGet, "/api/cloud/status", "")
	if code != http.StatusOK || body["logged_in"] != false {
		t.Fatalf("status after failed login = %d %v", code, body)
	}
}

// A failed re-login attempt must not destroy an already-valid session
// (a typo in the password shouldn't log you out).
func TestCloudFailedLoginKeepsExistingSession(t *testing.T) {
	s, _ := newCloudTestServer(t)
	doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"keep@example.com","password":"correct horse battery staple"}`)

	code, _ := doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"keep@example.com","password":"typo horse"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("wrong-password login code = %d, want 401", code)
	}
	code, body := doCloud(t, s, http.MethodGet, "/api/cloud/status", "")
	if code != http.StatusOK || body["logged_in"] != true {
		t.Fatalf("existing session dropped by failed login: %d %v", code, body)
	}
}

func TestCloudSyncRequiresLogin(t *testing.T) {
	s, _ := newCloudTestServer(t)
	code, _ := doCloud(t, s, http.MethodPost, "/api/cloud/sync", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("sync without login code = %d, want 401", code)
	}
}

func TestCloudLogoutClearsSession(t *testing.T) {
	s, _ := newCloudTestServer(t)
	doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"out@example.com","password":"correct horse battery staple"}`)
	doCloud(t, s, http.MethodPost, "/api/cloud/logout", "")

	code, body := doCloud(t, s, http.MethodGet, "/api/cloud/status", "")
	if code != http.StatusOK || body["logged_in"] != false {
		t.Fatalf("status after logout = %d %v", code, body)
	}
	code, _ = doCloud(t, s, http.MethodPost, "/api/cloud/sync", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("sync after logout code = %d, want 401", code)
	}
}

func TestCloudSyncRunsOrchestration(t *testing.T) {
	s, _ := newCloudTestServer(t)
	doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"sync@example.com","password":"correct horse battery staple"}`)

	var called bool
	s.syncNow = func() (*cloudsync.SyncResult, error) {
		called = true
		return &cloudsync.SyncResult{Pushed: []string{"meet-1"}}, nil
	}
	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/sync", "")
	if code != http.StatusOK {
		t.Fatalf("sync code = %d, body = %v", code, body)
	}
	if !called {
		t.Fatal("sync orchestration was not invoked")
	}
	pushed, _ := body["pushed"].([]any)
	if len(pushed) != 1 || pushed[0] != "meet-1" {
		t.Fatalf("sync response pushed = %v", body["pushed"])
	}

	code, body = doCloud(t, s, http.MethodGet, "/api/cloud/status", "")
	if code != http.StatusOK || body["last_sync_at"] == nil {
		t.Fatalf("status after sync missing last_sync_at: %v", body)
	}
}

func TestCloudSignupMasterKeyNeverPersisted(t *testing.T) {
	s, _ := newCloudTestServer(t)
	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"key@example.com","password":"correct horse battery staple"}`)
	if code != http.StatusOK {
		t.Fatalf("signup failed: %v", body)
	}
	raw := readFile(t, mustConfigPath(t))
	if strings.Contains(raw, "master") || strings.Contains(raw, "session") {
		t.Fatalf("config.yaml mentions master/session material:\n%s", raw)
	}
}

// --- helpers: fake worker reuse from cloudsync's tests, plus small file
// utilities. The fake worker's definitions live in
// internal/cloudsync's client_test.go (package cloudsync); to avoid
// importing test code, cloud_test.go re-hosts a minimal stateful fake
// here that mirrors the same response shapes.

type cloudFakeWorker struct {
	users    map[string]cloudsync.SignupRequest // email -> stored signup fields
	sessions map[string]string
}

func (f *cloudFakeWorker) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/signup", func(w http.ResponseWriter, r *http.Request) {
		var req cloudsync.SignupRequest
		json.NewDecoder(r.Body).Decode(&req)
		if _, dup := f.users[req.Email]; dup {
			writeCloudJSON(w, http.StatusConflict, map[string]string{"error": "email already registered"})
			return
		}
		f.users[req.Email] = req
		token := "tok-" + req.Email
		f.sessions[token] = req.Email
		writeCloudJSON(w, http.StatusOK, map[string]any{"session_token": token, "expires_at": int64(1 << 40)})
	})
	mux.HandleFunc("/auth-params", func(w http.ResponseWriter, r *http.Request) {
		email := r.URL.Query().Get("email")
		if stored, ok := f.users[email]; ok {
			writeCloudJSON(w, http.StatusOK, map[string]string{"auth_salt": stored.AuthSalt, "kdf_params": stored.KDFParams})
			return
		}
		writeCloudJSON(w, http.StatusOK, map[string]string{
			"auth_salt":  "ZmFrZS1zYWx0LWZvci11bmtub3duLWVtYWlsLQ==",
			"kdf_params": `{"time":2,"memory":19456,"parallelism":1,"key_len":32}`,
		})
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			AuthHash string `json:"auth_hash"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		stored, ok := f.users[req.Email]
		if !ok || stored.AuthHash != req.AuthHash {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		token := "tok-" + req.Email
		f.sessions[token] = req.Email
		writeCloudJSON(w, http.StatusOK, map[string]any{
			"session_token": token, "expires_at": int64(1 << 40),
			"wrapped_master_key": stored.WrappedMasterKey, "key_wrap_params": stored.KeyWrapParams,
		})
	})
	return mux
}

func writeCloudJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func httptestNewCloudWorker(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer((&cloudFakeWorker{users: map[string]cloudsync.SignupRequest{}, sessions: map[string]string{}}).handler())
	t.Cleanup(server.Close)
	return server
}

func mustConfigPath(t *testing.T) string {
	t.Helper()
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
