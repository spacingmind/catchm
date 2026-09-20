package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/spacingmind/catchm/internal/cloudsync"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/e2ee"
)

// cloudState is the in-memory-only session: session token, master key,
// and the email it belongs to. It is never written to disk in any
// form — a server restart requires logging in again. That's a
// deliberate simplicity choice: persisting the master key (even
// wrapped) reopens the "where does the wrapping key live" question,
// and M14's device-linking is the real answer for new devices anyway.
type cloudState struct {
	email        string
	sessionToken string
	masterKey    []byte
	loggedInAt   time.Time
	lastSyncAt   time.Time
}

// syncNow is the seam cloud_test.go uses to run a fake Sync instead of
// the real orchestration (which needs a store and a reachable backend).
type syncNow func() (*cloudsync.SyncResult, error)

func (s *Server) handleCloudSignup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Email == "" || req.Password == "" {
		httpError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	secrets, err := e2ee.SetupAccount(req.Password)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	client := s.cloudClient()
	token, _, err := client.Signup(cloudsync.SignupRequest{
		Email:             req.Email,
		AuthHash:          base64.StdEncoding.EncodeToString(secrets.AuthHash),
		AuthSalt:          base64.StdEncoding.EncodeToString(secrets.AuthSalt),
		KDFParams:         mustJSON(secrets.KDFParams),
		WrappedMasterKey:  base64.StdEncoding.EncodeToString(secrets.WrappedMasterKey),
		KeyWrapParams:     "{}",
		RecoveryBlob:      base64.StdEncoding.EncodeToString(secrets.RecoveryBlob),
		RecoveryKDFParams: mustJSON(secrets.RecoveryKDFParams),
	})
	if err != nil {
		respondCloudErr(w, err)
		return
	}

	s.saveCloudConfig(req.Email, base64.StdEncoding.EncodeToString(secrets.AuthSalt), mustJSON(secrets.KDFParams))
	s.mu.Lock()
	s.cloud = &cloudState{
		email:        req.Email,
		sessionToken: token,
		masterKey:    secrets.MasterKey,
		loggedInAt:   time.Now(),
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"email":         req.Email,
		"recovery_code": secrets.RecoveryCode,
	})
}

func (s *Server) handleCloudLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Email == "" || req.Password == "" {
		httpError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	client := s.cloudClient()
	salt, kdfParams, err := client.AuthParams(req.Email)
	if err != nil {
		respondCloudErr(w, err)
		return
	}
	authSalt, err := base64.StdEncoding.DecodeString(salt)
	if err != nil {
		httpError(w, http.StatusBadGateway, "server returned malformed auth salt")
		return
	}
	var params e2ee.KDFParams
	if err := json.Unmarshal([]byte(kdfParams), &params); kdfParams != "" && err != nil {
		httpError(w, http.StatusBadGateway, "server returned malformed kdf params")
		return
	}
	authKey := e2ee.DeriveAuthKey(req.Password, authSalt, params)
	token, wrappedMasterKey, _, err := client.Login(req.Email, base64.StdEncoding.EncodeToString(e2ee.DeriveAuthHash(authKey)))
	if err != nil {
		respondCloudErr(w, err)
		return
	}
	wmk, err := base64.StdEncoding.DecodeString(wrappedMasterKey)
	if err != nil {
		httpError(w, http.StatusBadGateway, "server returned malformed wrapped master key")
		return
	}
	masterKey, err := e2ee.UnlockWithPassword(req.Password, authSalt, params, wmk)
	if err != nil {
		// Right password, wrong salt/params (e.g. the anti-enumeration
		// fake values for an unknown email) — indistinguishable from
		// wrong credentials.
		httpError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	s.saveCloudConfig(req.Email, salt, kdfParams)
	s.mu.Lock()
	s.cloud = &cloudState{
		email:        req.Email,
		sessionToken: token,
		masterKey:    masterKey,
		loggedInAt:   time.Now(),
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"email": req.Email})
}

func (s *Server) handleCloudLogout(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.cloud = nil
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCloudStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	st := s.cloud
	s.mu.Unlock()
	if st == nil {
		writeJSON(w, http.StatusOK, map[string]any{"logged_in": false})
		return
	}
	lastSync := st.lastSyncAt
	writeJSON(w, http.StatusOK, map[string]any{
		"logged_in":    true,
		"email":        st.email,
		"logged_in_at": st.loggedInAt,
		"last_sync_at": lastSync,
	})
}

func (s *Server) handleCloudSync(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	st := s.cloud
	s.mu.Unlock()
	if st == nil {
		httpError(w, http.StatusUnauthorized, "not logged in")
		return
	}

	var result *cloudsync.SyncResult
	var err error
	if s.syncNow != nil {
		result, err = s.syncNow()
	} else {
		client := s.cloudClient()
		result, err = cloudsync.Sync(s.surface.Store(), client, st.sessionToken, st.masterKey)
	}
	if err != nil {
		respondCloudErr(w, err)
		return
	}
	s.mu.Lock()
	if s.cloud != nil {
		s.cloud.lastSyncAt = time.Now()
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, result)
}

// cloudClient builds a Client for the configured backend URL (via the
// constructor's newCloudClient seam when set — tests swap that to hit
// a fake backend).
func (s *Server) cloudClient() *cloudsync.Client {
	s.mu.Lock()
	fallback := s.newCloudClient
	url := s.cfg.Cloud.APIBaseURL
	s.mu.Unlock()
	if f := fallback; f != nil {
		return f()
	}
	return cloudsync.NewClient(url)
}

// saveCloudConfig persists the non-secret account parameters to
// config.yaml, following handlePutConfig's pointer-swap pattern.
func (s *Server) saveCloudConfig(email, salt, kdfParams string) {
	s.mu.Lock()
	updated := *s.cfg
	s.mu.Unlock()
	updated.Cloud.Email = email
	updated.Cloud.AuthSalt = salt
	updated.Cloud.KDFParams = kdfParams
	if err := config.Save(&updated); err != nil {
		return
	}
	if reloaded, err := config.Load(); err == nil {
		s.mu.Lock()
		s.cfg = reloaded
		s.mu.Unlock()
	}
}

func respondCloudErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cloudsync.ErrUnauthorized):
		httpError(w, http.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, cloudsync.ErrConflict):
		httpError(w, http.StatusConflict, "email already registered")
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		// KDFParams only holds fixed-shape numeric fields; Marshal
		// cannot fail for it.
		panic(fmt.Sprintf("marshal kdf params: %v", err))
	}
	return string(raw)
}
