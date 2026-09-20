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
	"strings"
	"sync"
	"testing"
)

// fakeWorker is a stateful in-memory stand-in for the catchm-cloud
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
}

type fakeUser struct {
	email             string
	authHash          string
	authSalt          string
	kdfParams         string
	wrappedMasterKey  string
	keyWrapParams     string
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

func (f *fakeWorker) handleList(w http.ResponseWriter, r *http.Request) {
	if _, ok := f.sessionUser(w, r); !ok {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []map[string]any{}
	for id, b := range f.blobs {
		out = append(out, map[string]any{
			"id": id, "version": b.version, "checksum": b.checksum,
			"size_bytes": len(b.data), "updated_at": b.updatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
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

func newTestClient(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(newFakeWorker().handler())
	t.Cleanup(server.Close)
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
		WrappedMasterKey: "w", KeyWrapParams: "{}", RecoveryBlob: "r", RecoveryKDFParams: "{}",
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
		WrappedMasterKey: "w", KeyWrapParams: "{}", RecoveryBlob: "r", RecoveryKDFParams: "{}",
	})
	if err == nil || !strings.Contains(err.Error(), "409") {
		t.Fatalf("duplicate signup err = %v, want 409", err)
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
