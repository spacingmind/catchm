package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spacingmind/catchm/internal/audio"
	"github.com/spacingmind/catchm/internal/cloudsync"
	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/stt"
	"github.com/spacingmind/catchm/internal/summarize"
)

// fakeSummarizer stands in for a real provider in HTTP-layer tests.
type fakeSummarizer struct {
	ex  summarize.Extraction
	err error
}

func (f *fakeSummarizer) Summarize(context.Context, summarize.Input) (summarize.Extraction, error) {
	return f.ex, f.err
}

// stubSummarizer rigs s.newSummarizer to always hand back sum, and
// points the (default Ollama) provider at a fake Ollama that has the
// model, so refine's readiness check passes.
func stubSummarizer(t *testing.T, s *Server, sum summarize.Summarizer) {
	t.Helper()
	s.newSummarizer = func(*config.Config, summarize.CloudAuth) (summarize.Summarizer, error) { return sum, nil }
	ollama := fakeOllamaTagsServer(t, "stub-model")
	t.Cleanup(ollama.Close)
	s.cfg.Translate.Host = ollama.URL
	s.cfg.Summary.OllamaModel = "stub-model"
}

// countBatchEngine wraps s.newBatchEngine to count how often refine
// builds a batch engine, i.e. reaches the batch pass.
func countBatchEngine(s *Server) *atomic.Int32 {
	var n atomic.Int32
	next := s.newBatchEngine
	s.newBatchEngine = func(cfg *config.Config) (stt.BatchEngine, error) {
		n.Add(1)
		return next(cfg)
	}
	return &n
}

// refinableMeeting creates an ended meeting with an audio.wav and a live
// transcript line, and points s at a fake Whisper batch server.
func refinableMeeting(t *testing.T, s *Server, store *meeting.Store) *meeting.Meeting {
	t.Helper()
	m, err := store.Start("Standup", time.Now().UTC(), meeting.StartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(m.ID, meeting.Utterance{Text: "live hello"}); err != nil {
		t.Fatal(err)
	}
	if err := store.End(m.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	aw, err := audio.NewWriter(filepath.Join(store.Root(), m.ID, "audio.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := aw.Write(make([]byte, 1600)); err != nil {
		t.Fatal(err)
	}
	if err := aw.Close(); err != nil {
		t.Fatal(err)
	}
	whisper := fakeWhisperBatchServer(t)
	t.Cleanup(whisper.Close)
	s.cfg.WhisperURL = whisper.URL
	return m
}

type refineErrBody struct {
	Error   string  `json:"error"`
	Kind    string  `json:"kind"`
	ResetAt *string `json:"reset_at"`
}

func TestRefineSummaryErrorKinds(t *testing.T) {
	for _, tc := range []struct {
		err     *summarize.Error
		status  int
		resetAt string
	}{
		{&summarize.Error{Kind: summarize.KindNotReady, Message: "Ollama isn't running."}, http.StatusServiceUnavailable, ""},
		{&summarize.Error{Kind: summarize.KindConsentRequired, Message: "consent"}, http.StatusConflict, ""},
		{&summarize.Error{Kind: summarize.KindQuotaExceeded, Message: "quota", ResetAt: "2026-09-29T00:00:00Z"}, http.StatusTooManyRequests, "2026-09-29T00:00:00Z"},
		{&summarize.Error{Kind: summarize.KindFailed, Message: "bad JSON", Err: fmt.Errorf("internal detail")}, http.StatusBadGateway, ""},
	} {
		t.Run(string(tc.err.Kind), func(t *testing.T) {
			s, store := newTestServer(t)
			m := refinableMeeting(t, s, store)
			stubSummarizer(t, s, &fakeSummarizer{err: tc.err})

			w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, tc.status, w.Body.String())
			}
			var v refineErrBody
			decode(t, w, &v)
			if v.Kind != string(tc.err.Kind) || v.Error != tc.err.Message {
				t.Fatalf("body = %+v", v)
			}
			if tc.resetAt == "" && v.ResetAt != nil || tc.resetAt != "" && (v.ResetAt == nil || *v.ResetAt != tc.resetAt) {
				t.Fatalf("reset_at = %v, want %q", v.ResetAt, tc.resetAt)
			}
			if got, _ := store.Get(m.ID); got.Refined {
				t.Fatal("refined set despite a failed summary")
			}
		})
	}
}

func TestRefineConstructionErrorSkipsBatchPass(t *testing.T) {
	s, store := newTestServer(t)
	m := refinableMeeting(t, s, store)
	var batchHits atomic.Int32
	whisper := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { batchHits.Add(1) }))
	defer whisper.Close()
	s.cfg.WhisperURL = whisper.URL
	s.cfg.Summary.Provider = summarize.ProviderOpenAI // no base URL: not ready

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
	var v refineErrBody
	decode(t, w, &v)
	if w.Code != http.StatusServiceUnavailable || v.Kind != "summary_not_ready" {
		t.Fatalf("status = %d, body = %+v", w.Code, v)
	}
	if batchHits.Load() != 0 {
		t.Fatal("batch transcription ran although the summary could never succeed")
	}
}

// TestRefineCloudWithoutConsentSendsNothing is the consent guard: with
// catchm-cloud chosen, a live session, and cloud_consent false, refine
// answers consent_required and the cloud backend sees no request.
func TestRefineCloudWithoutConsentSendsNothing(t *testing.T) {
	s, store := newTestServer(t)
	m := refinableMeeting(t, s, store)
	var cloudHits atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { cloudHits.Add(1) }))
	defer cloud.Close()
	s.newCloudClient = func() *cloudsync.Client { return cloudsync.NewClient(cloud.URL) }
	s.cloud = &cloudState{email: "a@b.c", sessionToken: "tok"}
	s.cfg.Summary.Provider = summarize.ProviderCloud
	s.cfg.Summary.CloudConsent = false

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
	var v refineErrBody
	decode(t, w, &v)
	if w.Code != http.StatusConflict || v.Kind != "consent_required" {
		t.Fatalf("status = %d, body = %+v", w.Code, v)
	}
	if cloudHits.Load() != 0 {
		t.Fatalf("cloud received %d requests without consent", cloudHits.Load())
	}
}

func TestRefineWithCloudProvider(t *testing.T) {
	s, store := newTestServer(t)
	m := refinableMeeting(t, s, store)
	var sent struct {
		Title, Transcript string
	}
	quotaLeft := true
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("unexpected %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.URL.Path == "/v1/summarize/quota" {
			// Quota says there's room, so the second call below reaches
			// /v1/summarize's own 429.
			fmt.Fprint(w, `{"remaining_today":5,"limit":5,"reset_at":"2026-09-29T00:00:00Z"}`)
			return
		}
		if r.URL.Path != "/v1/summarize" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&sent)
		if !quotaLeft {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":"quota_exceeded","reset_at":"2026-09-29T00:00:00Z"}`)
			return
		}
		fmt.Fprint(w, `{"summary":"cloud summary","participants":[],"decisions":[],"action_items":[{"text":"ship"}],"remaining_today":4}`)
	}))
	defer cloud.Close()
	s.newCloudClient = func() *cloudsync.Client { return cloudsync.NewClient(cloud.URL) }
	s.cloud = &cloudState{email: "a@b.c", sessionToken: "tok"}
	s.cfg.Summary.Provider = summarize.ProviderCloud
	s.cfg.Summary.CloudConsent = true

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var got meeting.Meeting
	decode(t, w, &got)
	if !got.Refined || got.Summary != "cloud summary" || len(got.ActionItems) != 1 {
		t.Fatalf("meeting = %+v", got)
	}
	// The batch (v2) transcript is what gets summarized.
	if sent.Title != "Standup" || !strings.Contains(sent.Transcript, "final hello") || strings.Contains(sent.Transcript, "live hello") {
		t.Fatalf("sent = %+v", sent)
	}

	quotaLeft = false
	w = do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
	var v refineErrBody
	decode(t, w, &v)
	if w.Code != http.StatusTooManyRequests || v.Kind != "quota_exceeded" || v.ResetAt == nil || *v.ResetAt != "2026-09-29T00:00:00Z" {
		t.Fatalf("quota: status = %d, body = %+v", w.Code, v)
	}
}

func TestRefineWithOllamaProvider(t *testing.T) {
	s, store := newTestServer(t)
	m := refinableMeeting(t, s, store)
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			fmt.Fprint(w, `{"models":[{"name":"qwen3.5:9b"}]}`)
			return
		}
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["model"] != "qwen3.5:9b" || req["format"] == nil {
			t.Errorf("request = %v", req)
		}
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{
			"content": `{"summary":"local summary","participants":["alex"],"decisions":[{"text":"go","scope":""}],"action_items":[]}`,
		}})
	}))
	defer ollama.Close()
	s.cfg.Translate.Host = ollama.URL
	s.cfg.Summary.OllamaModel = "qwen3.5:9b"

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var got meeting.Meeting
	decode(t, w, &got)
	if !got.Refined || got.Summary != "local summary" || len(got.Decisions) != 1 {
		t.Fatalf("meeting = %+v", got)
	}
}

// TestRefineReadinessSkipsBatchPass: a summary that can't run (Ollama
// down or missing the model, cloud logged out or out of quota) answers
// at once, without building a batch engine or calling the summarizer.
func TestRefineReadinessSkipsBatchPass(t *testing.T) {
	quotaZero := func(t *testing.T, s *Server) {
		cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/summarize/quota" {
				t.Errorf("meeting data sent to %s despite no quota", r.URL.Path)
			}
			fmt.Fprint(w, `{"remaining_today":0,"limit":5,"reset_at":"2026-09-29T00:00:00Z"}`)
		}))
		t.Cleanup(cloud.Close)
		s.newCloudClient = func() *cloudsync.Client { return cloudsync.NewClient(cloud.URL) }
		s.cloud = &cloudState{email: "a@b.c", sessionToken: "tok"}
		s.cfg.Summary.Provider = summarize.ProviderCloud
		s.cfg.Summary.CloudConsent = true
	}
	for _, tc := range []struct {
		name    string
		setup   func(*testing.T, *Server)
		status  int
		kind    string
		reason  string
		resetAt string
	}{
		{"ollama down", func(t *testing.T, s *Server) {
			down := httptest.NewServer(http.NotFoundHandler())
			down.Close()
			s.cfg.Translate.Host = down.URL
			s.cfg.Summary.OllamaModel = "qwen3.5:9b"
		}, http.StatusServiceUnavailable, "summary_not_ready", "Ollama isn't running. Install or start Ollama, then check again.", ""},
		{"model missing", func(t *testing.T, s *Server) {
			ollama := fakeOllamaTagsServer(t, "translategemma:4b")
			t.Cleanup(ollama.Close)
			s.cfg.Translate.Host = ollama.URL
			s.cfg.Summary.OllamaModel = "qwen3.5:9b"
		}, http.StatusServiceUnavailable, "summary_not_ready", "The summary model qwen3.5:9b isn't downloaded yet.", ""},
		{"cloud logged out", func(t *testing.T, s *Server) {
			s.cfg.Summary.Provider = summarize.ProviderCloud
			s.cfg.Summary.CloudConsent = true
		}, http.StatusServiceUnavailable, "summary_not_ready", "Log in to CatchM Cloud in Settings → Cloud sync to use cloud summaries.", ""},
		{"cloud quota used up", quotaZero, http.StatusTooManyRequests, "quota_exceeded", "You've used today's free cloud summaries.", "2026-09-29T00:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store := newTestServer(t)
			m := refinableMeeting(t, s, store)
			tc.setup(t, s)
			engines := countBatchEngine(s)
			var summarized atomic.Int32
			next := s.newSummarizer
			s.newSummarizer = func(cfg *config.Config, auth summarize.CloudAuth) (summarize.Summarizer, error) {
				sum, err := next(cfg, auth)
				if err != nil {
					return nil, err
				}
				return summarizerFunc(func(ctx context.Context, in summarize.Input) (summarize.Extraction, error) {
					summarized.Add(1)
					return sum.Summarize(ctx, in)
				}), nil
			}

			start := time.Now()
			w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
			var v refineErrBody
			decode(t, w, &v)
			if w.Code != tc.status || v.Kind != tc.kind || v.Error != tc.reason {
				t.Fatalf("status = %d, body = %+v", w.Code, v)
			}
			if tc.resetAt == "" && v.ResetAt != nil || tc.resetAt != "" && (v.ResetAt == nil || *v.ResetAt != tc.resetAt) {
				t.Fatalf("reset_at = %v, want %q", v.ResetAt, tc.resetAt)
			}
			if engines.Load() != 0 || summarized.Load() != 0 {
				t.Fatalf("batch engines built = %d, summaries = %d; want 0", engines.Load(), summarized.Load())
			}
			if d := time.Since(start); d > refineReadyTimeout {
				t.Fatalf("readiness check took %v", d)
			}
			if got, _ := store.Get(m.ID); got.Refined {
				t.Fatal("refined set although the summary wasn't ready")
			}
		})
	}
}

// TestRefineProceedsWhenQuotaUnreachable: a failing quota endpoint
// doesn't block refine; /v1/summarize itself is the authority.
func TestRefineProceedsWhenQuotaUnreachable(t *testing.T) {
	s, store := newTestServer(t)
	m := refinableMeeting(t, s, store)
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/summarize/quota" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, `{"summary":"cloud summary","participants":[],"decisions":[],"action_items":[],"remaining_today":4}`)
	}))
	defer cloud.Close()
	s.newCloudClient = func() *cloudsync.Client { return cloudsync.NewClient(cloud.URL) }
	s.cloud = &cloudState{email: "a@b.c", sessionToken: "tok"}
	s.cfg.Summary.Provider = summarize.ProviderCloud
	s.cfg.Summary.CloudConsent = true
	engines := countBatchEngine(s)

	w := do(t, s, "POST", "/api/meetings/"+m.ID+"/refine", "")
	if w.Code != http.StatusOK || engines.Load() != 1 {
		t.Fatalf("status = %d, engines = %d, body = %s", w.Code, engines.Load(), w.Body.String())
	}
}

// summarizerFunc adapts a function to summarize.Summarizer.
type summarizerFunc func(context.Context, summarize.Input) (summarize.Extraction, error)

func (f summarizerFunc) Summarize(ctx context.Context, in summarize.Input) (summarize.Extraction, error) {
	return f(ctx, in)
}

type summaryStatusBody struct {
	Provider string  `json:"provider"`
	Ready    bool    `json:"ready"`
	Reason   *string `json:"reason"`
	Ollama   *struct {
		Reachable bool   `json:"reachable"`
		Model     string `json:"model"`
		Installed bool   `json:"installed"`
	} `json:"ollama"`
	Cloud *struct {
		LoggedIn       bool   `json:"logged_in"`
		Consent        bool   `json:"consent"`
		RemainingToday *int   `json:"remaining_today"`
		Limit          *int   `json:"limit"`
		ResetAt        string `json:"reset_at"`
	} `json:"cloud"`
	OpenAI *struct {
		Configured bool `json:"configured"`
	} `json:"openai"`
	System *struct {
		TotalRAMGB *float64 `json:"total_ram_gb"`
		FreeDiskGB *float64 `json:"free_disk_gb"`
	} `json:"system"`
}

func getSummaryStatus(t *testing.T, s *Server) summaryStatusBody {
	t.Helper()
	w := do(t, s, "GET", "/api/summary/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var v summaryStatusBody
	decode(t, w, &v)
	return v
}

func TestSummaryStatusOllama(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg.Summary.OllamaModel = "qwen3.5:9b"

	ollama := fakeOllamaTagsServer(t, "qwen3.5:9b")
	s.cfg.Translate.Host = ollama.URL
	v := getSummaryStatus(t, s)
	if v.Provider != "ollama" || !v.Ready || v.Reason != nil || v.Ollama == nil || !v.Ollama.Reachable || !v.Ollama.Installed || v.Ollama.Model != "qwen3.5:9b" {
		t.Fatalf("installed: %+v ollama=%+v", v, v.Ollama)
	}
	ollama.Close()

	missing := fakeOllamaTagsServer(t, "translategemma:4b")
	defer missing.Close()
	s.cfg.Translate.Host = missing.URL
	v = getSummaryStatus(t, s)
	if v.Ready || v.Reason == nil || !v.Ollama.Reachable || v.Ollama.Installed {
		t.Fatalf("missing model: %+v ollama=%+v", v, v.Ollama)
	}

	s.cfg.Translate.Host = ollama.URL // closed
	v = getSummaryStatus(t, s)
	if v.Ready || v.Reason == nil || v.Ollama.Reachable {
		t.Fatalf("ollama down: %+v ollama=%+v", v, v.Ollama)
	}
}

// TestSummaryStatusSystem: the system block reports this machine's RAM
// and the free space where Ollama keeps models, rounded to one decimal.
func TestSummaryStatusSystem(t *testing.T) {
	t.Setenv("OLLAMA_MODELS", t.TempDir())
	s, _ := newTestServer(t)
	v := getSummaryStatus(t, s)
	if v.System == nil || v.System.TotalRAMGB == nil || v.System.FreeDiskGB == nil {
		t.Fatalf("system = %+v", v.System)
	}
	ram, disk := *v.System.TotalRAMGB, *v.System.FreeDiskGB
	if ram <= 0 || disk < 0 || math.Round(ram*10)/10 != ram || math.Round(disk*10)/10 != disk {
		t.Fatalf("total_ram_gb = %v, free_disk_gb = %v", ram, disk)
	}
}

func TestSummaryStatusCloud(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg.Summary.Provider = summarize.ProviderCloud

	v := getSummaryStatus(t, s)
	if v.Ready || v.Reason == nil || v.Cloud == nil || v.Cloud.LoggedIn || v.Cloud.RemainingToday != nil {
		t.Fatalf("logged out: %+v cloud=%+v", v, v.Cloud)
	}

	remaining := 4
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/summarize/quota" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("unexpected %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		fmt.Fprintf(w, `{"remaining_today":%d,"limit":5,"reset_at":"2026-09-29T00:00:00Z"}`, remaining)
	}))
	s.newCloudClient = func() *cloudsync.Client { return cloudsync.NewClient(cloud.URL) }
	s.cloud = &cloudState{email: "a@b.c", sessionToken: "tok"}

	v = getSummaryStatus(t, s)
	if v.Ready || v.Reason == nil || !v.Cloud.LoggedIn || v.Cloud.Consent {
		t.Fatalf("no consent: %+v cloud=%+v", v, v.Cloud)
	}

	s.cfg.Summary.CloudConsent = true
	v = getSummaryStatus(t, s)
	if !v.Ready || v.Reason != nil || v.Cloud.RemainingToday == nil || *v.Cloud.RemainingToday != 4 || *v.Cloud.Limit != 5 || v.Cloud.ResetAt != "2026-09-29T00:00:00Z" {
		t.Fatalf("ready: %+v cloud=%+v", v, v.Cloud)
	}

	remaining = 0
	v = getSummaryStatus(t, s)
	if v.Ready || v.Reason == nil {
		t.Fatalf("quota used up: %+v", v)
	}

	cloud.Close()
	v = getSummaryStatus(t, s)
	if !v.Ready || v.Cloud.RemainingToday != nil || v.Cloud.Limit != nil || v.Cloud.ResetAt != "" {
		t.Fatalf("quota unreachable should omit quota fields: %+v cloud=%+v", v, v.Cloud)
	}
}

func TestSummaryStatusOpenAI(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg.Summary.Provider = summarize.ProviderOpenAI
	v := getSummaryStatus(t, s)
	if v.Ready || v.Reason == nil || v.OpenAI == nil || v.OpenAI.Configured {
		t.Fatalf("unconfigured: %+v", v)
	}
	s.cfg.Summary.OpenAI = config.OpenAICompatConfig{BaseURL: "http://127.0.0.1:11435/v1", Model: "gpt-5"}
	v = getSummaryStatus(t, s)
	if !v.Ready || !v.OpenAI.Configured {
		t.Fatalf("configured: %+v", v)
	}
}

func TestTranslatePullSummaryTarget(t *testing.T) {
	s, _ := newTestServer(t)
	s.cfg.Summary.OllamaModel = "qwen3.5:9b"
	var pulled string
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		pulled, _ = req["model"].(string)
		fmt.Fprintln(w, `{"status":"success"}`)
	}))
	defer ollama.Close()
	s.cfg.Translate.Host = ollama.URL

	w := do(t, s, "POST", "/api/translate/pull", `{"target":"summary"}`)
	if w.Code != http.StatusOK || pulled != "qwen3.5:9b" {
		t.Fatalf("status = %d, pulled = %q", w.Code, pulled)
	}
}

// TestConfigSummaryRedactionAndPartialPut mirrors the Soniox key tests
// for summary.openai.api_key, and covers the rest of the summary block.
func TestConfigSummaryRedactionAndPartialPut(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	s, _ := newTestServer(t)

	w := do(t, s, "PUT", "/api/config", `{"summary":{"provider":"openai","openai":{"base_url":"http://127.0.0.1:11435/v1/","model":"gpt-5","api_key":"sk-sum-secret"}}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put: status = %d, body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "sk-sum-secret") {
		t.Fatalf("put response leaks the key: %s", w.Body.String())
	}

	type summaryBody struct {
		Summary struct {
			Provider    string            `json:"provider"`
			OllamaModel string            `json:"ollama_model"`
			OpenAI      map[string]string `json:"openai"`
			Consent     bool              `json:"cloud_consent"`
		} `json:"summary"`
		KeySet bool `json:"summary_openai_api_key_set"`
	}
	get := func() summaryBody {
		t.Helper()
		w := do(t, s, "GET", "/api/config", "")
		if strings.Contains(w.Body.String(), "sk-sum-secret") {
			t.Fatalf("get leaks the key: %s", w.Body.String())
		}
		var v summaryBody
		decode(t, w, &v)
		return v
	}
	v := get()
	if v.Summary.Provider != "openai" || v.Summary.OpenAI["base_url"] != "http://127.0.0.1:11435/v1" || v.Summary.OpenAI["model"] != "gpt-5" || !v.KeySet {
		t.Fatalf("get = %+v", v)
	}
	if _, ok := v.Summary.OpenAI["api_key"]; ok {
		t.Fatal("openai.api_key present in GET")
	}

	// Omitted or blank key keeps the stored one; consent toggles both ways.
	for _, body := range []string{
		`{"summary":{"cloud_consent":true}}`,
		`{"summary":{"openai":{"api_key":""}}}`,
		`{"engine":"whisper"}`,
	} {
		if w := do(t, s, "PUT", "/api/config", body); w.Code != http.StatusOK {
			t.Fatalf("put %s: status = %d", body, w.Code)
		}
		if v := get(); !v.KeySet || v.Summary.Provider != "openai" || v.Summary.OpenAI["model"] != "gpt-5" {
			t.Fatalf("put %s lost fields: %+v", body, v)
		}
	}
	if !get().Summary.Consent {
		t.Fatal("cloud_consent true not saved")
	}
	do(t, s, "PUT", "/api/config", `{"summary":{"cloud_consent":false}}`)
	if get().Summary.Consent {
		t.Fatal("cloud_consent false not saved")
	}

	onDisk, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.Summary.OpenAI.APIKey != "sk-sum-secret" {
		t.Fatalf("key on disk = %q", onDisk.Summary.OpenAI.APIKey)
	}

	// Pointing at a different server drops a key entered for the old one.
	do(t, s, "PUT", "/api/config", `{"summary":{"openai":{"base_url":"https://openrouter.ai/api/v1"}}}`)
	if v := get(); v.KeySet || v.Summary.OpenAI["base_url"] != "https://openrouter.ai/api/v1" {
		t.Fatalf("base_url change kept the old key: %+v", v)
	}

	for _, bad := range []string{
		`{"summary":{"provider":"claude"}}`,
		`{"summary":{"openai":{"base_url":"not a url"}}}`,
		`{"summary":{"openai":{"base_url":"ftp://x/v1"}}}`,
	} {
		if w := do(t, s, "PUT", "/api/config", bad); w.Code != http.StatusBadRequest {
			t.Fatalf("put %s: status = %d, want 400", bad, w.Code)
		}
	}
}

func TestConfigSummaryDefaultsOnGet(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	s, _ := newTestServer(t)
	s.cfg = cfg
	w := do(t, s, "GET", "/api/config", "")
	var v struct {
		Summary struct {
			Provider    string `json:"provider"`
			OllamaModel string `json:"ollama_model"`
		} `json:"summary"`
		KeySet *bool `json:"summary_openai_api_key_set"`
	}
	decode(t, w, &v)
	if v.Summary.Provider != "ollama" || v.Summary.OllamaModel != "qwen3.5:9b" || v.KeySet == nil || *v.KeySet {
		t.Fatalf("defaults = %+v", v)
	}
}
