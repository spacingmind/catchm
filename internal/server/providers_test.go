package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/spacingmind/catchm/internal/config"
	"github.com/spacingmind/catchm/internal/meeting"
	"github.com/spacingmind/catchm/internal/translate"
)

func TestVerifyProviderEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	var gotName, gotKey string
	orig := verifyProvider
	t.Cleanup(func() { verifyProvider = orig })
	verifyProvider = func(_ context.Context, name, key string, _ *Server) error {
		gotName, gotKey = name, key
		if key != "good" {
			return errors.New("status 400: API key not valid")
		}
		return nil
	}

	var v struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	decode(t, do(t, s, "POST", "/api/providers/google/verify", `{"api_key":"good"}`), &v)
	if !v.OK || gotName != "google" || gotKey != "good" {
		t.Fatalf("good key: %+v (%s %s)", v, gotName, gotKey)
	}
	decode(t, do(t, s, "POST", "/api/providers/google/verify", `{"api_key":"bad"}`), &v)
	if v.OK || v.Error != "status 400: API key not valid" {
		t.Fatalf("bad key: %+v", v)
	}
	// No key typed and none stored → 400; a stored one is used.
	if w := do(t, s, "POST", "/api/providers/google/verify", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("no key: %d", w.Code)
	}
	s.cfg.Translate.Google.APIKey = "good"
	decode(t, do(t, s, "POST", "/api/providers/google/verify", ""), &v)
	if !v.OK {
		t.Fatalf("stored key: %+v", v)
	}
	// llm needs a resolvable server first.
	if w := do(t, s, "POST", "/api/providers/llm/verify", `{"api_key":"good"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("llm without preset: %d", w.Code)
	}
	s.cfg.LLM = config.LLMConfig{Preset: config.LLMOpenAI}
	decode(t, do(t, s, "POST", "/api/providers/llm/verify", `{"api_key":"good"}`), &v)
	if !v.OK || gotName != "llm" {
		t.Fatalf("llm: %+v", v)
	}
	if w := do(t, s, "POST", "/api/providers/bing/verify", `{"api_key":"x"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown provider: %d", w.Code)
	}
}

func TestBuildTranslatorByProvider(t *testing.T) {
	cfg := &config.Config{Translate: config.TranslateConfig{Provider: config.TranslateGoogle, LiveModel: "live", BatchModel: "batch"}}
	if _, err := buildTranslator(cfg, "live"); err == nil {
		t.Fatal("google without a key built a translator")
	}
	cfg.Translate.Google.APIKey = "k"
	if tr, err := buildTranslator(cfg, "live"); err != nil {
		t.Fatal(err)
	} else if _, ok := tr.(*translate.GoogleTranslator); !ok {
		t.Fatalf("google → %T", tr)
	}

	cfg.Translate.Provider = config.TranslateLLM
	if _, err := buildTranslator(cfg, "live"); err == nil {
		t.Fatal("llm without setup built a translator")
	}
	cfg.LLM = config.LLMConfig{Preset: config.LLMGroq, APIKey: "k"}
	live, _ := buildTranslator(cfg, "live")
	batch, _ := buildTranslator(cfg, "batch")
	p := config.LLMPresets[config.LLMGroq]
	if live.(*translate.LLMTranslator).Model != p.FastModel || batch.(*translate.LLMTranslator).Model != p.StrongModel {
		t.Fatalf("models: live %s batch %s", live.(*translate.LLMTranslator).Model, batch.(*translate.LLMTranslator).Model)
	}

	cfg.Translate.Provider = config.TranslateOllama
	if tr, _ := buildTranslator(cfg, "live"); tr.(*translate.OllamaTranslator).Model != "live" {
		t.Fatal("ollama didn't get the requested model")
	}
}

func TestTranslateStatusCloudProviders(t *testing.T) {
	s, _ := newTestServer(t)
	var v struct {
		Provider string `json:"provider"`
		Ready    bool   `json:"ready"`
		Reason   string `json:"reason"`
	}
	s.cfg.Translate.Provider = config.TranslateGoogle
	decode(t, do(t, s, "GET", "/api/translate/status", ""), &v)
	if v.Provider != "google" || v.Ready || v.Reason == "" {
		t.Fatalf("google no key: %+v", v)
	}
	s.cfg.Translate.Google.APIKey = "k"
	decode(t, do(t, s, "GET", "/api/translate/status", ""), &v)
	if !v.Ready {
		t.Fatalf("google with key: %+v", v)
	}
	s.cfg.Translate.Provider = config.TranslateLLM
	s.cfg.LLM = config.LLMConfig{Preset: config.LLMOpenAI, APIKey: "k"}
	v.Reason = ""
	decode(t, do(t, s, "GET", "/api/translate/status", ""), &v)
	if v.Provider != "llm" || !v.Ready {
		t.Fatalf("llm: %+v", v)
	}
}

// Deepgram goes through the cloud engine registry everywhere engines
// are named.
func TestDeepgramRegistered(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	s, _ := newTestServer(t)

	if w := do(t, s, "PUT", "/api/config", `{"engine":"deepgram","deepgram_api_key":"dg"}`); w.Code != http.StatusOK {
		t.Fatalf("put deepgram: %d %s", w.Code, w.Body.String())
	}
	var cfg struct {
		Engine string `json:"engine"`
		KeySet bool   `json:"deepgram_api_key_set"`
	}
	decode(t, do(t, s, "GET", "/api/config", ""), &cfg)
	if cfg.Engine != "deepgram" || !cfg.KeySet {
		t.Fatalf("config = %+v", cfg)
	}
	if w := do(t, s, "PUT", "/api/config", `{"engine":"assemblyai"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown engine accepted: %d", w.Code)
	}

	w := do(t, s, "GET", "/api/stt/status", "")
	var raw map[string]any
	decode(t, w, &raw)
	if raw["default_engine"] != "deepgram" {
		t.Fatalf("default_engine = %v", raw["default_engine"])
	}
	if dg, _ := raw["deepgram"].(map[string]any); dg["ready"] != true {
		t.Fatalf("deepgram status = %v", raw["deepgram"])
	}

	m := &meeting.Meeting{Engine: "deepgram"}
	unreachableWhisper(s)
	if got, err := selectRefineEngine(context.Background(), s.cfg, m, "auto"); err != nil || got != "deepgram" {
		t.Fatalf("auto refine engine = %q, %v", got, err)
	}
	s.cfg.Deepgram.APIKey = ""
	if _, err := newEngine("deepgram", s.cfg); !isMissingKey(err) || !strings.Contains(err.Error(), "Deepgram API key is not set") {
		t.Fatalf("missing key err = %v", err)
	}
	if _, err := selectRefineEngine(context.Background(), s.cfg, m, "deepgram"); err == nil {
		t.Fatal("explicit deepgram without a key was accepted")
	}
}
