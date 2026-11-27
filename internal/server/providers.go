package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/spacingmind/catchm/internal/translate"
)

// verifyTimeout bounds a key check: one small request to the provider.
const verifyTimeout = 15 * time.Second

// verifyProvider checks a key against a provider's real API with its
// cheapest authenticated call (docs/plans/active/cloud-mode-providers.md).
// Tests swap it for a fake.
var verifyProvider = func(ctx context.Context, name, apiKey string, s *Server) error {
	switch name {
	case "google":
		return translate.VerifyGoogle(ctx, nil, "", apiKey)
	default: // "llm"
		s.mu.Lock()
		base, fast, _ := s.cfg.LLM.Resolved()
		s.mu.Unlock()
		return translate.VerifyChat(ctx, nil, base, apiKey, fast)
	}
}

// handleVerifyProvider: POST /api/providers/{name}/verify with an
// optional {"api_key": "..."} — the key the user just typed, checked
// before saving; omitted means the stored one. 200 {"ok": true} or
// {"ok": false, "error": "<the provider's own message>"}; 400 for an
// unknown provider or nothing to check.
func (s *Server) handleVerifyProvider(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req struct {
		APIKey string `json:"api_key"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "bad json")
			return
		}
	}
	s.mu.Lock()
	cfg := *s.cfg
	s.mu.Unlock()
	key := req.APIKey
	switch name {
	case "google":
		if key == "" {
			key = cfg.Translate.Google.APIKey
		}
	case "llm":
		if key == "" {
			key = cfg.LLM.APIKey
		}
		if base, fast, _ := cfg.LLM.Resolved(); base == "" || fast == "" {
			httpError(w, http.StatusBadRequest, "choose a provider (and model) first")
			return
		}
	default:
		httpError(w, http.StatusBadRequest, `provider must be "google" or "llm"`)
		return
	}
	if key == "" {
		httpError(w, http.StatusBadRequest, "no API key to check")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), verifyTimeout)
	defer cancel()
	if err := verifyProvider(ctx, name, key, s); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
