package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/cloudsync"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/tools"
)

// newCloudTestServer builds a Server whose cloud client hits the shared
// fake Worker, with CATCHM_HOME isolated to a temp dir so config.yaml
// persistence is real and observable.
func newCloudTestServer(t *testing.T) (*Server, *cloudFakeWorker) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)

	fake := httptestNewCloudWorker(t)
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(tools.New(store), &config.Config{Cloud: config.CloudConfig{APIBaseURL: fake.url}})
	s.newCloudClient = func() *cloudsync.Client { return cloudsync.NewClient(fake.url) }
	return s, fake
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
	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"ghost@example.com","password":"whatever"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("unknown-email login code = %d, want 401", code)
	}
	if body["code"] != "invalid_credentials" {
		t.Fatalf("code = %v, want invalid_credentials", body["code"])
	}
	code, body = doCloud(t, s, http.MethodGet, "/api/cloud/status", "")
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
	users      map[string]cloudsync.SignupRequest // email -> stored signup fields
	sessions   map[string]string
	url        string
	resetCalls int
	blobs      map[string][]byte // "<email>/<id>" -> content
	blobVers   map[string]int    // "<email>/<id>" -> version

	// Auth hardening (cloud_security_test.go). hardenedSignup: /signup
	// answers 202 and leaves the email unverified. unverified: login and
	// recover need email_code "123456". totp: login needs totp_code
	// "654321" (also the code /totp/enable and /disable accept).
	hardenedSignup bool
	unverified     map[string]bool
	totp           map[string]bool
	totpPending    map[string]bool
	lastRecover    map[string]any
	resendStatus   int // /verify/resend answers this (0: 202)
}

// fakeSessionID is the public id the fake gives a session token.
func fakeSessionID(token string) string { return "sid-" + token }

// fakeAuthGate is the hardened Worker's email-code / TOTP gate; it
// reports whether it answered.
func (f *cloudFakeWorker) fakeAuthGate(w http.ResponseWriter, email, emailCode, totpCode string, checkTOTP bool) bool {
	if f.unverified[email] {
		switch emailCode {
		case "":
			writeCloudJSON(w, http.StatusForbidden, map[string]string{"error": "verification_required", "challenge": "email_code"})
			return true
		case "123456":
			delete(f.unverified, email)
		default:
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_code", "challenge": "email_code"})
			return true
		}
	}
	if checkTOTP && f.totp[email] {
		switch totpCode {
		case "":
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "totp_required", "challenge": "totp"})
			return true
		case "654321":
		default:
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_code", "challenge": "totp"})
			return true
		}
	}
	return false
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
		if f.hardenedSignup {
			f.unverified[req.Email] = true
			writeCloudJSON(w, http.StatusAccepted, map[string]any{"verification_required": true})
			return
		}
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
			Email     string `json:"email"`
			AuthHash  string `json:"auth_hash"`
			EmailCode string `json:"email_code"`
			TOTPCode  string `json:"totp_code"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		stored, ok := f.users[req.Email]
		if !ok || stored.AuthHash != req.AuthHash {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		if f.fakeAuthGate(w, req.Email, req.EmailCode, req.TOTPCode, true) {
			return
		}
		token := "tok-" + req.Email
		f.sessions[token] = req.Email
		writeCloudJSON(w, http.StatusOK, map[string]any{
			"session_token": token, "session_id": fakeSessionID(token), "expires_at": int64(1 << 40),
			"wrapped_master_key": stored.WrappedMasterKey, "key_wrap_params": stored.KeyWrapParams,
		})
	})
	mux.HandleFunc("/recovery-params", func(w http.ResponseWriter, r *http.Request) {
		email := r.URL.Query().Get("email")
		if stored, ok := f.users[email]; ok {
			writeCloudJSON(w, http.StatusOK, map[string]string{
				"recovery_salt": stored.RecoverySalt, "recovery_kdf_params": stored.RecoveryKDFParams,
			})
			return
		}
		writeCloudJSON(w, http.StatusOK, map[string]string{
			"recovery_salt": "ZmFrZS1yZWNvdmVyeS1zYWx0", "recovery_kdf_params": `{"time":2,"memory":19456,"parallelism":1,"key_len":32}`,
		})
	})
	mux.HandleFunc("/recover", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email            string `json:"email"`
			RecoveryAuthHash string `json:"recovery_auth_hash"`
			EmailCode        string `json:"email_code"`
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &req)
		f.lastRecover = map[string]any{}
		json.Unmarshal(raw, &f.lastRecover)
		stored, ok := f.users[req.Email]
		if !ok || stored.RecoveryAuthHash != req.RecoveryAuthHash {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		if f.fakeAuthGate(w, req.Email, req.EmailCode, "", false) {
			return
		}
		token := "rtok-" + stored.Email
		f.sessions[token] = stored.Email
		writeCloudJSON(w, http.StatusOK, map[string]any{
			"session_token": token, "session_id": fakeSessionID(token), "expires_at": int64(1 << 40),
			"recovery_blob": stored.RecoveryBlob, "recovery_kdf_params": stored.RecoveryKDFParams,
		})
	})
	mux.HandleFunc("/account/reset-password", func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		email, ok := f.sessions[bearer]
		if !ok || !strings.HasPrefix(bearer, "rtok-") && !strings.HasPrefix(bearer, "tok-") {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired session"})
			return
		}
		var req cloudsync.ResetPasswordRequest
		json.NewDecoder(r.Body).Decode(&req)
		for _, v := range []string{req.AuthHash, req.AuthSalt, req.KDFParams, req.WrappedMasterKey,
			req.RecoverySalt, req.RecoveryAuthHash, req.RecoveryBlob, req.RecoveryKDFParams} {
			if v == "" {
				writeCloudJSON(w, http.StatusBadRequest, map[string]string{"error": "missing or empty field"})
				return
			}
		}
		f.resetCalls++
		stored := f.users[email]
		stored.AuthHash = req.AuthHash
		stored.AuthSalt = req.AuthSalt
		stored.KDFParams = req.KDFParams
		stored.WrappedMasterKey = req.WrappedMasterKey
		stored.RecoverySalt = req.RecoverySalt
		stored.RecoveryAuthHash = req.RecoveryAuthHash
		stored.RecoveryBlob = req.RecoveryBlob
		stored.RecoveryKDFParams = req.RecoveryKDFParams
		f.users[email] = stored
		writeCloudJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/account/change-password", func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		email, ok := f.sessions[bearer]
		if !ok {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired session"})
			return
		}
		var req cloudsync.ChangePasswordRequest
		json.NewDecoder(r.Body).Decode(&req)
		stored := f.users[email]
		if req.CurrentAuthHash != stored.AuthHash {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		stored.AuthHash, stored.AuthSalt, stored.KDFParams, stored.WrappedMasterKey = req.AuthHash, req.AuthSalt, req.KDFParams, req.WrappedMasterKey
		f.users[email] = stored
		signedOut := 0
		if req.SignOutOthers {
			for tok, e := range f.sessions {
				if e == email && tok != bearer {
					delete(f.sessions, tok)
					signedOut++
				}
			}
		}
		writeCloudJSON(w, http.StatusOK, map[string]any{"status": "ok", "signed_out": signedOut})
	})
	mux.HandleFunc("POST /verify/resend", func(w http.ResponseWriter, r *http.Request) {
		switch f.resendStatus {
		case http.StatusTooManyRequests:
			writeCloudJSON(w, http.StatusTooManyRequests, map[string]any{"error": "resend_throttled", "retry_after": 42})
		case http.StatusConflict:
			writeCloudJSON(w, http.StatusConflict, map[string]string{"error": "already_verified"})
		default:
			writeCloudJSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
		}
	})
	mux.HandleFunc("GET /sessions", func(w http.ResponseWriter, r *http.Request) {
		email, ok := f.sessionEmail(r)
		if !ok {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired session"})
			return
		}
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		out := []map[string]any{}
		for token, owner := range f.sessions {
			if owner == email {
				out = append(out, map[string]any{
					"id": fakeSessionID(token), "device_name": "Device " + token,
					"created_at": 1, "last_seen_at": 2, "current": token == bearer,
				})
			}
		}
		writeCloudJSON(w, http.StatusOK, map[string]any{"sessions": out})
	})
	mux.HandleFunc("DELETE /sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		email, ok := f.sessionEmail(r)
		if !ok {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired session"})
			return
		}
		for token, owner := range f.sessions {
			if owner == email && fakeSessionID(token) == r.PathValue("id") {
				delete(f.sessions, token)
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		writeCloudJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	})
	mux.HandleFunc("POST /sessions/revoke-others", func(w http.ResponseWriter, r *http.Request) {
		email, ok := f.sessionEmail(r)
		if !ok {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired session"})
			return
		}
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		n := 0
		for token, owner := range f.sessions {
			if owner == email && token != bearer {
				delete(f.sessions, token)
				n++
			}
		}
		writeCloudJSON(w, http.StatusOK, map[string]int{"revoked": n})
	})
	mux.HandleFunc("POST /totp/{action}", func(w http.ResponseWriter, r *http.Request) {
		email, ok := f.sessionEmail(r)
		if !ok {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired session"})
			return
		}
		var req struct {
			Code string `json:"code"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch r.PathValue("action") {
		case "setup":
			if f.totp[email] {
				writeCloudJSON(w, http.StatusConflict, map[string]string{"error": "totp_already_enabled"})
				return
			}
			f.totpPending[email] = true
			writeCloudJSON(w, http.StatusOK, map[string]string{
				"secret": "JBSWY3DPEHPK3PXP", "otpauth_uri": "otpauth://totp/CatchM%3A" + email + "?secret=JBSWY3DPEHPK3PXP",
			})
		case "enable":
			switch {
			case f.totp[email]:
				writeCloudJSON(w, http.StatusConflict, map[string]string{"error": "totp_already_enabled"})
			case !f.totpPending[email]:
				writeCloudJSON(w, http.StatusConflict, map[string]string{"error": "totp_not_set_up"})
			case req.Code != "654321":
				writeCloudJSON(w, http.StatusForbidden, map[string]string{"error": "invalid_code"})
			default:
				f.totp[email] = true
				delete(f.totpPending, email)
				writeCloudJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			}
		case "disable":
			switch {
			case !f.totp[email]:
				writeCloudJSON(w, http.StatusConflict, map[string]string{"error": "totp_not_enabled"})
			case req.Code != "654321":
				writeCloudJSON(w, http.StatusForbidden, map[string]string{"error": "invalid_code"})
			default:
				delete(f.totp, email)
				writeCloudJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	// Minimal blob endpoints (enough for Sync in the isolation test).
	mux.HandleFunc("/blobs", func(w http.ResponseWriter, r *http.Request) {
		email, ok := f.sessionEmail(r)
		if !ok {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired session"})
			return
		}
		out := []map[string]any{}
		for key, content := range f.blobs {
			if parts := strings.SplitN(key, "/", 2); len(parts) == 2 && parts[0] == email {
				out = append(out, map[string]any{
					"id": parts[1], "version": f.blobVers[key],
					"checksum": "", "size_bytes": len(content), "updated_at": 1,
				})
			}
		}
		// Real Worker envelope (Part B.4): {"blobs":[...],"next_cursor":...}
		// — no bare-array fallback. A single page is enough here.
		writeCloudJSON(w, http.StatusOK, map[string]any{"blobs": out, "next_cursor": nil})
	})
	mux.HandleFunc("/blobs/", func(w http.ResponseWriter, r *http.Request) {
		email, ok := f.sessionEmail(r)
		if !ok {
			writeCloudJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired session"})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/blobs/")
		key := email + "/" + id
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			f.blobs[key] = body
			f.blobVers[key]++
			writeCloudJSON(w, http.StatusOK, map[string]any{"version": f.blobVers[key], "checksum": ""})
		case http.MethodGet:
			content, exists := f.blobs[key]
			if !exists {
				writeCloudJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
				return
			}
			w.Header().Set("X-Version", fmt.Sprintf("%d", f.blobVers[key]))
			w.Write(content)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return mux
}

// sessionEmail resolves the bearer token to the owning email.
func (f *cloudFakeWorker) sessionEmail(r *http.Request) (string, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	email, ok := f.sessions[token]
	return email, ok
}

func writeCloudJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func httptestNewCloudWorker(t *testing.T) *cloudFakeWorker {
	t.Helper()
	f := &cloudFakeWorker{
		users:    map[string]cloudsync.SignupRequest{},
		sessions: map[string]string{},
		blobs:    map[string][]byte{},
		blobVers: map[string]int{},

		unverified:  map[string]bool{},
		totp:        map[string]bool{},
		totpPending: map[string]bool{},
	}
	server := httptest.NewServer(f.handler())
	t.Cleanup(server.Close)
	f.url = server.URL
	return f
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

// Regression for the M15 signup gap: the server handler must send ALL
// fields the real Worker requires — the fake validates every one of
// them, so a handler forgetting RecoverySalt/RecoveryAuthHash fails
// here with a 400 instead of silently succeeding against a stub.
func TestCloudSignupSendsAllRequiredFields(t *testing.T) {
	s, fake := newCloudTestServer(t)
	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"fields@example.com","password":"correct horse battery staple"}`)
	if code != http.StatusOK {
		t.Fatalf("signup code = %d, body = %v", code, body)
	}

	stored, ok := fake.users["fields@example.com"]
	if !ok {
		t.Fatal("signup not stored in fake worker")
	}
	for name, v := range map[string]string{
		"auth_hash": stored.AuthHash, "auth_salt": stored.AuthSalt,
		"wrapped_master_key": stored.WrappedMasterKey,
		"recovery_salt":      stored.RecoverySalt, "recovery_auth_hash": stored.RecoveryAuthHash,
		"recovery_blob": stored.RecoveryBlob, "recovery_kdf_params": stored.RecoveryKDFParams,
	} {
		if v == "" {
			t.Fatalf("signup stored empty %s", name)
		}
	}
	// recovery_salt decodes back to 16 bytes (e2ee.saltLen).
	salt, err := base64.StdEncoding.DecodeString(stored.RecoverySalt)
	if err != nil || len(salt) != 16 {
		t.Fatalf("recovery_salt not base64 of 16 bytes: %v, %d bytes", err, len(salt))
	}
}

func TestCloudRecoverFlow(t *testing.T) {
	s, _ := newCloudTestServer(t)

	// Signup, capture the recovery code.
	_, body := doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"rec@example.com","password":"old password"}`)
	originalCode, _ := body["recovery_code"].(string)
	if originalCode == "" {
		t.Fatalf("signup returned no recovery code: %v", body)
	}

	// Simulate lost password: call recover with the code + a new password.
	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/recover",
		`{"email":"rec@example.com","recovery_code":"`+originalCode+`","new_password":"new password"}`)
	if code != http.StatusOK {
		t.Fatalf("recover code = %d, body = %v", code, body)
	}
	newCode, _ := body["recovery_code"].(string)
	if newCode == "" || newCode == originalCode {
		t.Fatalf("recover must return a NEW rotated code: %q vs %q", newCode, originalCode)
	}

	// Logged in immediately after recovery.
	code, body = doCloud(t, s, http.MethodGet, "/api/cloud/status", "")
	if code != http.StatusOK || body["logged_in"] != true || body["email"] != "rec@example.com" {
		t.Fatalf("status after recover = %d %v", code, body)
	}

	// New password works; old one doesn't.
	code, _ = doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"rec@example.com","password":"new password"}`)
	if code != http.StatusOK {
		t.Fatalf("login with new password after recover = %d", code)
	}
	code, _ = doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"rec@example.com","password":"old password"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("login with old password after recover = %d, want 401", code)
	}

	// The OLD recovery code no longer recovers (rotation).
	code, _ = doCloud(t, s, http.MethodPost, "/api/cloud/recover",
		`{"email":"rec@example.com","recovery_code":"`+originalCode+`","new_password":"again"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("recover with OLD code after rotation = %d, want 401", code)
	}
	// The NEW code does.
	code, _ = doCloud(t, s, http.MethodPost, "/api/cloud/recover",
		`{"email":"rec@example.com","recovery_code":"`+newCode+`","new_password":"third"}`)
	if code != http.StatusOK {
		t.Fatalf("recover with NEW code after rotation = %d", code)
	}
}

func TestCloudRecoverWrongCodeNeverResetsPassword(t *testing.T) {
	s, fake := newCloudTestServer(t)
	doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"wrongrec@example.com","password":"old password"}`)

	// Count reset-password calls the fake receives.
	before := fake.resetCalls
	code, _ := doCloud(t, s, http.MethodPost, "/api/cloud/recover",
		`{"email":"wrongrec@example.com","recovery_code":"AAAAA-BBBBB-CCCCC-DDDDD-EEEEE","new_password":"new"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("recover with wrong code = %d, want 401", code)
	}
	if fake.resetCalls != before {
		t.Fatalf("reset-password was called %d times despite wrong recovery code", fake.resetCalls-before)
	}

	// Original password still logs in — nothing was overwritten.
	code, _ = doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"wrongrec@example.com","password":"old password"}`)
	if code != http.StatusOK {
		t.Fatalf("original password broken by a failed recover attempt: %d", code)
	}
}

// 429 from the Worker on login surfaces as a 429 with a human message.
func TestCloudLoginRateLimitedSurfaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":"too many attempts"}`)
	}))
	t.Cleanup(server.Close)
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := New(tools.New(store), &config.Config{Cloud: config.CloudConfig{APIBaseURL: server.URL}})

	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"x@example.com","password":"p"}`)
	if code != http.StatusTooManyRequests {
		t.Fatalf("code = %d, want 429", code)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "Too many attempts") {
		t.Fatalf("message = %q", msg)
	}
}

// 507 from the Worker during sync surfaces as a 507 with a human message.
func TestCloudSyncQuotaExceededSurfaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/signup" {
			writeCloudJSON(w, http.StatusOK, map[string]any{"session_token": "tok", "expires_at": int64(1 << 40)})
			return
		}
		if r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/blobs/") {
			w.WriteHeader(http.StatusInsufficientStorage)
			io.WriteString(w, `{"error":"storage quota exceeded","used":1000,"quota":1000}`)
			return
		}
		writeCloudJSON(w, http.StatusOK, []any{})
	}))
	t.Cleanup(server.Close)
	store, err := meeting.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start("Quota meeting", time.Now().UTC(), meeting.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	s := New(tools.New(store), &config.Config{Cloud: config.CloudConfig{APIBaseURL: server.URL}})

	if code, body := doCloud(t, s, http.MethodPost, "/api/cloud/signup", `{"email":"q@example.com","password":"p"}`); code != http.StatusOK {
		t.Fatalf("signup failed against fake: %d %v", code, body)
	}

	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/sync", "")
	if code != http.StatusInsufficientStorage {
		t.Fatalf("code = %d, body = %v, want 507", code, body)
	}
	if msg, _ := body["error"].(string); !strings.Contains(msg, "Cloud storage is full") {
		t.Fatalf("message = %q", msg)
	}
}

// Test-only helpers shared with cloudserver_test.go: a bare fake worker
// (no Server wiring) plus direct signup, and manifest path resolution.
func newCloudTestWorkerOnly(t *testing.T) *cloudFakeWorker {
	t.Helper()
	return httptestNewCloudWorker(t)
}

func cloudTestSignupOn(t *testing.T, fake *cloudFakeWorker, email string) string {
	t.Helper()
	res, err := http.Post(fake.url+"/signup", "application/json",
		strings.NewReader(`{"email":"`+email+`","auth_hash":"h","auth_salt":"s","kdf_params":"{}","wrapped_master_key":"w","key_wrap_params":"{}","recovery_salt":"rs","recovery_auth_hash":"rh","recovery_blob":"rb","recovery_kdf_params":"{}"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		SessionToken string `json:"session_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.SessionToken == "" {
		t.Fatal("no session token from fake signup")
	}
	return out.SessionToken
}

func manifestPathFor(t *testing.T, serverURL string) string {
	t.Helper()
	// Mirrors cloudsync.statePath's layout for the given server URL.
	home := os.Getenv("CATCHM_HOME")
	sum := sha256.Sum256([]byte(serverURL))
	key := hex.EncodeToString(sum[:])[:16]
	return filepath.Join(home, fmt.Sprintf("cloud-sync-state-%s.json", key))
}

func timeNowUTC() time.Time { return time.Now().UTC() }

// A breached password gets a warning the user must confirm; it never
// blocks, and a failed lookup never warns.
func TestCloudSignupBreachedPasswordWarns(t *testing.T) {
	orig := passwordBreachCount
	t.Cleanup(func() { passwordBreachCount = orig })
	passwordBreachCount = func(context.Context, string) (int, error) { return 42, nil }

	s, _ := newTestServer(t)
	w := do(t, s, "POST", "/api/cloud/signup", `{"email":"a@example.com","password":"password"}`)
	var body map[string]any
	decode(t, w, &body)
	if w.Code != http.StatusConflict || body["code"] != "password_breached" || body["count"] != float64(42) {
		t.Fatalf("breached signup = %d %v", w.Code, body)
	}
	if !warnIfBreached(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), "password", false) {
		t.Fatal("warning not raised")
	}
	if warnIfBreached(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), "password", true) {
		t.Fatal("confirmed signup still warned")
	}
	passwordBreachCount = func(context.Context, string) (int, error) { return 0, context.DeadlineExceeded }
	if warnIfBreached(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), "password", false) {
		t.Fatal("a failed lookup warned")
	}
}

// Change password: the current one must be right; afterwards the new one
// logs in, the old one doesn't, and the recovery code still works.
func TestCloudChangePassword(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	s, _ := newCloudTestServer(t)
	_, body := doCloud(t, s, http.MethodPost, "/api/cloud/signup",
		`{"email":"chg@example.com","password":"old password"}`)
	recoveryCode, _ := body["recovery_code"].(string)
	if recoveryCode == "" {
		t.Fatalf("signup: %v", body)
	}

	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/change-password",
		`{"current_password":"not it","new_password":"brand new password"}`)
	if code != http.StatusForbidden || body["code"] != "wrong_password" {
		t.Fatalf("wrong current = %d %v", code, body)
	}
	// Still logged in after a wrong current password.
	if _, st := doCloud(t, s, http.MethodGet, "/api/cloud/status", ""); st["logged_in"] != true {
		t.Fatalf("logged out by a wrong current password: %v", st)
	}
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/change-password",
		`{"current_password":"old password","new_password":"old password"}`)
	if code != http.StatusBadRequest || body["code"] != "same_password" {
		t.Fatalf("same password = %d %v", code, body)
	}

	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/change-password",
		`{"current_password":"old password","new_password":"brand new password","sign_out_others":true}`)
	if code != http.StatusOK {
		t.Fatalf("change = %d %v", code, body)
	}
	if code, _ := doCloud(t, s, http.MethodPost, "/api/cloud/login", `{"email":"chg@example.com","password":"brand new password"}`); code != http.StatusOK {
		t.Fatalf("login with new password = %d", code)
	}
	if code, _ := doCloud(t, s, http.MethodPost, "/api/cloud/login", `{"email":"chg@example.com","password":"old password"}`); code != http.StatusUnauthorized {
		t.Fatalf("login with old password = %d", code)
	}
	if code, _ := doCloud(t, s, http.MethodPost, "/api/cloud/recover",
		`{"email":"chg@example.com","recovery_code":"`+recoveryCode+`","new_password":"third one here"}`); code != http.StatusOK {
		t.Fatalf("recovery code stopped working after a change = %d", code)
	}
}

func TestCloudChangePasswordNeedsLogin(t *testing.T) {
	s, _ := newCloudTestServer(t)
	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/change-password",
		`{"current_password":"a","new_password":"b"}`)
	if code != http.StatusUnauthorized || body["code"] != "not_logged_in" {
		t.Fatalf("not logged in = %d %v", code, body)
	}
}
