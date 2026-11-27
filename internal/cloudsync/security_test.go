package cloudsync

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A hardened /recover takes the device fields and answers the same
// email_code challenge as /login.
func TestRecoverWithDeviceAndEmailCode(t *testing.T) {
	var last map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = nil
		_ = json.NewDecoder(r.Body).Decode(&last)
		switch last["email_code"] {
		case nil:
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"verification_required","challenge":"email_code"}`)
		case "000000":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"invalid_code","challenge":"email_code"}`)
		default:
			_, _ = io.WriteString(w, `{"session_token":"tok","session_id":"sid","expires_at":1,"recovery_blob":"blob","recovery_kdf_params":"{}"}`)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	req := RecoverRequest{Email: "a@b.c", RecoveryAuthHash: "rh", Device: Device{ID: "dev1", Name: "Mac"}}

	_, err := c.Recover(req)
	var ch *ChallengeError
	if !errors.As(err, &ch) || ch.Challenge != "email_code" || ch.Reason != "" {
		t.Fatalf("first recover = %v", err)
	}
	if last["device_id"] != "dev1" || last["device_name"] != "Mac" || last["recovery_auth_hash"] != "rh" {
		t.Fatalf("recover body = %v", last)
	}
	if _, has := last["email_code"]; has {
		t.Fatalf("empty email_code sent: %v", last)
	}
	req.EmailCode = "000000"
	if _, err := c.Recover(req); !errors.As(err, &ch) || ch.Reason != "invalid_code" {
		t.Fatalf("wrong code = %v", err)
	}
	req.EmailCode = "123456"
	res, err := c.Recover(req)
	if err != nil || res.SessionToken != "tok" || res.SessionID != "sid" || res.RecoveryBlob != "blob" {
		t.Fatalf("recover = %+v %v", res, err)
	}
}

func TestSessionsAndTOTPEndpoints(t *testing.T) {
	type call struct{ method, path, auth string }
	var calls []call
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{r.Method, r.URL.Path, r.Header.Get("Authorization")})
		lastBody = nil
		_ = json.NewDecoder(r.Body).Decode(&lastBody)
		switch r.Method + " " + r.URL.Path {
		case "GET /sessions":
			_, _ = io.WriteString(w, `{"sessions":[{"id":"s1","device_name":"Mac","created_at":1,"last_seen_at":2,"current":true},{"id":"s2","device_name":"PC","created_at":3,"last_seen_at":4,"current":false}]}`)
		case "DELETE /sessions/s2":
			w.WriteHeader(http.StatusNoContent)
		case "DELETE /sessions/nope":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not found"}`)
		case "POST /sessions/revoke-others":
			_, _ = io.WriteString(w, `{"revoked":3}`)
		case "POST /totp/setup":
			_, _ = io.WriteString(w, `{"secret":"JBSWY3DPEHPK3PXP","otpauth_uri":"otpauth://totp/CatchM%3Aa%40b.c?secret=JBSWY3DPEHPK3PXP"}`)
		case "POST /totp/enable":
			switch lastBody["code"] {
			case "111111":
				_, _ = io.WriteString(w, `{"status":"ok"}`)
			case "222222":
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"error":"totp_not_set_up"}`)
			default:
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"error":"invalid_code"}`)
			}
		case "POST /totp/disable":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"totp_not_enabled"}`)
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL)

	sessions, err := c.ListSessions("tok")
	if err != nil || len(sessions) != 2 || !sessions[0].Current || sessions[1].DeviceName != "PC" || sessions[1].LastSeenAt != 4 {
		t.Fatalf("sessions = %+v %v", sessions, err)
	}
	if calls[0].auth != "Bearer tok" {
		t.Fatalf("no bearer: %+v", calls[0])
	}
	if err := c.RevokeSession("tok", "s2"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := c.RevokeSession("tok", "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown = %v", err)
	}
	if n, err := c.RevokeOtherSessions("tok"); err != nil || n != 3 {
		t.Fatalf("revoke others = %d %v", n, err)
	}

	setup, err := c.TOTPSetup("tok")
	if err != nil || setup.Secret != "JBSWY3DPEHPK3PXP" || setup.OTPAuthURI == "" {
		t.Fatalf("setup = %+v %v", setup, err)
	}
	if err := c.TOTPEnable("tok", "111111"); err != nil || lastBody["code"] != "111111" {
		t.Fatalf("enable = %v body %v", err, lastBody)
	}
	// A wrong code is a 403, typed — not ErrUnauthorized (an expired session).
	if err := c.TOTPEnable("tok", "999999"); !errors.Is(err, ErrInvalidCode) || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong code = %v", err)
	}
	if err := c.TOTPEnable("tok", "222222"); !errors.Is(err, ErrTOTPNotSetUp) || !errors.Is(err, ErrConflict) {
		t.Fatalf("not set up = %v", err)
	}
	if err := c.TOTPDisable("tok", "111111"); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("disable when off = %v", err)
	}
}

func TestTypedErrorBodies(t *testing.T) {
	status, body := 0, ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status == http.StatusTooManyRequests {
			w.Header().Set("Retry-After", "42")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)

	status, body = http.StatusTooManyRequests, `{"error":"resend_throttled","retry_after":42}`
	err := c.ResendCode("a@b.c", "h")
	var th *ThrottledError
	if !errors.As(err, &th) || th.RetryAfter != 42 || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("throttled = %v", err)
	}
	status, body = http.StatusTooManyRequests, `{"error":"too many requests"}`
	if err := c.ResendCode("a@b.c", "h"); errors.As(err, &th) || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("plain 429 = %v", err)
	}
	status, body = http.StatusConflict, `{"error":"already_verified"}`
	if err := c.ResendCode("a@b.c", "h"); !errors.Is(err, ErrAlreadyVerified) {
		t.Fatalf("already verified = %v", err)
	}
	status, body = http.StatusConflict, `{"error":"totp_already_enabled"}`
	if _, err := c.TOTPSetup("tok"); !errors.Is(err, ErrTOTPAlreadyEnabled) {
		t.Fatalf("already enabled = %v", err)
	}
	// An unrecognised 409 is still plain ErrConflict.
	status, body = http.StatusConflict, `{"error":"email already registered"}`
	if err := c.ResendCode("a@b.c", "h"); !errors.Is(err, ErrConflict) || errors.Is(err, ErrAlreadyVerified) {
		t.Fatalf("plain 409 = %v", err)
	}
	status, body = http.StatusServiceUnavailable, `{"error":"email_unavailable"}`
	if err := c.ResendCode("a@b.c", "h"); !errors.Is(err, ErrEmailUnavailable) {
		t.Fatalf("email unavailable = %v", err)
	}
	// A 403 without a code (and without a challenge) stays a status error.
	status, body = http.StatusForbidden, `{"error":"forbidden"}`
	if err := c.TOTPEnable("tok", "1"); err == nil || errors.Is(err, ErrInvalidCode) {
		t.Fatalf("plain 403 = %v", err)
	}
}
