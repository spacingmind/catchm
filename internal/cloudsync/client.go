// Package cloudsync is the Go client for the catchm-api Worker:
// the HTTP endpoints (this file), meeting-bundle encryption, and sync
// orchestration. See docs/plans/active/m13c-cloud-sync-client.md.
package cloudsync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Sentinel errors returned by Client methods; test with errors.Is.
var (
	// ErrUnauthorized is a 401: the session token is missing, malformed,
	// or expired.
	ErrUnauthorized = errors.New("cloudsync: unauthorized")

	// ErrWrongPassword: change-password's current password didn't match
	// (the session itself is fine).
	ErrWrongPassword = errors.New("cloudsync: wrong current password")

	// ErrNotFound is a 404: the blob does not exist, or exists but
	// belongs to a different account — the server does not distinguish.
	ErrNotFound = errors.New("cloudsync: not found")

	// ErrConflict is a 409 on a JSON endpoint (e.g. signup with an
	// already-registered email). PutBlob's 409 is not an error and is
	// returned as its conflict value instead.
	ErrConflict = errors.New("cloudsync: conflict")

	// ErrRateLimited is a 429: the Worker's rate limiters (login/
	// recover/signup) tripped. The Retry-After seconds, when the server
	// sends one, are appended to the message.
	ErrRateLimited = errors.New("cloudsync: rate limited")

	// ErrQuotaExceeded is a 507: the account's storage quota is full
	// (server carries used/quota in the body).
	ErrQuotaExceeded = errors.New("cloudsync: storage quota exceeded")

	// ErrBlobTooLarge is a 413: the blob exceeds the server's
	// per-blob size cap (MAX_BLOB_BYTES).
	ErrBlobTooLarge = errors.New("cloudsync: blob too large")

	// ErrInvalidCode is a 403 {error:"invalid_code"} from /totp/enable or
	// /totp/disable: the authenticator code was wrong (a 403, not a 401,
	// so it isn't mistaken for an expired session).
	ErrInvalidCode = errors.New("cloudsync: invalid code")

	// The 409s below each also match ErrConflict.

	// ErrTOTPAlreadyEnabled: /totp/setup or /totp/enable while two-factor
	// is already on.
	ErrTOTPAlreadyEnabled = errors.New("cloudsync: two-factor already enabled")
	// ErrTOTPNotEnabled: /totp/disable while two-factor is off.
	ErrTOTPNotEnabled = errors.New("cloudsync: two-factor not enabled")
	// ErrTOTPNotSetUp: /totp/enable without a /totp/setup first.
	ErrTOTPNotSetUp = errors.New("cloudsync: two-factor not set up")
	// ErrAlreadyVerified: /verify/resend for an email that is already
	// verified.
	ErrAlreadyVerified = errors.New("cloudsync: email already verified")

	// ErrEmailUnavailable is a 503 {error:"email_unavailable"}: the
	// server couldn't send the email a step needs (verification code).
	ErrEmailUnavailable = errors.New("cloudsync: email unavailable")
)

// conflictCodes maps a 409 body's error code to its sentinel.
var conflictCodes = map[string]error{
	"totp_already_enabled": ErrTOTPAlreadyEnabled,
	"totp_not_enabled":     ErrTOTPNotEnabled,
	"totp_not_set_up":      ErrTOTPNotSetUp,
	"already_verified":     ErrAlreadyVerified,
}

// ThrottledError is a 429 {error:"resend_throttled", retry_after}: a new
// email code was asked for too soon. It also matches ErrRateLimited.
type ThrottledError struct {
	RetryAfter int // seconds
}

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("cloudsync: resend throttled, retry after %ds", e.RetryAfter)
}

func (e *ThrottledError) Unwrap() error { return ErrRateLimited }

// errorCode is the {"error": ...} of a JSON error body, or "".
func errorCode(raw []byte) string {
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	return body.Error
}

// ChallengeError: the credentials were right, but the server wants one
// more proof before it issues a session — "email_code" (the account's
// email isn't verified yet; a code was just sent) or "totp" (two-factor
// is on). Reason is the server's error code, e.g. "invalid_code" when a
// code was sent and was wrong. docs/plans/active/cloud-auth-hardening.md.
type ChallengeError struct {
	Challenge string
	Reason    string
}

func (e *ChallengeError) Error() string {
	if e.Reason != "" {
		return "cloudsync: " + e.Challenge + " required (" + e.Reason + ")"
	}
	return "cloudsync: " + e.Challenge + " required"
}

// challengeFrom reads a 401/403 body for a challenge.
func challengeFrom(raw []byte) *ChallengeError {
	var body struct {
		Error     string `json:"error"`
		Challenge string `json:"challenge"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Challenge == "" {
		return nil
	}
	reason := body.Error
	if reason == "verification_required" {
		reason = ""
	}
	return &ChallengeError{Challenge: body.Challenge, Reason: reason}
}

// Device identifies this installation to the server, for the
// new-device alert and the signed-in devices list.
type Device struct {
	ID   string `json:"device_id"`
	Name string `json:"device_name"`
}

// BlobMeta is one entry of GET /blobs, and the body of PUT /blobs/:id's
// 409 conflict response.
type BlobMeta struct {
	ID        string `json:"id"`
	Version   int    `json:"version"`
	Checksum  string `json:"checksum"`
	SizeBytes int64  `json:"size_bytes"`
	UpdatedAt int64  `json:"updated_at"`
}

// ResetPasswordRequest is the POST /account/reset-password body: the
// new password-path fields and the rotated recovery-path fields,
// overwritten server-side in one transaction.
type ResetPasswordRequest struct {
	AuthHash          string `json:"auth_hash"`
	AuthSalt          string `json:"auth_salt"`
	KDFParams         string `json:"kdf_params"`
	WrappedMasterKey  string `json:"wrapped_master_key"`
	RecoverySalt      string `json:"recovery_salt"`
	RecoveryAuthHash  string `json:"recovery_auth_hash"`
	RecoveryBlob      string `json:"recovery_blob"`
	RecoveryKDFParams string `json:"recovery_kdf_params"`
}

// SignupRequest is the POST /signup body. All crypto fields are opaque
// strings to the server; the caller encodes bytes (base64) before
// building this struct.
type SignupRequest struct {
	Device
	Email             string `json:"email"`
	AuthHash          string `json:"auth_hash"`
	AuthSalt          string `json:"auth_salt"`
	KDFParams         string `json:"kdf_params"`
	WrappedMasterKey  string `json:"wrapped_master_key"`
	KeyWrapParams     string `json:"key_wrap_params"`
	RecoverySalt      string `json:"recovery_salt"`
	RecoveryAuthHash  string `json:"recovery_auth_hash"`
	RecoveryBlob      string `json:"recovery_blob"`
	RecoveryKDFParams string `json:"recovery_kdf_params"`
}

// Client talks to a catchm-api Worker. It holds no session state:
// every method takes the session token explicitly, so one Client can
// be shared across sessions.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a Client for the Worker at baseURL.
func NewClient(baseURL string) *Client {
	return &Client{baseURL: strings.TrimSuffix(baseURL, "/"), http: http.DefaultClient}
}

// BaseURL returns the (normalized, trailing-slash-trimmed) cloud base
// URL this client talks to — Sync keys its per-server manifest by it.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// AuthParams fetches the auth_salt and kdf_params a login needs. The
// server returns plausible fake values for unknown emails
// (anti-enumeration); the result is opaque and a login with the wrong
// derivation fails on its own.
func (c *Client) AuthParams(email string) (salt, kdfParams string, err error) {
	var out struct {
		AuthSalt  string `json:"auth_salt"`
		KDFParams string `json:"kdf_params"`
	}
	if err := c.doJSON(http.MethodGet, "/auth-params?email="+url.QueryEscape(email), "", nil, &out); err != nil {
		return "", "", err
	}
	return out.AuthSalt, out.KDFParams, nil
}

// Signup creates an account. A hardened server (202) creates it
// unverified and emails a code — no session; the caller logs in with the
// code next. An older server returns a session at once (sessionToken
// set). The server answers a duplicate email exactly like a new one.
func (c *Client) Signup(req SignupRequest) (sessionToken string, expiresAt int64, err error) {
	var out struct {
		SessionToken string `json:"session_token"`
		ExpiresAt    int64  `json:"expires_at"`
	}
	if err := c.doJSON(http.MethodPost, "/signup", "", req, &out); err != nil {
		return "", 0, err
	}
	return out.SessionToken, out.ExpiresAt, nil
}

// LoginRequest is the POST /login body. EmailCode / TOTPCode answer a
// ChallengeError from a previous attempt.
type LoginRequest struct {
	Email    string `json:"email"`
	AuthHash string `json:"auth_hash"`
	Device
	EmailCode string `json:"email_code,omitempty"`
	TOTPCode  string `json:"totp_code,omitempty"`
}

// LoginResult is a successful POST /login. SessionID is the public id
// of the new session (GET /sessions); older servers don't send it.
type LoginResult struct {
	SessionToken     string `json:"session_token"`
	SessionID        string `json:"session_id"`
	WrappedMasterKey string `json:"wrapped_master_key"`
	KeyWrapParams    string `json:"key_wrap_params"`
}

// LoginWith authenticates and returns a fresh session plus the stored
// wrapped master key and its params. A *ChallengeError means the
// password was right but a code is needed.
func (c *Client) LoginWith(req LoginRequest) (LoginResult, error) {
	var out LoginResult
	if err := c.doJSON(http.MethodPost, "/login", "", req, &out); err != nil {
		return LoginResult{}, err
	}
	return out, nil
}

// ResendCode asks for a new email code (the credentials must be right).
// The server allows one a minute: sooner is a *ThrottledError with the
// wait; an already-verified email is ErrAlreadyVerified.
func (c *Client) ResendCode(email, authHash string) error {
	body := struct {
		Email    string `json:"email"`
		AuthHash string `json:"auth_hash"`
	}{email, authHash}
	return c.doJSON(http.MethodPost, "/verify/resend", "", body, nil)
}

// Login authenticates with the client-derived auth_hash and returns a
// fresh session token plus the stored wrapped master key and its
// params, which the caller unwraps locally.
func (c *Client) Login(email, authHash string) (sessionToken string, wrappedMasterKey, keyWrapParams string, err error) {
	var out struct {
		SessionToken     string `json:"session_token"`
		WrappedMasterKey string `json:"wrapped_master_key"`
		KeyWrapParams    string `json:"key_wrap_params"`
	}
	body := struct {
		Email    string `json:"email"`
		AuthHash string `json:"auth_hash"`
	}{email, authHash}
	if err := c.doJSON(http.MethodPost, "/login", "", body, &out); err != nil {
		return "", "", "", err
	}
	return out.SessionToken, out.WrappedMasterKey, out.KeyWrapParams, nil
}

// RecoveryParams fetches the recovery salt and kdf params a recover
// attempt needs. Like AuthParams, the server returns plausible fake
// values for unknown emails (anti-enumeration) — a wrong derivation
// fails the /recover call on its own.
func (c *Client) RecoveryParams(email string) (salt, kdfParams string, err error) {
	var out struct {
		RecoverySalt      string `json:"recovery_salt"`
		RecoveryKDFParams string `json:"recovery_kdf_params"`
	}
	if err := c.doJSON(http.MethodGet, "/recovery-params?email="+url.QueryEscape(email), "", nil, &out); err != nil {
		return "", "", err
	}
	return out.RecoverySalt, out.RecoveryKDFParams, nil
}

// RecoverRequest is the POST /recover body. EmailCode answers an
// email_code ChallengeError (an unverified account); there is no TOTP
// step, since the recovery code is itself the second secret. Servers
// from before auth hardening ignore the device fields and the code.
type RecoverRequest struct {
	Email            string `json:"email"`
	RecoveryAuthHash string `json:"recovery_auth_hash"`
	Device
	EmailCode string `json:"email_code,omitempty"`
}

// RecoverResult is a successful POST /recover.
type RecoverResult struct {
	SessionToken      string `json:"session_token"`
	SessionID         string `json:"session_id"`
	RecoveryBlob      string `json:"recovery_blob"`
	RecoveryKDFParams string `json:"recovery_kdf_params"`
	// TwoFactorDisabled: the account had 2FA on, and recovering turned
	// it off (the Worker emails the owner why).
	TwoFactorDisabled bool `json:"two_factor_disabled"`
}

// Recover authenticates with the client-derived recovery_auth_hash
// and returns a fresh session plus the stored recovery blob and its
// kdf params, which the caller unwraps locally. A *ChallengeError means
// the code was right but the email needs verifying first.
func (c *Client) Recover(req RecoverRequest) (RecoverResult, error) {
	var out RecoverResult
	if err := c.doJSON(http.MethodPost, "/recover", "", req, &out); err != nil {
		return RecoverResult{}, err
	}
	return out, nil
}

// Session is one entry of GET /sessions. Times are Unix milliseconds;
// Current marks the session the request was made with.
type Session struct {
	ID         string `json:"id"`
	DeviceName string `json:"device_name"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt int64  `json:"last_seen_at"`
	Current    bool   `json:"current"`
}

// ListSessions lists the account's signed-in sessions.
func (c *Client) ListSessions(sessionToken string) ([]Session, error) {
	var out struct {
		Sessions []Session `json:"sessions"`
	}
	if err := c.doJSON(http.MethodGet, "/sessions", sessionToken, nil, &out); err != nil {
		return nil, err
	}
	if out.Sessions == nil {
		out.Sessions = []Session{}
	}
	return out.Sessions, nil
}

// RevokeSession signs one session out. ErrNotFound when it isn't one of
// the account's sessions (or is already gone). Revoking the calling
// session signs this device out.
func (c *Client) RevokeSession(sessionToken, id string) error {
	return c.doJSON(http.MethodDelete, "/sessions/"+url.PathEscape(id), sessionToken, nil, nil)
}

// RevokeOtherSessions signs out every session but the calling one and
// returns how many it ended.
func (c *Client) RevokeOtherSessions(sessionToken string) (int, error) {
	var out struct {
		Revoked int `json:"revoked"`
	}
	if err := c.doJSON(http.MethodPost, "/sessions/revoke-others", sessionToken, nil, &out); err != nil {
		return 0, err
	}
	return out.Revoked, nil
}

// TOTPSetup is a fresh, not-yet-enabled authenticator secret.
type TOTPSetup struct {
	Secret     string `json:"secret"`      // base32
	OTPAuthURI string `json:"otpauth_uri"` // otpauth://totp/CatchM%3A<email>?…
}

// TOTPSetup starts two-factor setup (replacing any unconfirmed secret).
// ErrTOTPAlreadyEnabled when it's already on.
func (c *Client) TOTPSetup(sessionToken string) (TOTPSetup, error) {
	var out TOTPSetup
	if err := c.doJSON(http.MethodPost, "/totp/setup", sessionToken, struct{}{}, &out); err != nil {
		return TOTPSetup{}, err
	}
	return out, nil
}

// TOTPEnable turns two-factor on with a code from the authenticator.
// ErrInvalidCode for a wrong code; ErrTOTPNotSetUp / ErrTOTPAlreadyEnabled.
func (c *Client) TOTPEnable(sessionToken, code string) error {
	return c.doJSON(http.MethodPost, "/totp/enable", sessionToken, map[string]string{"code": code}, nil)
}

// TOTPDisable turns two-factor off; it needs a current code.
// ErrInvalidCode for a wrong code; ErrTOTPNotEnabled when it's off.
func (c *Client) TOTPDisable(sessionToken, code string) error {
	return c.doJSON(http.MethodPost, "/totp/disable", sessionToken, map[string]string{"code": code}, nil)
}

// ChangePasswordRequest is the POST /account/change-password body: the
// current password's auth hash (proof beyond the session) and the
// password half re-wrapped under the new password.
type ChangePasswordRequest struct {
	CurrentAuthHash  string `json:"current_auth_hash"`
	AuthHash         string `json:"auth_hash"`
	AuthSalt         string `json:"auth_salt"`
	KDFParams        string `json:"kdf_params"`
	WrappedMasterKey string `json:"wrapped_master_key"`
	SignOutOthers    bool   `json:"sign_out_others"`
}

// ChangePassword sets a new password for a signed-in user who knows the
// current one; the recovery code is unchanged. It returns how many other
// sessions were signed out. ErrWrongPassword when the current password
// is wrong; ErrUnauthorized when the session ended; ErrNotFound on a
// server without the endpoint.
func (c *Client) ChangePassword(sessionToken string, req ChangePasswordRequest) (int, error) {
	var out struct {
		SignedOut int `json:"signed_out"`
	}
	err := c.doJSON(http.MethodPost, "/account/change-password", sessionToken, req, &out)
	if errors.Is(err, ErrUnauthorized) && strings.Contains(err.Error(), "invalid credentials") {
		return 0, ErrWrongPassword
	}
	return out.SignedOut, err
}

// ResetPassword overwrites the account's password-path fields and
// rotates the recovery-path fields server-side, atomically.
func (c *Client) ResetPassword(sessionToken string, fields ResetPasswordRequest) error {
	return c.doJSON(http.MethodPost, "/account/reset-password", sessionToken, fields, nil)
}

// PutBlob uploads ciphertext with optimistic concurrency. On a 409 it
// returns the server's current metadata as conflict with a nil error —
// for every 409 shape, including "expected a version but no such blob"
// (conflict.Version 0) — so all conflicts take one code path in the
// caller.
func (c *Client) PutBlob(sessionToken, id string, ciphertext []byte, expectedVersion int) (newVersion int, conflict *BlobMeta, err error) {
	sum := sha256.Sum256(ciphertext)
	req, err := http.NewRequest(http.MethodPut, c.baseURL+"/blobs/"+url.PathEscape(id), bytes.NewReader(ciphertext))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Checksum", hex.EncodeToString(sum[:]))
	req.Header.Set("X-Expected-Version", strconv.Itoa(expectedVersion))
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var out struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return 0, nil, fmt.Errorf("cloudsync: malformed PUT response: %w", err)
		}
		return out.Version, nil, nil
	case http.StatusConflict:
		var meta BlobMeta
		if err := json.Unmarshal(raw, &meta); err != nil {
			return 0, nil, fmt.Errorf("cloudsync: malformed 409 body: %w", err)
		}
		return 0, &meta, nil
	default:
		return 0, nil, mapStatusError(resp, raw)
	}
}

// GetBlob downloads a blob's ciphertext and its current version.
func (c *Client) GetBlob(sessionToken, id string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/blobs/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+sessionToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		version, err := strconv.Atoi(resp.Header.Get("X-Version"))
		if err != nil {
			return nil, 0, fmt.Errorf("cloudsync: malformed X-Version header: %w", err)
		}
		return raw, version, nil
	case http.StatusUnauthorized:
		return nil, 0, fmt.Errorf("%w: %s", ErrUnauthorized, strings.TrimSpace(string(raw)))
	case http.StatusNotFound:
		return nil, 0, fmt.Errorf("%w: %s", ErrNotFound, strings.TrimSpace(string(raw)))
	default:
		return nil, 0, mapStatusError(resp, raw)
	}
}

// ListBlobs returns the authenticated account's blob metadata,
// following the server's next_cursor pagination
// (docs/plans/completed/m24-cloud-update-endpoint.md Part B.4) until it
// reports done. The response envelope is strict: a missing "blobs"
// key is an error (there is no bare-array fallback — the unpaginated
// shape was never deployed), and a cursor that fails to advance
// between pages is also an error rather than an infinite loop.
func (c *Client) ListBlobs(sessionToken string) ([]BlobMeta, error) {
	var all []BlobMeta
	cursor := ""
	for {
		path := "/blobs"
		if cursor != "" {
			path += "?cursor=" + url.QueryEscape(cursor)
		}
		raw, err := c.doRaw(http.MethodGet, path, sessionToken, nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Blobs      *[]BlobMeta      `json:"blobs"`
			NextCursor *json.RawMessage `json:"next_cursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("cloudsync: malformed list response: %w", err)
		}
		if page.Blobs == nil {
			return nil, fmt.Errorf("cloudsync: list response missing %q", "blobs")
		}
		all = append(all, *page.Blobs...)

		var next string
		if page.NextCursor != nil {
			if err := json.Unmarshal(*page.NextCursor, &next); err != nil {
				return nil, fmt.Errorf("cloudsync: malformed %q: %w", "next_cursor", err)
			}
		}
		if next == "" {
			return all, nil
		}
		if next == cursor {
			return nil, fmt.Errorf("cloudsync: list cursor did not advance (%q)", next)
		}
		cursor = next
	}
}

// DeleteBlob removes a blob and its stored bytes.
func (c *Client) DeleteBlob(sessionToken, id string) error {
	return c.doJSON(http.MethodDelete, "/blobs/"+url.PathEscape(id), sessionToken, nil, nil)
}

// doJSON performs a JSON request/response round trip. A nil out means
// no response body is expected (including a 204's empty body).
func (c *Client) doJSON(method, path, sessionToken string, body, out any) error {
	raw, err := c.doRaw(method, path, sessionToken, body)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// doRaw performs a request/response round trip and returns the raw
// response body on 200/204, mapping other statuses to sentinel
// errors. A nil body means no request body.
func (c *Client) doRaw(method, path, sessionToken string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if sessionToken != "" {
		req.Header.Set("Authorization", "Bearer "+sessionToken)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		return raw, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		if ch := challengeFrom(raw); ch != nil {
			return nil, ch
		}
		if resp.StatusCode == http.StatusForbidden {
			if errorCode(raw) == "invalid_code" {
				return nil, ErrInvalidCode
			}
			return nil, statusError(resp.StatusCode, raw)
		}
		return nil, fmt.Errorf("%w: %s", ErrUnauthorized, strings.TrimSpace(string(raw)))
	case http.StatusNotFound:
		return nil, fmt.Errorf("%w: %s", ErrNotFound, strings.TrimSpace(string(raw)))
	case http.StatusConflict:
		if specific, ok := conflictCodes[errorCode(raw)]; ok {
			return nil, fmt.Errorf("%w: %w", ErrConflict, specific)
		}
		return nil, fmt.Errorf("%w: %s", ErrConflict, strings.TrimSpace(string(raw)))
	default:
		return nil, mapStatusError(resp, raw)
	}
}

func statusError(status int, body []byte) error {
	return fmt.Errorf("cloudsync: unexpected status %d: %s", status, strings.TrimSpace(string(body)))
}

// mapStatusError maps the abuse-hardening statuses (429/507/413) and a
// 503 email_unavailable to their sentinels: a resend_throttled 429 is a
// *ThrottledError, other 429s get the Retry-After seconds appended.
// Everything else falls through to statusError.
func mapStatusError(resp *http.Response, body []byte) error {
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		var throttled struct {
			Error      string `json:"error"`
			RetryAfter int    `json:"retry_after"`
		}
		if json.Unmarshal(body, &throttled) == nil && throttled.Error == "resend_throttled" {
			return &ThrottledError{RetryAfter: max(throttled.RetryAfter, 1)}
		}
		if after := strings.TrimSpace(resp.Header.Get("Retry-After")); after != "" {
			return fmt.Errorf("%w: retry after %ss", ErrRateLimited, after)
		}
		return fmt.Errorf("%w: %s", ErrRateLimited, strings.TrimSpace(string(body)))
	case http.StatusServiceUnavailable:
		if errorCode(body) == "email_unavailable" {
			return ErrEmailUnavailable
		}
		return statusError(resp.StatusCode, body)
	case http.StatusInsufficientStorage:
		return fmt.Errorf("%w: %s", ErrQuotaExceeded, strings.TrimSpace(string(body)))
	case http.StatusRequestEntityTooLarge:
		return fmt.Errorf("%w: %s", ErrBlobTooLarge, strings.TrimSpace(string(body)))
	default:
		return statusError(resp.StatusCode, body)
	}
}
