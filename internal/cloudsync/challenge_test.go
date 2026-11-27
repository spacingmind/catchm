package cloudsync

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginChallenges(t *testing.T) {
	var last map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&last)
		switch {
		case r.URL.Path == "/signup":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"verification_required":true}`)
		case r.URL.Path == "/verify/resend":
			_, _ = io.WriteString(w, `{"ok":true}`)
		case last["email_code"] == nil && last["totp_code"] == nil:
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"verification_required","challenge":"email_code"}`)
		case last["email_code"] == "000000":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"invalid_code","challenge":"email_code"}`)
		case last["email_code"] == "123456" && last["totp_code"] == nil:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"challenge":"totp"}`)
		default:
			_, _ = io.WriteString(w, `{"session_token":"tok","wrapped_master_key":"wmk","key_wrap_params":"{}"}`)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	dev := Device{ID: "dev1", Name: "Mac"}

	tok, _, err := c.Signup(SignupRequest{Device: dev, Email: "a@b.c"})
	if err != nil || tok != "" || last["device_id"] != "dev1" || last["device_name"] != "Mac" {
		t.Fatalf("signup 202: tok %q err %v body %v", tok, err, last)
	}

	_, _, _, err = c.LoginWith(LoginRequest{Email: "a@b.c", AuthHash: "h", Device: dev})
	var ch *ChallengeError
	if !errors.As(err, &ch) || ch.Challenge != "email_code" || ch.Reason != "" {
		t.Fatalf("first login = %v", err)
	}
	_, _, _, err = c.LoginWith(LoginRequest{Email: "a@b.c", AuthHash: "h", Device: dev, EmailCode: "000000"})
	if !errors.As(err, &ch) || ch.Reason != "invalid_code" {
		t.Fatalf("wrong code = %v", err)
	}
	_, _, _, err = c.LoginWith(LoginRequest{Email: "a@b.c", AuthHash: "h", Device: dev, EmailCode: "123456"})
	if !errors.As(err, &ch) || ch.Challenge != "totp" {
		t.Fatalf("totp challenge = %v", err)
	}
	tok, wmk, _, err := c.LoginWith(LoginRequest{Email: "a@b.c", AuthHash: "h", Device: dev, EmailCode: "123456", TOTPCode: "654321"})
	if err != nil || tok != "tok" || wmk != "wmk" {
		t.Fatalf("final login: %q %q %v", tok, wmk, err)
	}
	if err := c.ResendCode("a@b.c", "h"); err != nil {
		t.Fatalf("resend: %v", err)
	}
	// A plain 401 without a challenge is still ErrUnauthorized.
	if challengeFrom([]byte(`{"error":"invalid credentials"}`)) != nil {
		t.Fatal("plain 401 parsed as a challenge")
	}
}
