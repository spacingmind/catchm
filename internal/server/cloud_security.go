package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/spacingmind/catchm/internal/cloudsync"
	"github.com/spacingmind/catchm/internal/e2ee"
)

// Settings → Cloud sync → Security: signed-in devices and two-factor
// authentication, all made with this device's session token.
// docs/plans/active/cloud-auth-hardening.md.

// liveCloudSession returns the current session, or answers 401
// not_logged_in and returns nil.
func (s *Server) liveCloudSession(w http.ResponseWriter) *cloudState {
	s.mu.Lock()
	st := s.cloud
	s.mu.Unlock()
	if st == nil {
		httpErrorCode(w, http.StatusUnauthorized, "not_logged_in", "not logged in")
	}
	return st
}

// endCloudSession drops st as the local session (if it still is one):
// the server ended it, so this device is signed out too.
func (s *Server) endCloudSession(st *cloudState) {
	s.mu.Lock()
	if s.cloud == st {
		s.cloud = nil
	}
	s.mu.Unlock()
}

// setTOTPEnabled records what this device now knows about two-factor.
func (s *Server) setTOTPEnabled(st *cloudState, on bool) {
	s.mu.Lock()
	st.totpEnabled = on
	s.mu.Unlock()
}

// respondSessionErr is respondCloudErr for calls made with the session
// token. A 401 means the session ended (signed out from another device,
// or expired), so the local login state is cleared and the UI is told
// to log in again. A 404 on these routes means a server from before
// auth hardening.
func (s *Server) respondSessionErr(w http.ResponseWriter, st *cloudState, err error) {
	switch {
	case errors.Is(err, cloudsync.ErrUnauthorized):
		s.endCloudSession(st)
		httpErrorCode(w, http.StatusUnauthorized, "session_ended", "Your CatchM Cloud session ended. Log in again.")
	case errors.Is(err, cloudsync.ErrNotFound):
		httpErrorCode(w, http.StatusNotImplemented, "cloud_unsupported", "This CatchM Cloud server doesn't support this yet.")
	default:
		respondCloudErr(w, err)
	}
}

func (s *Server) handleCloudSessions(w http.ResponseWriter, _ *http.Request) {
	st := s.liveCloudSession(w)
	if st == nil {
		return
	}
	sessions, err := s.cloudClient().ListSessions(st.sessionToken)
	if err != nil {
		s.respondSessionErr(w, st, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// handleCloudRevokeSession signs one session out. Revoking this
// device's own session is a sign-out: the local login state goes too
// (signed_out: true in the answer).
func (s *Server) handleCloudRevokeSession(w http.ResponseWriter, r *http.Request) {
	st := s.liveCloudSession(w)
	if st == nil {
		return
	}
	id := r.PathValue("id")
	client := s.cloudClient()
	current := st.sessionID != "" && id == st.sessionID
	if st.sessionID == "" {
		// No id from the login (shouldn't happen with a server that has
		// /sessions): ask which one is ours.
		if sessions, err := client.ListSessions(st.sessionToken); err == nil {
			for _, sess := range sessions {
				current = current || (sess.Current && sess.ID == id)
			}
		}
	}
	if err := client.RevokeSession(st.sessionToken, id); err != nil {
		if errors.Is(err, cloudsync.ErrNotFound) {
			httpErrorCode(w, http.StatusNotFound, "session_not_found", "That device is already signed out.")
			return
		}
		s.respondSessionErr(w, st, err)
		return
	}
	if current {
		s.endCloudSession(st)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "signed_out": current})
}

func (s *Server) handleCloudRevokeOtherSessions(w http.ResponseWriter, _ *http.Request) {
	st := s.liveCloudSession(w)
	if st == nil {
		return
	}
	n, err := s.cloudClient().RevokeOtherSessions(st.sessionToken)
	if err != nil {
		s.respondSessionErr(w, st, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revoked": n})
}

// handleCloudTOTPSetup starts two-factor setup: a new secret and its
// otpauth:// URI (the UI draws the QR code). Nothing is on until
// /enable confirms a code.
func (s *Server) handleCloudTOTPSetup(w http.ResponseWriter, _ *http.Request) {
	st := s.liveCloudSession(w)
	if st == nil {
		return
	}
	setup, err := s.cloudClient().TOTPSetup(st.sessionToken)
	if err != nil {
		if errors.Is(err, cloudsync.ErrTOTPAlreadyEnabled) {
			s.setTOTPEnabled(st, true)
		}
		s.respondSessionErr(w, st, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"secret": setup.Secret, "otpauth_uri": setup.OTPAuthURI})
}

func (s *Server) handleCloudTOTPEnable(w http.ResponseWriter, r *http.Request) {
	s.handleCloudTOTPChange(w, r, true)
}

func (s *Server) handleCloudTOTPDisable(w http.ResponseWriter, r *http.Request) {
	s.handleCloudTOTPChange(w, r, false)
}

// handleCloudTOTPChange turns two-factor on or off with a code from the
// authenticator app. The server's "already on/off" answers correct what
// this device believed.
func (s *Server) handleCloudTOTPChange(w http.ResponseWriter, r *http.Request, enable bool) {
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	code := normalizeCode(req.Code)
	if code == "" {
		httpError(w, http.StatusBadRequest, "code is required")
		return
	}
	st := s.liveCloudSession(w)
	if st == nil {
		return
	}
	client := s.cloudClient()
	var err error
	if enable {
		err = client.TOTPEnable(st.sessionToken, code)
	} else {
		err = client.TOTPDisable(st.sessionToken, code)
	}
	switch {
	case err == nil:
		s.setTOTPEnabled(st, enable)
		writeJSON(w, http.StatusOK, map[string]any{"totp_enabled": enable})
		return
	case errors.Is(err, cloudsync.ErrTOTPAlreadyEnabled):
		s.setTOTPEnabled(st, true)
	case errors.Is(err, cloudsync.ErrTOTPNotEnabled):
		s.setTOTPEnabled(st, false)
	}
	s.respondSessionErr(w, st, err)
}

// handleCloudChangePassword sets a new password for the signed-in user.
// The current password is checked by the Worker (its auth hash), the
// master key in memory is re-wrapped under the new one, and the
// recovery code is unchanged. sign_out_others ends the other sessions.
// A breached new password gets the same confirm-first warning as signup.
func (s *Server) handleCloudChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		SignOutOthers   bool   `json:"sign_out_others"`
		AcceptBreached  bool   `json:"accept_breached"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		httpError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}
	st := s.liveCloudSession(w)
	if st == nil {
		return
	}
	if req.NewPassword == req.CurrentPassword {
		httpErrorCode(w, http.StatusBadRequest, "same_password", "The new password is the same as the current one.")
		return
	}
	if warnIfBreached(w, r, req.NewPassword, req.AcceptBreached) {
		return
	}

	client := s.cloudClient()
	salt, kdfParams, err := client.AuthParams(st.email)
	if err != nil {
		s.respondSessionErr(w, st, err)
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
	current := e2ee.DeriveAuthHash(e2ee.DeriveAuthKey(req.CurrentPassword, authSalt, params))

	s.mu.Lock()
	masterKey := st.masterKey
	s.mu.Unlock()
	pw, err := e2ee.RewrapForPassword(masterKey, req.NewPassword)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	newSalt := base64.StdEncoding.EncodeToString(pw.AuthSalt)
	newParams := mustJSON(pw.KDFParams)
	signedOut, err := client.ChangePassword(st.sessionToken, cloudsync.ChangePasswordRequest{
		CurrentAuthHash:  base64.StdEncoding.EncodeToString(current),
		AuthHash:         base64.StdEncoding.EncodeToString(pw.AuthHash),
		AuthSalt:         newSalt,
		KDFParams:        newParams,
		WrappedMasterKey: base64.StdEncoding.EncodeToString(pw.WrappedMasterKey),
		SignOutOthers:    req.SignOutOthers,
	})
	if errors.Is(err, cloudsync.ErrWrongPassword) {
		httpErrorCode(w, http.StatusForbidden, "wrong_password", "That isn't your current password.")
		return
	}
	if err != nil {
		s.respondSessionErr(w, st, err)
		return
	}
	s.saveCloudConfig(st.email, newSalt, newParams)
	writeJSON(w, http.StatusOK, map[string]any{"signed_out": signedOut})
}
