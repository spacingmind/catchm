package server

import (
	"net/http"
	"testing"
)

const cloudTestBody = `{"email":"sec@example.com","password":"correct horse battery staple"}`

// cloudLoggedIn signs up on the fake, logs out, and logs back in, so the
// local session carries the login's session_id.
func cloudLoggedIn(t *testing.T) (*Server, *cloudFakeWorker) {
	t.Helper()
	s, fake := newCloudTestServer(t)
	if code, body := doCloud(t, s, http.MethodPost, "/api/cloud/signup", cloudTestBody); code != http.StatusOK {
		t.Fatalf("signup = %d %v", code, body)
	}
	doCloud(t, s, http.MethodPost, "/api/cloud/logout", "")
	if code, body := doCloud(t, s, http.MethodPost, "/api/cloud/login", cloudTestBody); code != http.StatusOK {
		t.Fatalf("login = %d %v", code, body)
	}
	return s, fake
}

func cloudStatusOf(t *testing.T, s *Server) map[string]any {
	t.Helper()
	_, body := doCloud(t, s, http.MethodGet, "/api/cloud/status", "")
	return body
}

// Signup on a hardened server: no session, then a login with the
// emailed code.
func TestCloudSignupThenEmailCodeLogin(t *testing.T) {
	s, fake := newCloudTestServer(t)
	fake.hardenedSignup = true
	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/signup", cloudTestBody)
	if code != http.StatusOK || body["verification_required"] != true || body["recovery_code"] == "" {
		t.Fatalf("signup = %d %v", code, body)
	}
	if cloudStatusOf(t, s)["logged_in"] != false {
		t.Fatal("logged in before the email was verified")
	}
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"sec@example.com","password":"correct horse battery staple","email_code":"000 000"}`)
	if code != http.StatusUnauthorized || body["code"] != "challenge" || body["challenge"] != "email_code" || body["reason"] != "invalid_code" {
		t.Fatalf("wrong code = %d %v", code, body)
	}
	// Spaces in a pasted code are dropped.
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"sec@example.com","password":"correct horse battery staple","email_code":" 123 456 "}`)
	if code != http.StatusOK {
		t.Fatalf("login with code = %d %v", code, body)
	}
	if st := cloudStatusOf(t, s); st["logged_in"] != true || st["totp_enabled"] != false {
		t.Fatalf("status = %v", st)
	}
}

func TestCloudResendCodeErrors(t *testing.T) {
	s, fake := newCloudTestServer(t)
	fake.hardenedSignup = true
	doCloud(t, s, http.MethodPost, "/api/cloud/signup", cloudTestBody)
	resend := `{"email":"sec@example.com","password":"correct horse battery staple","resend":true}`

	if code, body := doCloud(t, s, http.MethodPost, "/api/cloud/login", resend); code != http.StatusOK || body["resent"] != true {
		t.Fatalf("resend = %d %v", code, body)
	}
	fake.resendStatus = http.StatusTooManyRequests
	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/login", resend)
	if code != http.StatusTooManyRequests || body["code"] != "resend_throttled" || body["retry_after"] != float64(42) {
		t.Fatalf("throttled = %d %v", code, body)
	}
	fake.resendStatus = http.StatusConflict
	if code, body := doCloud(t, s, http.MethodPost, "/api/cloud/login", resend); code != http.StatusConflict || body["code"] != "already_verified" {
		t.Fatalf("already verified = %d %v", code, body)
	}
}

// A TOTP challenge at login means two-factor is on; a login without one
// means it's off.
func TestCloudLoginTOTPChallengeSetsStatus(t *testing.T) {
	s, fake := cloudLoggedIn(t)
	doCloud(t, s, http.MethodPost, "/api/cloud/logout", "")
	fake.totp["sec@example.com"] = true

	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/login", cloudTestBody)
	if code != http.StatusUnauthorized || body["challenge"] != "totp" || body["reason"] != "totp_required" {
		t.Fatalf("totp challenge = %d %v", code, body)
	}
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"sec@example.com","password":"correct horse battery staple","totp_code":"111111"}`)
	if code != http.StatusUnauthorized || body["challenge"] != "totp" || body["reason"] != "invalid_code" {
		t.Fatalf("wrong totp = %d %v", code, body)
	}
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/login",
		`{"email":"sec@example.com","password":"correct horse battery staple","totp_code":"654321"}`)
	if code != http.StatusOK {
		t.Fatalf("totp login = %d %v", code, body)
	}
	if st := cloudStatusOf(t, s); st["totp_enabled"] != true {
		t.Fatalf("status after a TOTP login = %v", st)
	}
}

func TestCloudRecoverEmailChallenge(t *testing.T) {
	s, fake := newCloudTestServer(t)
	_, signup := doCloud(t, s, http.MethodPost, "/api/cloud/signup", cloudTestBody)
	doCloud(t, s, http.MethodPost, "/api/cloud/logout", "")
	fake.unverified["sec@example.com"] = true
	recovery := signup["recovery_code"].(string)
	body := func(emailCode string) string {
		return `{"email":"sec@example.com","recovery_code":"` + recovery + `","new_password":"another fine password","email_code":"` + emailCode + `"}`
	}

	code, out := doCloud(t, s, http.MethodPost, "/api/cloud/recover", body(""))
	if code != http.StatusUnauthorized || out["code"] != "challenge" || out["challenge"] != "email_code" {
		t.Fatalf("recover challenge = %d %v", code, out)
	}
	if id, _ := fake.lastRecover["device_id"].(string); id == "" || fake.lastRecover["device_name"] == "" {
		t.Fatalf("recover sent no device: %v", fake.lastRecover)
	}
	if fake.resetCalls != 0 {
		t.Fatal("password reset before the challenge was answered")
	}
	code, out = doCloud(t, s, http.MethodPost, "/api/cloud/recover", body("999999"))
	if code != http.StatusUnauthorized || out["reason"] != "invalid_code" {
		t.Fatalf("recover wrong code = %d %v", code, out)
	}
	code, out = doCloud(t, s, http.MethodPost, "/api/cloud/recover", body("123456"))
	if code != http.StatusOK || out["recovery_code"] == "" || out["recovery_code"] == recovery {
		t.Fatalf("recover = %d %v", code, out)
	}
	if fake.resetCalls != 1 || cloudStatusOf(t, s)["logged_in"] != true {
		t.Fatalf("recover didn't finish: resets %d", fake.resetCalls)
	}
}

func TestCloudSessionsListAndRevoke(t *testing.T) {
	s, fake := cloudLoggedIn(t)
	// This device's session plus two other devices'.
	fake.sessions["other-tok"] = "sec@example.com"
	fake.sessions["third-tok"] = "sec@example.com"

	code, body := doCloud(t, s, http.MethodGet, "/api/cloud/sessions", "")
	sessions, _ := body["sessions"].([]any)
	if code != http.StatusOK || len(sessions) != 3 {
		t.Fatalf("sessions = %d %v", code, body)
	}
	current := 0
	for _, raw := range sessions {
		if raw.(map[string]any)["current"] == true {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("current sessions = %d in %v", current, sessions)
	}

	// Another device: this one stays signed in.
	code, body = doCloud(t, s, http.MethodDelete, "/api/cloud/sessions/"+fakeSessionID("other-tok"), "")
	if code != http.StatusOK || body["signed_out"] != false || cloudStatusOf(t, s)["logged_in"] != true {
		t.Fatalf("revoke other = %d %v", code, body)
	}
	code, body = doCloud(t, s, http.MethodDelete, "/api/cloud/sessions/"+fakeSessionID("other-tok"), "")
	if code != http.StatusNotFound || body["code"] != "session_not_found" {
		t.Fatalf("revoke gone = %d %v", code, body)
	}

	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/sessions/revoke-others", "")
	if code != http.StatusOK || body["revoked"] != float64(1) { // third-tok
		t.Fatalf("revoke others = %d %v", code, body)
	}

	// This device: a sign-out.
	code, body = doCloud(t, s, http.MethodDelete, "/api/cloud/sessions/"+fakeSessionID("tok-sec@example.com"), "")
	if code != http.StatusOK || body["signed_out"] != true {
		t.Fatalf("revoke current = %d %v", code, body)
	}
	if cloudStatusOf(t, s)["logged_in"] != false {
		t.Fatal("still logged in after revoking this device's session")
	}
}

// With no session_id from the login (signup on an older server), the
// current session is found through the list.
func TestCloudRevokeCurrentWithoutSessionID(t *testing.T) {
	s, _ := newCloudTestServer(t)
	doCloud(t, s, http.MethodPost, "/api/cloud/signup", cloudTestBody)
	code, body := doCloud(t, s, http.MethodDelete, "/api/cloud/sessions/"+fakeSessionID("tok-sec@example.com"), "")
	if code != http.StatusOK || body["signed_out"] != true || cloudStatusOf(t, s)["logged_in"] != false {
		t.Fatalf("revoke current = %d %v", code, body)
	}
}

// A session ended elsewhere signs this device out on its next call.
func TestCloudSessionEndedClearsLogin(t *testing.T) {
	s, fake := cloudLoggedIn(t)
	for token := range fake.sessions {
		delete(fake.sessions, token)
	}
	code, body := doCloud(t, s, http.MethodGet, "/api/cloud/sessions", "")
	if code != http.StatusUnauthorized || body["code"] != "session_ended" {
		t.Fatalf("ended session = %d %v", code, body)
	}
	if cloudStatusOf(t, s)["logged_in"] != false {
		t.Fatal("still logged in after the session ended")
	}
}

func TestCloudSecurityNeedsLogin(t *testing.T) {
	s, _ := newCloudTestServer(t)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/cloud/sessions", ""},
		{http.MethodDelete, "/api/cloud/sessions/x", ""},
		{http.MethodPost, "/api/cloud/sessions/revoke-others", ""},
		{http.MethodPost, "/api/cloud/totp/setup", ""},
		{http.MethodPost, "/api/cloud/totp/enable", `{"code":"1"}`},
		{http.MethodPost, "/api/cloud/totp/disable", `{"code":"1"}`},
	} {
		if code, body := doCloud(t, s, c.method, c.path, c.body); code != http.StatusUnauthorized || body["code"] != "not_logged_in" {
			t.Fatalf("%s %s = %d %v", c.method, c.path, code, body)
		}
	}
}

func TestCloudTOTPSetupEnableDisable(t *testing.T) {
	s, fake := cloudLoggedIn(t)

	code, body := doCloud(t, s, http.MethodPost, "/api/cloud/totp/enable", `{"code":"654321"}`)
	if code != http.StatusConflict || body["code"] != "totp_not_set_up" {
		t.Fatalf("enable before setup = %d %v", code, body)
	}
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/totp/setup", "")
	if code != http.StatusOK || body["secret"] != "JBSWY3DPEHPK3PXP" || body["otpauth_uri"] == "" {
		t.Fatalf("setup = %d %v", code, body)
	}
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/totp/enable", `{"code":"111111"}`)
	if code != http.StatusForbidden || body["code"] != "invalid_code" {
		t.Fatalf("wrong code = %d %v", code, body)
	}
	if cloudStatusOf(t, s)["logged_in"] != true {
		t.Fatal("a wrong code signed this device out")
	}
	if code, body := doCloud(t, s, http.MethodPost, "/api/cloud/totp/enable", `{"code":""}`); code != http.StatusBadRequest {
		t.Fatalf("empty code = %d %v", code, body)
	}
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/totp/enable", `{"code":"654 321"}`)
	if code != http.StatusOK || body["totp_enabled"] != true || cloudStatusOf(t, s)["totp_enabled"] != true {
		t.Fatalf("enable = %d %v", code, body)
	}
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/totp/disable", `{"code":"654321"}`)
	if code != http.StatusOK || cloudStatusOf(t, s)["totp_enabled"] != false {
		t.Fatalf("disable = %d %v", code, body)
	}

	// The server's "already on / off" answers correct this device.
	fake.totp["sec@example.com"] = true
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/totp/setup", "")
	if code != http.StatusConflict || body["code"] != "totp_already_enabled" || cloudStatusOf(t, s)["totp_enabled"] != true {
		t.Fatalf("setup while on = %d %v", code, body)
	}
	delete(fake.totp, "sec@example.com")
	code, body = doCloud(t, s, http.MethodPost, "/api/cloud/totp/disable", `{"code":"654321"}`)
	if code != http.StatusConflict || body["code"] != "totp_not_enabled" || cloudStatusOf(t, s)["totp_enabled"] != false {
		t.Fatalf("disable while off = %d %v", code, body)
	}
}

// Logging out ends the session on the server too, so it drops off the
// signed-in devices list instead of lingering until it expires.
func TestCloudLogoutRevokesServerSession(t *testing.T) {
	s, fake := cloudLoggedIn(t)
	before := len(fake.sessions)
	if before == 0 {
		t.Fatal("fake has no session to revoke")
	}
	if code, _ := doCloud(t, s, http.MethodPost, "/api/cloud/logout", ""); code != http.StatusOK {
		t.Fatalf("logout = %d", code)
	}
	if len(fake.sessions) != before-1 {
		t.Fatalf("server sessions %d → %d, want one fewer", before, len(fake.sessions))
	}
	if cloudStatusOf(t, s)["logged_in"] != false {
		t.Fatal("still logged in locally")
	}
}
