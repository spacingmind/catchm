package cloudsync

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeWorker is a stateful in-memory stand-in for the catchm-api
// Worker, mirroring the real handlers' status codes and response
// bodies (see docs/plans/completed/m12-cloud-backend-auth.md and
// m13a-cloud-blob-storage.md Validation sections) closely enough to
// exercise every Client branch hermetically.
type fakeWorker struct {
	mu       sync.Mutex
	users    map[string]fakeUser  // email -> user
	sessions map[string]string    // token -> email
	blobs    map[string]*fakeBlob // blob id -> blob
	nextSeq  int
	// listPageSize forces small pages out of handleList so tests can
	// exercise next_cursor pagination; 0 means "return everything in
	// one page", matching the real server's default LIMIT of 500.
	listPageSize int
}

type fakeUser struct {
	email             string
	authHash          string
	authSalt          string
	kdfParams         string
	wrappedMasterKey  string
	keyWrapParams     string
	recoverySalt      string
	recoveryAuthHash  string
	recoveryBlob      string
	recoveryKDFParams string
}

type fakeBlob struct {
	data      []byte
	version   int
	checksum  string
	updatedAt int64
}

func newFakeWorker() *fakeWorker {
	return &fakeWorker{users: map[string]fakeUser{}, sessions: map[string]string{}, blobs: map[string]*fakeBlob{}}
}

func (f *fakeWorker) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/signup", f.handleSignup)
	mux.HandleFunc("/login", f.handleLogin)
	mux.HandleFunc("/auth-params", f.handleAuthParams)
	mux.HandleFunc("/recovery-params", f.handleRecoveryParams)
	mux.HandleFunc("/recover", f.handleRecover)
	mux.HandleFunc("/account/reset-password", f.handleResetPassword)
	mux.HandleFunc("/blobs", f.handleList)
	mux.HandleFunc("/blobs/", f.handleBlobItem)
	return mux
}

func (f *fakeWorker) sessionUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || len(header) <= len("Bearer ") {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"missing or malformed authorization header"}`)
		return "", false
	}
	email, ok := f.sessions[strings.TrimPrefix(header, "Bearer ")]
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"invalid or expired session"}`)
		return "", false
	}
	return email, true
}

func bearerToken(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func (f *fakeWorker) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"invalid JSON body"}`)
		return
	}
	if !strings.Contains(req.Email, "@") || !strings.Contains(req.Email, ".") {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"invalid email"}`)
		return
	}
	// Mirror the real Worker's SIGNUP_STRING_FIELDS validation: all
	// nine opaque fields must be non-empty strings. This is what makes
	// TestSignupSendsAllRequiredFields (internal/server) able to catch
	// a handler that forgets one.
	for _, v := range []string{
		req.AuthHash, req.AuthSalt, req.KDFParams,
		req.WrappedMasterKey, req.KeyWrapParams,
		req.RecoverySalt, req.RecoveryAuthHash, req.RecoveryBlob, req.RecoveryKDFParams,
	} {
		if v == "" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"missing or empty field"}`)
			return
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, dup := f.users[req.Email]; dup {
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, `{"error":"email already registered"}`)
		return
	}
	f.users[req.Email] = fakeUser{
		email:             req.Email,
		authHash:          req.AuthHash,
		authSalt:          req.AuthSalt,
		kdfParams:         req.KDFParams,
		wrappedMasterKey:  req.WrappedMasterKey,
		keyWrapParams:     req.KeyWrapParams,
		recoverySalt:      req.RecoverySalt,
		recoveryAuthHash:  req.RecoveryAuthHash,
		recoveryBlob:      req.RecoveryBlob,
		recoveryKDFParams: req.RecoveryKDFParams,
	}
	token := strings.Repeat(fmt.Sprintf("%08x", f.nextSeq), 8) // deterministic, unique per signup
	f.nextSeq++
	f.sessions[token] = req.Email
	writeJSON(w, http.StatusOK, map[string]any{"session_token": token, "expires_at": int64(1 << 40)})
}

func (f *fakeWorker) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		AuthHash string `json:"auth_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"invalid JSON body"}`)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	user, ok := f.users[req.Email]
	if !ok || user.authHash != req.AuthHash {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"invalid credentials"}`)
		return
	}
	token := strings.Repeat(fmt.Sprintf("%08x", f.nextSeq), 8)
	f.nextSeq++
	f.sessions[token] = req.Email
	writeJSON(w, http.StatusOK, map[string]any{
		"session_token":      token,
		"expires_at":         int64(1 << 40),
		"wrapped_master_key": user.wrappedMasterKey,
		"key_wrap_params":    user.keyWrapParams,
	})
}

func (f *fakeWorker) handleAuthParams(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	f.mu.Lock()
	defer f.mu.Unlock()
	if user, ok := f.users[email]; ok {
		writeJSON(w, http.StatusOK, map[string]any{"auth_salt": user.authSalt, "kdf_params": user.kdfParams})
		return
	}
	// Deterministic fake, same shape as the real anti-enumeration path.
	sum := sha256.Sum256([]byte(email + ":no-such-user-salt"))
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_salt":  hex.EncodeToString(sum[:]),
		"kdf_params": `{"algo":"argon2id","memory_kib":19456,"iterations":2,"parallelism":1}`,
	})
}

func (f *fakeWorker) handleRecoveryParams(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	f.mu.Lock()
	defer f.mu.Unlock()
	if user, ok := f.users[email]; ok && user.recoverySalt != "" {
		writeJSON(w, http.StatusOK, map[string]any{"recovery_salt": user.recoverySalt, "recovery_kdf_params": user.recoveryKDFParams})
		return
	}
	// Deterministic fake, distinct domain from auth-params' (mirrors the real Worker).
	sum := sha256.Sum256([]byte(email + ":no-such-user-recovery-salt"))
	writeJSON(w, http.StatusOK, map[string]any{
		"recovery_salt":       hex.EncodeToString(sum[:]),
		"recovery_kdf_params": `{"algo":"argon2id","memory_kib":19456,"iterations":2,"parallelism":1}`,
	})
}

func (f *fakeWorker) handleRecover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email            string `json:"email"`
		RecoveryAuthHash string `json:"recovery_auth_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"invalid JSON body"}`)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	user, ok := f.users[req.Email]
	if !ok || user.recoveryAuthHash != req.RecoveryAuthHash {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"invalid credentials"}`)
		return
	}
	token := strings.Repeat(fmt.Sprintf("%08x", f.nextSeq), 8)
	f.nextSeq++
	f.sessions[token] = req.Email
	writeJSON(w, http.StatusOK, map[string]any{
		"session_token":       token,
		"expires_at":          int64(1 << 40),
		"recovery_blob":       user.recoveryBlob,
		"recovery_kdf_params": user.recoveryKDFParams,
	})
}

func (f *fakeWorker) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if _, ok := f.sessionUser(w, r); !ok {
		return
	}
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"error":"invalid JSON body"}`)
		return
	}
	for _, v := range []string{req.AuthHash, req.AuthSalt, req.KDFParams, req.WrappedMasterKey, req.RecoverySalt, req.RecoveryAuthHash, req.RecoveryBlob, req.RecoveryKDFParams} {
		if v == "" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"missing or empty field"}`)
			return
		}
	}
	email := f.sessions[bearerToken(r)]
	f.mu.Lock()
	defer f.mu.Unlock()
	user := f.users[email]
	user.authHash = req.AuthHash
	user.authSalt = req.AuthSalt
	user.kdfParams = req.KDFParams
	user.wrappedMasterKey = req.WrappedMasterKey
	user.recoverySalt = req.RecoverySalt
	user.recoveryAuthHash = req.RecoveryAuthHash
	user.recoveryBlob = req.RecoveryBlob
	user.recoveryKDFParams = req.RecoveryKDFParams
	f.users[email] = user
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleList mirrors the real Worker's paginated shape (Part B.4):
// {"blobs": [...], "next_cursor": "<id>"|null}, ordered by id with
// `?cursor=<last id>` (WHERE id > ?) pagination.
func (f *fakeWorker) handleList(w http.ResponseWriter, r *http.Request) {
	if _, ok := f.sessionUser(w, r); !ok {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	ids := make([]string, 0, len(f.blobs))
	for id := range f.blobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	cursor := r.URL.Query().Get("cursor")
	start := 0
	if cursor != "" {
		start = len(ids)
		for i, id := range ids {
			if id > cursor {
				start = i
				break
			}
		}
	}
	limit := f.listPageSize
	if limit <= 0 {
		limit = 500
	}
	end := start + limit
	if end > len(ids) {
		end = len(ids)
	}
	page := ids[start:end]

	out := make([]map[string]any, 0, len(page))
	for _, id := range page {
		b := f.blobs[id]
		out = append(out, map[string]any{
			"id": id, "version": b.version, "checksum": b.checksum,
			"size_bytes": len(b.data), "updated_at": b.updatedAt,
		})
	}
	var nextCursor any
	if end < len(ids) {
		nextCursor = ids[end-1]
	}
	writeJSON(w, http.StatusOK, map[string]any{"blobs": out, "next_cursor": nextCursor})
}

func (f *fakeWorker) handleBlobItem(w http.ResponseWriter, r *http.Request) {
	if _, ok := f.sessionUser(w, r); !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/blobs/")
	f.mu.Lock()
	defer f.mu.Unlock()
	blob, exists := f.blobs[id]

	switch r.Method {
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		if hdr := r.Header.Get("X-Checksum"); hdr == "" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"missing or malformed X-Checksum header"}`)
			return
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != r.Header.Get("X-Checksum") {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":"checksum mismatch"}`)
			return
		}
		expected := 0
		fmt.Sscanf(r.Header.Get("X-Expected-Version"), "%d", &expected)
		if exists {
			if expected != blob.version {
				writeJSON(w, http.StatusConflict, map[string]any{
					"version": blob.version, "checksum": blob.checksum,
					"size_bytes": len(blob.data), "updated_at": blob.updatedAt,
				})
				return
			}
		} else if expected != 0 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "no such blob", "version": 0, "checksum": "",
				"size_bytes": 0, "updated_at": 0,
			})
			return
		}
		if !exists {
			blob = &fakeBlob{}
			f.blobs[id] = blob
		}
		blob.data = body
		blob.version = expected + 1
		blob.checksum = hex.EncodeToString(sum[:])
		blob.updatedAt = 12345
		writeJSON(w, http.StatusOK, map[string]any{"version": blob.version, "checksum": blob.checksum})
	case http.MethodGet:
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"not found"}`)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Version", fmt.Sprintf("%d", blob.version))
		w.Header().Set("X-Checksum", blob.checksum)
		w.Write(blob.data)
	case http.MethodDelete:
		if !exists {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":"not found"}`)
			return
		}
		delete(f.blobs, id)
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

// httptestNewServer/httptestSignup are thin renames so sync_test.go can
// reuse the fake Worker and signup helper against shared fixtures.
func httptestNewServer(t *testing.T, worker *fakeWorker) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(worker.handler())
	t.Cleanup(server.Close)
	return server
}

func httptestSignup(t *testing.T, c *Client, email string) string {
	return testSignup(t, c, email)
}

func newTestClient(t *testing.T) *Client {
	t.Helper()
	server := httptestNewServer(t, newFakeWorker())
	return NewClient(server.URL)
}

func testSignup(t *testing.T, c *Client, email string) string {
	t.Helper()
	token, _, err := c.Signup(SignupRequest{
		Email:             email,
		AuthHash:          "hash-" + email,
		AuthSalt:          "salt-" + email,
		KDFParams:         `{"time":2}`,
		WrappedMasterKey:  "wmk-" + email,
		KeyWrapParams:     `{"time":2}`,
		RecoverySalt:      "rsalt-" + email,
		RecoveryAuthHash:  "rhash-" + email,
		RecoveryBlob:      "rb-" + email,
		RecoveryKDFParams: `{"time":2}`,
	})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	return token
}

func TestAuthParamsReturnsStoredValues(t *testing.T) {
	c := newTestClient(t)
	testSignup(t, c, "known@example.com")

	salt, params, err := c.AuthParams("known@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if salt != "salt-known@example.com" || params != `{"time":2}` {
		t.Fatalf("auth-params returned %q / %q", salt, params)
	}
}

func TestAuthParamsUnknownEmailIsStillSuccess(t *testing.T) {
	c := newTestClient(t)
	salt, _, err := c.AuthParams("nobody@example.com")
	if err != nil {
		t.Fatalf("unknown email must not be a client-visible error: %v", err)
	}
	if len(salt) != 64 {
		t.Fatalf("fake salt shape = %q", salt)
	}
}

func TestSignupSuccess(t *testing.T) {
	c := newTestClient(t)
	token, expiresAt, err := c.Signup(SignupRequest{
		Email: "a@example.com", AuthHash: "h", AuthSalt: "s", KDFParams: "{}",
		WrappedMasterKey: "w", KeyWrapParams: "{}", RecoverySalt: "rs", RecoveryAuthHash: "rh",
		RecoveryBlob: "r", RecoveryKDFParams: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || expiresAt == 0 {
		t.Fatalf("signup returned %q / %d", token, expiresAt)
	}
	if _, err := c.ListBlobs(token); err != nil {
		t.Fatalf("signup session not usable: %v", err)
	}
}

func TestSignupDuplicateEmail(t *testing.T) {
	c := newTestClient(t)
	testSignup(t, c, "dup@example.com")
	_, _, err := c.Signup(SignupRequest{
		Email: "dup@example.com", AuthHash: "h", AuthSalt: "s", KDFParams: "{}",
		WrappedMasterKey: "w", KeyWrapParams: "{}", RecoverySalt: "rs", RecoveryAuthHash: "rh",
		RecoveryBlob: "r", RecoveryKDFParams: "{}",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate signup err = %v, want ErrConflict", err)
	}
}

func TestLoginReturnsWrappedKey(t *testing.T) {
	c := newTestClient(t)
	testSignup(t, c, "login@example.com")

	token, wmk, params, err := c.Login("login@example.com", "hash-login@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || wmk != "wmk-login@example.com" || params != `{"time":2}` {
		t.Fatalf("login returned %q %q %q", token, wmk, params)
	}
}

func TestLoginWrongHashFails(t *testing.T) {
	c := newTestClient(t)
	testSignup(t, c, "login2@example.com")
	if _, _, _, err := c.Login("login2@example.com", "wrong"); err == nil {
		t.Fatal("login with wrong hash unexpectedly succeeded")
	}
}

func TestPutGetBlobRoundTrip(t *testing.T) {
	c := newTestClient(t)
	token := testSignup(t, c, "blobs@example.com")

	plaintext := make([]byte, 100)
	rand.Read(plaintext)
	version, conflict, err := c.PutBlob(token, "meet-1", plaintext, 0)
	if err != nil || conflict != nil {
		t.Fatalf("put: err=%v conflict=%v", err, conflict)
	}
	if version != 1 {
		t.Fatalf("first put version = %d, want 1", version)
	}

	got, version, err := c.GetBlob(token, "meet-1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plaintext) || version != 1 {
		t.Fatalf("get returned %d bytes at version %d", len(got), version)
	}
}

func TestPutBlobConflictReturnsServerMeta(t *testing.T) {
	c := newTestClient(t)
	token := testSignup(t, c, "conflict@example.com")

	if _, _, err := c.PutBlob(token, "meet-1", []byte("v1"), 0); err != nil {
		t.Fatal(err)
	}
	// Stale expected-version on an existing row: 409 with real metadata.
	_, conflict, err := c.PutBlob(token, "meet-1", []byte("v2"), 7)
	if err != nil {
		t.Fatalf("conflict must not be a Go error, got %v", err)
	}
	if conflict == nil || conflict.Version != 1 {
		t.Fatalf("conflict meta = %+v, want server's current version 1", conflict)
	}
}

func TestPutBlobConflictOnMissingRow(t *testing.T) {
	c := newTestClient(t)
	token := testSignup(t, c, "missing@example.com")

	// Expected a version but the row doesn't exist: same conflict path,
	// version 0 — not a special error.
	_, conflict, err := c.PutBlob(token, "never-pushed", []byte("x"), 3)
	if err != nil {
		t.Fatalf("missing-row 409 must not be a Go error, got %v", err)
	}
	if conflict == nil || conflict.Version != 0 {
		t.Fatalf("conflict meta = %+v, want version 0", conflict)
	}
}

func TestPutBlobUnauthorized(t *testing.T) {
	c := newTestClient(t)
	_, _, err := c.PutBlob("bad-token", "meet-1", []byte("x"), 0)
	if !strings.Contains(fmt.Sprint(err), "401") {
		t.Fatalf("err = %v, want 401", err)
	}
}

func TestGetBlobNotFound(t *testing.T) {
	c := newTestClient(t)
	token := testSignup(t, c, "nf@example.com")
	if _, _, err := c.GetBlob(token, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestBlobEndpointsUnauthorized(t *testing.T) {
	c := newTestClient(t)
	if _, _, err := c.GetBlob("bad", "x"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("get err = %v, want ErrUnauthorized", err)
	}
	if _, err := c.ListBlobs("bad"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("list err = %v, want ErrUnauthorized", err)
	}
	if err := c.DeleteBlob("bad", "x"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("delete err = %v, want ErrUnauthorized", err)
	}
}

func TestListBlobs(t *testing.T) {
	c := newTestClient(t)
	token := testSignup(t, c, "list@example.com")
	if _, _, err := c.PutBlob(token, "b", []byte("data"), 0); err != nil {
		t.Fatal(err)
	}

	metas, err := c.ListBlobs(token)
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].ID != "b" || metas[0].Version != 1 {
		t.Fatalf("list = %+v", metas)
	}
	sum := sha256.Sum256([]byte("data"))
	if metas[0].Checksum != hex.EncodeToString(sum[:]) {
		t.Fatalf("checksum = %q", metas[0].Checksum)
	}
}

func TestDeleteBlob(t *testing.T) {
	c := newTestClient(t)
	token := testSignup(t, c, "del@example.com")
	if _, _, err := c.PutBlob(token, "meet-1", []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteBlob(token, "meet-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.GetBlob(token, "meet-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete err = %v, want ErrNotFound", err)
	}
	if err := c.DeleteBlob(token, "meet-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete err = %v, want ErrNotFound", err)
	}
}

func TestRecoveryParamsReturnsStoredValues(t *testing.T) {
	c := newTestClient(t)
	testSignup(t, c, "rp@example.com")

	salt, params, err := c.RecoveryParams("rp@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if salt != "rsalt-rp@example.com" || params != `{"time":2}` {
		t.Fatalf("recovery-params returned %q / %q", salt, params)
	}
}

func TestRecoveryParamsUnknownEmailIsStillSuccess(t *testing.T) {
	c := newTestClient(t)
	salt, _, err := c.RecoveryParams("nobody@example.com")
	if err != nil {
		t.Fatalf("unknown email must not be a client-visible error: %v", err)
	}
	if len(salt) != 64 { // same fake-salt shape as the real anti-enumeration path
		t.Fatalf("fake recovery salt shape = %q", salt)
	}
}

func TestRecoverReturnsBlobAndSession(t *testing.T) {
	c := newTestClient(t)
	testSignup(t, c, "rec@example.com")

	res, err := c.Recover(RecoverRequest{Email: "rec@example.com", RecoveryAuthHash: "rhash-rec@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	token, blob, params := res.SessionToken, res.RecoveryBlob, res.RecoveryKDFParams
	if token == "" {
		t.Fatal("no session token")
	}
	if blob != "rb-rec@example.com" || params != `{"time":2}` {
		t.Fatalf("recover returned %q / %q", blob, params)
	}
	// The recovery session works like a login session.
	if _, err := c.ListBlobs(token); err != nil {
		t.Fatalf("recovery session not usable: %v", err)
	}
}

func TestRecoverWrongHashFails(t *testing.T) {
	c := newTestClient(t)
	testSignup(t, c, "rec2@example.com")
	if _, err := c.Recover(RecoverRequest{Email: "rec2@example.com", RecoveryAuthHash: "wrong"}); err == nil {
		t.Fatal("recover with wrong hash unexpectedly succeeded")
	}
}

func TestResetPasswordOverwritesAndRotates(t *testing.T) {
	c := newTestClient(t)
	testSignup(t, c, "reset@example.com")
	_, err := c.Recover(RecoverRequest{Email: "reset@example.com", RecoveryAuthHash: "rhash-reset@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Grab the recovery session token via the fake's deterministic scheme is
	// not possible (counter-based); instead use Recover's returned token.
	rec, err := c.Recover(RecoverRequest{Email: "reset@example.com", RecoveryAuthHash: "rhash-reset@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	token := rec.SessionToken

	err = c.ResetPassword(token, ResetPasswordRequest{
		AuthHash: "new-hash", AuthSalt: "new-salt", KDFParams: `{"t":9}`,
		WrappedMasterKey: "new-wmk",
		RecoverySalt:     "new-rsalt", RecoveryAuthHash: "new-rhash",
		RecoveryBlob: "new-rb", RecoveryKDFParams: `{"t":9}`,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Old password hash no longer logs in; new one does.
	if _, _, _, err := c.Login("reset@example.com", "hash-reset@example.com"); err == nil {
		t.Fatal("old auth_hash still logs in after reset")
	}
	if _, _, _, err := c.Login("reset@example.com", "new-hash"); err != nil {
		t.Fatalf("new auth_hash rejected after reset: %v", err)
	}
	// Old recovery hash no longer recovers; new one returns the rotated blob.
	if _, err := c.Recover(RecoverRequest{Email: "reset@example.com", RecoveryAuthHash: "rhash-reset@example.com"}); err == nil {
		t.Fatal("old recovery hash still recovers after reset")
	}
	rotated, err := c.Recover(RecoverRequest{Email: "reset@example.com", RecoveryAuthHash: "new-rhash"})
	if err != nil {
		t.Fatal(err)
	}
	if rotated.RecoveryBlob != "new-rb" {
		t.Fatalf("rotated blob = %q", rotated.RecoveryBlob)
	}
	// Recovery params serve the rotated salt.
	salt, _, err := c.RecoveryParams("reset@example.com")
	if err != nil || salt != "new-rsalt" {
		t.Fatalf("recovery params after reset = %q, %v", salt, err)
	}
}

func TestResetPasswordRequiresSession(t *testing.T) {
	c := newTestClient(t)
	req := ResetPasswordRequest{
		AuthHash: "h", AuthSalt: "s", KDFParams: "{}", WrappedMasterKey: "w",
		RecoverySalt: "rs", RecoveryAuthHash: "rh", RecoveryBlob: "rb", RecoveryKDFParams: "{}",
	}
	if err := c.ResetPassword("bogus-token", req); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestLoginRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":"too many attempts"}`)
	}))
	t.Cleanup(server.Close)
	c := NewClient(server.URL)

	_, _, _, err := c.Login("x@example.com", "h")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if !strings.Contains(err.Error(), "60") {
		t.Fatalf("Retry-After not surfaced: %v", err)
	}
}

func TestPutBlobQuotaExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInsufficientStorage)
		io.WriteString(w, `{"error":"storage quota exceeded","used":1000,"quota":1000}`)
	}))
	t.Cleanup(server.Close)
	c := NewClient(server.URL)

	_, conflict, err := c.PutBlob("tok", "meet-1", []byte("x"), 0)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("err = %v (conflict=%v), want ErrQuotaExceeded", err, conflict)
	}
	if conflict != nil {
		t.Fatal("507 must not be treated as a conflict")
	}
}

func TestGetBlobRateLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":"too many attempts"}`)
	}))
	t.Cleanup(server.Close)
	c := NewClient(server.URL)

	_, _, err := c.GetBlob("tok", "meet-1")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestPutBlobTooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		io.WriteString(w, `{"error":"blob too large"}`)
	}))
	t.Cleanup(server.Close)
	c := NewClient(server.URL)

	_, conflict, err := c.PutBlob("tok", "meet-1", []byte("x"), 0)
	if !errors.Is(err, ErrBlobTooLarge) {
		t.Fatalf("err = %v (conflict=%v), want ErrBlobTooLarge", err, conflict)
	}
}

func TestPutBlobSendsAccurateContentLength(t *testing.T) {
	data := make([]byte, 12345)
	rand.Read(data)

	var gotLen int64 = -1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLen = r.ContentLength
		body, _ := io.ReadAll(r.Body)
		sum := sha256.Sum256(body)
		writeJSON(w, http.StatusOK, map[string]any{"version": 1, "checksum": hex.EncodeToString(sum[:])})
	}))
	t.Cleanup(server.Close)
	c := NewClient(server.URL)

	if _, _, err := c.PutBlob("tok", "meet-1", data, 0); err != nil {
		t.Fatal(err)
	}
	if gotLen != int64(len(data)) {
		t.Fatalf("Content-Length = %d, want %d", gotLen, len(data))
	}
}

func TestListBlobsFollowsCursorAcrossPages(t *testing.T) {
	worker := newFakeWorker()
	worker.listPageSize = 1 // force pagination across every one of the blobs below
	server := httptestNewServer(t, worker)
	c := NewClient(server.URL)
	token := testSignup(t, c, "page@example.com")

	for _, id := range []string{"a", "b", "c"} {
		if _, _, err := c.PutBlob(token, id, []byte(id), 0); err != nil {
			t.Fatal(err)
		}
	}

	metas, err := c.ListBlobs(token)
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("got %d blobs, want 3: %+v", len(metas), metas)
	}
	gotIDs := map[string]bool{}
	for _, m := range metas {
		gotIDs[m.ID] = true
	}
	for _, id := range []string{"a", "b", "c"} {
		if !gotIDs[id] {
			t.Fatalf("missing blob %q across pages: %+v", id, metas)
		}
	}
}

func TestListBlobsMissingBlobsKeyIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"unrelated": true})
	}))
	t.Cleanup(server.Close)
	c := NewClient(server.URL)

	if _, err := c.ListBlobs("tok"); err == nil {
		t.Fatal("expected an error for a response with no \"blobs\" key (no bare-array fallback)")
	}
}

func TestListBlobsCursorMustAdvance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always claims more pages at the same cursor: a buggy server
		// that would otherwise loop the client forever.
		writeJSON(w, http.StatusOK, map[string]any{"blobs": []map[string]any{}, "next_cursor": "stuck"})
	}))
	t.Cleanup(server.Close)
	c := NewClient(server.URL)

	if _, err := c.ListBlobs("tok"); err == nil {
		t.Fatal("expected an error when next_cursor does not advance")
	}
}
