package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/summarize"
	"github.com/spacingmind/catchm/internal/sysinfo"
	"github.com/spacingmind/catchm/internal/translate"
)

// summaryStatusTimeout bounds the Ollama and cloud-quota probes so the
// status check answers quickly when either is unreachable.
const summaryStatusTimeout = 3 * time.Second

// refineReadyTimeout bounds refine's readiness probe, so checking before
// the batch pass never adds noticeable delay.
const refineReadyTimeout = 2 * time.Second

// summaryStatus is GET /api/summary/status's body. Ready and Reason are
// for the active provider; every provider's block is included so the
// settings page can show each option's readiness.
type summaryStatus struct {
	Provider string              `json:"provider"`
	Ready    bool                `json:"ready"`
	Reason   string              `json:"reason,omitempty"`
	Ollama   *ollamaSummaryState `json:"ollama,omitempty"`
	Cloud    *cloudSummaryState  `json:"cloud,omitempty"`
	OpenAI   *openaiSummaryState `json:"openai,omitempty"`
	System   *sysinfo.Info       `json:"system,omitempty"`

	// kind classifies a not-ready Reason for refine's early answer.
	kind summarize.Kind
}

type ollamaSummaryState struct {
	Reachable bool   `json:"reachable"`
	Model     string `json:"model"`
	Installed bool   `json:"installed"`
}

// cloudSummaryState's quota fields are omitted when catchm-api's quota
// endpoint can't be reached (or the user is logged out).
type cloudSummaryState struct {
	LoggedIn       bool   `json:"logged_in"`
	Consent        bool   `json:"consent"`
	RemainingToday *int   `json:"remaining_today,omitempty"`
	Limit          *int   `json:"limit,omitempty"`
	ResetAt        string `json:"reset_at,omitempty"`
}

type openaiSummaryState struct {
	Configured bool `json:"configured"`
}

// cloudAuth snapshots the cloud-sync session for a catchm-cloud
// summarizer; Token is empty when logged out.
func (s *Server) cloudAuth() summarize.CloudAuth {
	base := s.cloudClient().BaseURL()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cloud == nil {
		return summarize.CloudAuth{BaseURL: base}
	}
	return summarize.CloudAuth{BaseURL: base, Token: s.cloud.sessionToken}
}

// handleSummaryStatus reports whether summaries can run with the chosen
// provider. It always answers 200: an unreachable Ollama or cloud is a
// status, not a server error. It never sends meeting data anywhere.
func (s *Server) handleSummaryStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := *s.cfg
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), summaryStatusTimeout)
	defer cancel()
	st := summaryReadiness(ctx, &cfg, s.cloudAuth(), true)
	info := sysinfo.Read()
	st.System = &info
	writeJSON(w, http.StatusOK, st)
}

// summaryReadiness probes the providers and sets Ready, Reason and
// notReady for the active one. With all, every provider's block is
// probed (for the settings page); otherwise only the active provider's.
// It never sends meeting data anywhere.
func summaryReadiness(ctx context.Context, cfg *config.Config, auth summarize.CloudAuth, all bool) summaryStatus {
	provider := summarize.ProviderOf(cfg)
	ollama := &ollamaSummaryState{Model: cfg.Summary.OllamaModel}
	cloud := &cloudSummaryState{LoggedIn: auth.Token != "", Consent: cfg.Summary.CloudConsent}
	var wg sync.WaitGroup
	if all || provider == summarize.ProviderOllama {
		wg.Go(func() {
			names, err := translate.ListModels(ctx, cfg.Translate.Host)
			if err != nil {
				return
			}
			ollama.Reachable = true
			ollama.Installed = modelInstalled(names, cfg.Summary.OllamaModel)
		})
	}
	if cloud.LoggedIn && (all || provider == summarize.ProviderCloud) {
		wg.Go(func() {
			q, err := (&summarize.Cloud{BaseURL: auth.BaseURL, Token: auth.Token}).Quota(ctx)
			if err != nil {
				return
			}
			cloud.RemainingToday, cloud.Limit, cloud.ResetAt = &q.RemainingToday, &q.Limit, q.ResetAt
		})
	}
	wg.Wait()
	openai := &openaiSummaryState{Configured: cfg.Summary.OpenAI.BaseURL != "" && cfg.Summary.OpenAI.Model != ""}

	st := summaryStatus{Provider: provider, Ollama: ollama, Cloud: cloud, OpenAI: openai}
	st.kind = summarize.KindNotReady
	switch st.Provider {
	case summarize.ProviderOllama:
		switch {
		case !ollama.Reachable:
			st.Reason = "Ollama isn't running. Install or start Ollama, then check again."
		case !ollama.Installed:
			st.Reason = fmt.Sprintf("The summary model %s isn't downloaded yet.", ollama.Model)
		}
	case summarize.ProviderCloud:
		switch {
		case !cloud.LoggedIn:
			st.Reason = "Log in to CatchM Cloud in Settings → Cloud sync to use cloud summaries."
		case !cloud.Consent:
			st.Reason = "Allow CatchM Cloud to receive your transcripts to use cloud summaries."
			st.kind = summarize.KindConsentRequired
		case cloud.RemainingToday != nil && *cloud.RemainingToday <= 0:
			st.Reason = "You've used today's free cloud summaries."
			st.kind = summarize.KindQuotaExceeded
		}
	case summarize.ProviderOpenAI:
		if !openai.Configured {
			st.Reason = "Add the server address and model name to use this option."
		}
	default:
		st.Reason = fmt.Sprintf("Unknown summary provider %q. Pick one in Settings → Summaries.", st.Provider)
	}
	st.Ready = st.Reason == ""
	return st
}

// err is st's not-ready state as a classified summary error, or nil
// when ready.
func (st summaryStatus) err() error {
	if st.Ready {
		return nil
	}
	se := &summarize.Error{Kind: st.kind, Message: st.Reason}
	if st.kind == summarize.KindQuotaExceeded {
		se.ResetAt = st.Cloud.ResetAt
	}
	return se
}

// modelInstalled matches Ollama's tag list, where an untagged name is
// listed as "<name>:latest".
func modelInstalled(names []string, model string) bool {
	if model == "" {
		return false
	}
	for _, n := range names {
		if n == model || n == model+":latest" {
			return true
		}
	}
	return false
}

// validateSummaryBaseURL checks an OpenAI-compatible base URL: absolute
// http(s) with a host and no userinfo, query or fragment. The trailing
// slash is trimmed as normalization.
func validateSummaryBaseURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("must be an absolute http or https URL")
	}
	if u.User != nil {
		return "", fmt.Errorf("userinfo not allowed")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", fmt.Errorf("query and fragment not allowed")
	}
	return trimmed, nil
}
