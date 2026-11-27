package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
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
	// sessionID is the session's public id (GET /sessions), so revoking
	// it from the devices list can sign this device out. Empty with
	// servers from before auth hardening.
	sessionID  string
	masterKey  []byte
	loggedInAt time.Time
	lastSyncAt time.Time
	// totpEnabled: two-factor is on for the account, as far as this
	// device knows. No endpoint reports it, so it comes from the login
	// (a TOTP challenge means on, none means off), from enable/disable
	// here, and from the server's totp_already_enabled /
	// totp_not_enabled answers. After a recover it starts false.
	totpEnabled bool
}

// syncNow is the seam cloud_test.go uses to run a fake Sync instead of
// the real orchestration (which needs a store and a reachable backend).
type syncNow func() (*cloudsync.SyncResult, error)

func (s *Server) handleCloudSignup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email          string `json:"email"`
		Password       string `json:"password"`
		AcceptBreached bool   `json:"accept_breached"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Email == "" || req.Password == "" {
		httpError(w, http.StatusBadRequest, "email and password are required")
		return
	}
	if warnIfBreached(w, r, req.Password, req.AcceptBreached) {
		return
	}

	secrets, err := e2ee.SetupAccount(req.Password)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	client := s.cloudClient()
	token, _, err := client.Signup(cloudsync.SignupRequest{
		Device:            s.cloudDevice(),
		Email:             req.Email,
		AuthHash:          base64.StdEncoding.EncodeToString(secrets.AuthHash),
		AuthSalt:          base64.StdEncoding.EncodeToString(secrets.AuthSalt),
		KDFParams:         mustJSON(secrets.KDFParams),
		WrappedMasterKey:  base64.StdEncoding.EncodeToString(secrets.WrappedMasterKey),
		KeyWrapParams:     "{}",
		RecoverySalt:      base64.StdEncoding.EncodeToString(secrets.RecoverySalt),
		RecoveryAuthHash:  base64.StdEncoding.EncodeToString(secrets.RecoveryAuthHash),
		RecoveryBlob:      base64.StdEncoding.EncodeToString(secrets.RecoveryBlob),
		RecoveryKDFParams: mustJSON(secrets.RecoveryKDFParams),
	})
	if err != nil {
		respondCloudErr(w, err)
		return
	}

	s.saveCloudConfig(req.Email, base64.StdEncoding.EncodeToString(secrets.AuthSalt), mustJSON(secrets.KDFParams))
	if token == "" {
		// Hardened server: the account exists but is unverified, and a
		// code went to the email. The UI shows the recovery code, then
		// logs in with the emailed code.
		writeJSON(w, http.StatusOK, map[string]any{
			"email":                 req.Email,
			"recovery_code":         secrets.RecoveryCode,
			"verification_required": true,
		})
		return
	}
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
		Email     string `json:"email"`
		Password  string `json:"password"`
		EmailCode string `json:"email_code"`
		TOTPCode  string `json:"totp_code"`
		// Resend asks for a new email code instead of logging in.
		Resend bool `json:"resend"`
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
	authHash := base64.StdEncoding.EncodeToString(e2ee.DeriveAuthHash(authKey))
	if req.Resend {
		if err := client.ResendCode(req.Email, authHash); err != nil {
			respondCloudErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"resent": true})
		return
	}
	totpCode := normalizeCode(req.TOTPCode)
	login, err := client.LoginWith(cloudsync.LoginRequest{
		Email: req.Email, AuthHash: authHash, Device: s.cloudDevice(),
		EmailCode: normalizeCode(req.EmailCode), TOTPCode: totpCode,
	})
	if err != nil {
		respondCloudErr(w, err)
		return
	}
	wmk, err := base64.StdEncoding.DecodeString(login.WrappedMasterKey)
	if err != nil {
		httpError(w, http.StatusBadGateway, "server returned malformed wrapped master key")
		return
	}
	masterKey, err := e2ee.UnlockWithPassword(req.Password, authSalt, params, wmk)
	if err != nil {
		// Right password, wrong salt/params (e.g. the anti-enumeration
		// fake values for an unknown email) — indistinguishable from
		// wrong credentials.
		httpErrorCode(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
		return
	}

	s.saveCloudConfig(req.Email, salt, kdfParams)
	s.mu.Lock()
	s.cloud = &cloudState{
		email:        req.Email,
		sessionToken: login.SessionToken,
		sessionID:    login.SessionID,
		masterKey:    masterKey,
		loggedInAt:   time.Now(),
		// The server asks every login for a TOTP code when two-factor
		// is on, and the UI only sends one when asked.
		totpEnabled: totpCode != "",
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{"email": req.Email})
}

// handleCloudLogout forgets the login here and ends the session on the
// server too (best-effort: offline or an older server just leaves it to
// expire), so it drops off the account's signed-in devices list.
func (s *Server) handleCloudLogout(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	st := s.cloud
	s.cloud = nil
	s.mu.Unlock()
	if st != nil && st.sessionToken != "" && st.sessionID != "" {
		_ = s.cloudClient().RevokeSession(st.sessionToken, st.sessionID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCloudStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	st := s.cloud
	base := s.cfg.Cloud.APIBaseURL
	// account_stored: the params a server change would wipe are on
	// disk, so the UI knows whether that change needs a confirm.
	accountStored := s.cfg.Cloud.Email != "" || s.cfg.Cloud.AuthSalt != "" || s.cfg.Cloud.KDFParams != ""
	var email string
	var loggedInAt, lastSync time.Time
	var totpEnabled bool
	if st != nil {
		email, loggedInAt, lastSync, totpEnabled = st.email, st.loggedInAt, st.lastSyncAt, st.totpEnabled
	}
	s.mu.Unlock()
	if st == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"logged_in":      false,
			"api_base_url":   base,
			"account_stored": accountStored,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"logged_in":    true,
		"email":        email,
		"logged_in_at": loggedInAt,
		"last_sync_at": lastSync,
		"api_base_url": base,
		// A live session is an account on this server, even if the
		// config write after login failed.
		"account_stored": true,
		"totp_enabled":   totpEnabled,
	})
}

func (s *Server) handleCloudSync(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	st := s.cloud
	s.mu.Unlock()
	if st == nil {
		httpErrorCode(w, http.StatusUnauthorized, "not_logged_in", "not logged in")
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

// handleCloudRecover is the full lost-password flow in one call:
// fetch recovery params, derive the recovery key and its auth hash
// locally, prove knowledge of the code to the Worker, unwrap the
// master key from the returned blob (a wrong code fails here, before
// any reset-password call is attempted), then re-wrap under the new
// password and a freshly rotated recovery code, push both halves to
// the Worker, and land logged-in with the new session. The response's
// recovery_code is the NEW one, same one-time-display contract as
// signup. An unverified account answers the email_code challenge like
// login does (no TOTP: the recovery code is the second secret); the UI
// retries with email_code, and retrying without it asks for a new code
// once the last one has expired.
func (s *Server) handleCloudRecover(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email        string `json:"email"`
		RecoveryCode string `json:"recovery_code"`
		NewPassword  string `json:"new_password"`
		EmailCode    string `json:"email_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.Email == "" || req.RecoveryCode == "" || req.NewPassword == "" {
		httpError(w, http.StatusBadRequest, "email, recovery_code and new_password are required")
		return
	}

	client := s.cloudClient()
	saltB64, kdfParams, err := client.RecoveryParams(req.Email)
	if err != nil {
		respondCloudErr(w, err)
		return
	}
	salt, err := base64.StdEncoding.DecodeString(saltB64)
	if err != nil {
		httpError(w, http.StatusBadGateway, "server returned malformed recovery salt")
		return
	}
	var params e2ee.KDFParams
	if kdfParams != "" {
		if err := json.Unmarshal([]byte(kdfParams), &params); err != nil {
			httpError(w, http.StatusBadGateway, "server returned malformed kdf params")
			return
		}
	}
	recoveryKey := e2ee.DeriveRecoveryKey(req.RecoveryCode, salt, params)

	rec, err := client.Recover(cloudsync.RecoverRequest{
		Email:            req.Email,
		RecoveryAuthHash: base64.StdEncoding.EncodeToString(e2ee.DeriveRecoveryAuthHash(recoveryKey)),
		Device:           s.cloudDevice(),
		EmailCode:        normalizeCode(req.EmailCode),
	})
	if err != nil {
		respondCloudErr(w, err) // wrong code → 401 from the Worker (fake salt can't match)
		return
	}
	token := rec.SessionToken
	blob, err := base64.StdEncoding.DecodeString(rec.RecoveryBlob)
	if err != nil {
		httpError(w, http.StatusBadGateway, "server returned malformed recovery blob")
		return
	}
	masterKey, err := e2ee.RecoverMasterKey(req.RecoveryCode, salt, params, blob)
	if err != nil {
		// Right email, wrong code: the AEAD open on the real blob fails.
		httpErrorCode(w, http.StatusUnauthorized, "invalid_recovery_code", "invalid recovery code")
		return
	}

	pw, err := e2ee.RewrapForPassword(masterKey, req.NewPassword)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rw, err := e2ee.RewrapForRecovery(masterKey)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	err = client.ResetPassword(token, cloudsync.ResetPasswordRequest{
		AuthHash:          base64.StdEncoding.EncodeToString(pw.AuthHash),
		AuthSalt:          base64.StdEncoding.EncodeToString(pw.AuthSalt),
		KDFParams:         mustJSON(pw.KDFParams),
		WrappedMasterKey:  base64.StdEncoding.EncodeToString(pw.WrappedMasterKey),
		RecoverySalt:      base64.StdEncoding.EncodeToString(rw.RecoverySalt),
		RecoveryAuthHash:  base64.StdEncoding.EncodeToString(rw.RecoveryAuthHash),
		RecoveryBlob:      base64.StdEncoding.EncodeToString(rw.RecoveryBlob),
		RecoveryKDFParams: mustJSON(rw.RecoveryKDFParams),
	})
	if err != nil {
		respondCloudErr(w, err)
		return
	}

	s.mu.Lock()
	s.cloud = &cloudState{
		email:        req.Email,
		sessionToken: token,
		sessionID:    rec.SessionID,
		masterKey:    masterKey,
		loggedInAt:   time.Now(),
	}
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"email":               req.Email,
		"recovery_code":       rw.RecoveryCode,
		"two_factor_disabled": rec.TwoFactorDisabled,
	})
}

// validateCloudServerURL validates a user-supplied cloud base URL.
// Rules (M18 spec): must be absolute; https for any host; plain http
// only for loopback (localhost, 127.0.0.0/8, ::1) — auth_hash is
// replayable for login and session tokens are bearer credentials, so
// they must never cross the network in plaintext to a remote host.
// Userinfo, query, and fragment are rejected. The trailing slash is
// trimmed as normalization.
func validateCloudServerURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid URL")
	}
	if !u.IsAbs() || u.Host == "" {
		return "", fmt.Errorf("must be an absolute URL")
	}
	if u.User != nil {
		return "", fmt.Errorf("userinfo not allowed")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", fmt.Errorf("query and fragment not allowed")
	}
	switch u.Scheme {
	case "https":
		// any host
	case "http":
		host := u.Hostname()
		ip := net.ParseIP(host)
		if host == "localhost" || (ip != nil && (ip.IsLoopback())) {
			// loopback ok
		} else {
			return "", fmt.Errorf("http is only allowed for localhost")
		}
	default:
		return "", fmt.Errorf("scheme must be http or https")
	}
	return trimmed, nil
}

// handleCloudServer sets which catchm-api instance this install
// talks to. Only while logged out: the session, master key, stored
// account params, and sync manifest all belong to one server.
func (s *Server) handleCloudServer(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	loggedIn := s.cloud != nil
	s.mu.Unlock()
	if loggedIn {
		httpErrorCode(w, http.StatusConflict, "logout_required", "log out before changing the cloud server")
		return
	}

	var req struct {
		APIBaseURL string `json:"api_base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	normalized, err := validateCloudServerURL(req.APIBaseURL)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	updated := *s.cfg
	s.mu.Unlock()
	// The account params identify an account on the OLD server; a new
	// server means a fresh signup/login.
	updated.Cloud.Email = ""
	updated.Cloud.AuthSalt = ""
	updated.Cloud.KDFParams = ""
	updated.Cloud.APIBaseURL = normalized
	if err := config.Save(&updated); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if reloaded, err := config.Load(); err == nil {
		s.mu.Lock()
		s.cfg = reloaded
		s.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]string{"api_base_url": normalized})
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

// cloudDevice returns this installation's device identity, creating and
// saving the id on first use. The name is the computer's hostname.
func (s *Server) cloudDevice() cloudsync.Device {
	s.mu.Lock()
	id := s.cfg.Cloud.DeviceID
	s.mu.Unlock()
	if id == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		id = hex.EncodeToString(b)
		s.mu.Lock()
		updated := *s.cfg
		s.mu.Unlock()
		updated.Cloud.DeviceID = id
		if config.Save(&updated) == nil {
			if reloaded, err := config.Load(); err == nil {
				s.mu.Lock()
				s.cfg = reloaded
				s.mu.Unlock()
			}
		}
	}
	name, _ := os.Hostname()
	if name == "" {
		name = "CatchM"
	}
	return cloudsync.Device{ID: id, Name: name}
}

// breachCheckTimeout bounds the HIBP lookup; a slow or failed lookup
// never blocks signup, it just skips the warning.
const breachCheckTimeout = 3 * time.Second

// passwordBreachCount is the HIBP lookup; a var so tests stay offline.
var passwordBreachCount = func(ctx context.Context, password string) (int, error) {
	return cloudsync.PasswordBreachCount(ctx, nil, "", password)
}

// warnIfBreached answers 409 {code:"password_breached", count} when the
// password is in known breaches and the user hasn't confirmed yet. It
// reports whether it answered.
func warnIfBreached(w http.ResponseWriter, r *http.Request, password string, confirmed bool) bool {
	if confirmed {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), breachCheckTimeout)
	defer cancel()
	n, err := passwordBreachCount(ctx, password)
	if err != nil || n == 0 {
		return false
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": "This password has appeared in known data breaches. Choose another, or confirm to use it anyway.",
		"code":  "password_breached",
		"count": n,
	})
	return true
}

func respondCloudErr(w http.ResponseWriter, err error) {
	var ch *cloudsync.ChallengeError
	if errors.As(err, &ch) {
		// The UI asks for the code and retries with it.
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":     challengeMessage(ch),
			"code":      "challenge",
			"challenge": ch.Challenge,
			"reason":    ch.Reason,
		})
		return
	}
	var throttled *cloudsync.ThrottledError
	if errors.As(err, &throttled) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":       fmt.Sprintf("Wait %d seconds before asking for another code.", throttled.RetryAfter),
			"code":        "resend_throttled",
			"retry_after": throttled.RetryAfter,
		})
		return
	}
	switch {
	case errors.Is(err, cloudsync.ErrInvalidCode):
		httpErrorCode(w, http.StatusForbidden, "invalid_code", "That code isn't right. Check it and try again.")
	case errors.Is(err, cloudsync.ErrTOTPAlreadyEnabled):
		httpErrorCode(w, http.StatusConflict, "totp_already_enabled", "Two-factor authentication is already on.")
	case errors.Is(err, cloudsync.ErrTOTPNotEnabled):
		httpErrorCode(w, http.StatusConflict, "totp_not_enabled", "Two-factor authentication is already off.")
	case errors.Is(err, cloudsync.ErrTOTPNotSetUp):
		httpErrorCode(w, http.StatusConflict, "totp_not_set_up", "Start two-factor setup again.")
	case errors.Is(err, cloudsync.ErrAlreadyVerified):
		httpErrorCode(w, http.StatusConflict, "already_verified", "Your email is already verified. Log in.")
	case errors.Is(err, cloudsync.ErrEmailUnavailable):
		httpErrorCode(w, http.StatusServiceUnavailable, "email_unavailable", "CatchM Cloud couldn't send the email. Try again later.")
	case errors.Is(err, cloudsync.ErrUnauthorized):
		httpErrorCode(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
	case errors.Is(err, cloudsync.ErrConflict):
		httpErrorCode(w, http.StatusConflict, "email_taken", "email already registered")
	case errors.Is(err, cloudsync.ErrRateLimited):
		httpErrorCode(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts — try again in a minute.")
	case errors.Is(err, cloudsync.ErrQuotaExceeded):
		httpErrorCode(w, http.StatusInsufficientStorage, "cloud_storage_full", "Cloud storage is full.")
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
}

// normalizeCode trims a typed one-time code and drops the spaces or
// dashes people paste in from an email or an authenticator ("123 456").
func normalizeCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(code))
}

func challengeMessage(ch *cloudsync.ChallengeError) string {
	switch {
	case ch.Reason == "invalid_code":
		return "That code isn't right. Check it and try again."
	case ch.Reason == "code_expired":
		return "That code has expired. Send a new one."
	case ch.Challenge == "totp":
		return "Enter the 6-digit code from your authenticator app."
	default:
		return "We sent a 6-digit code to your email. Enter it to continue."
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
