package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Engine != defaultEngine || c.UILanguage != defaultUILanguage || c.WhisperURL != defaultWhisperURL || c.Soniox.URL != defaultSonioxURL {
		t.Fatalf("defaults not applied: %+v", c)
	}
	if c.Translate.Host != defaultOllamaHost || c.Translate.LiveModel != defaultLiveModel || c.Translate.BatchModel != defaultBatchModel {
		t.Fatalf("translate defaults not applied: %+v", c.Translate)
	}
	if c.Summary.Provider != defaultSummaryProvider || c.Summary.OllamaModel != defaultSummaryModel || c.Summary.CloudConsent {
		t.Fatalf("summary defaults not applied: %+v", c.Summary)
	}
}

func TestLoadSummaryOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)
	yaml := `
summary:
  provider: openai
  openai:
    base_url: http://127.0.0.1:11435/v1
    model: gpt-5
    api_key: sk-sum
  cloud_consent: true
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := SummaryConfig{
		Provider:     "openai",
		OllamaModel:  defaultSummaryModel, // unset: default kept
		CloudConsent: true,
	}
	if c.Summary != want {
		t.Fatalf("summary = %+v, want %+v", c.Summary, want)
	}
	// The old summary.openai server moves into the shared llm block.
	wantLLM := LLMConfig{Preset: LLMCustom, BaseURL: "http://127.0.0.1:11435/v1", APIKey: "sk-sum", FastModel: "gpt-5", StrongModel: "gpt-5"}
	if c.LLM != wantLLM {
		t.Fatalf("llm = %+v, want %+v", c.LLM, wantLLM)
	}
}

// A Cloudflare Workers AI URL (all the old settings UI ever wrote)
// becomes the cloudflare preset with its account ID.
func TestMigrateCloudflareSummary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)
	yaml := "summary:\n  provider: openai\n  openai:\n    base_url: https://api.cloudflare.com/client/v4/accounts/abc123/ai/v1\n    model: \"@cf/qwen/qwen3-30b-a3b-fp8\"\n    api_key: cf-tok\n"
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.Preset != LLMCloudflare || c.LLM.AccountID != "abc123" || c.LLM.APIKey != "cf-tok" || c.LLM.BaseURL != "" {
		t.Fatalf("llm = %+v", c.LLM)
	}
	base, fast, strong := c.LLM.Resolved()
	if base != "https://api.cloudflare.com/client/v4/accounts/abc123/ai/v1" || fast != "@cf/qwen/qwen3-30b-a3b-fp8" || strong != fast {
		t.Fatalf("resolved = %s %s %s", base, fast, strong)
	}
	// Saved and reloaded, it stays put and summary.openai is gone.
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, "config.yaml"))
	if strings.Contains(string(raw), "openai:") {
		t.Fatalf("summary.openai written again:\n%s", raw)
	}
}

func TestLLMResolvedDefaults(t *testing.T) {
	base, fast, strong := LLMConfig{Preset: LLMGemini, StrongModel: "my-strong"}.Resolved()
	if base != LLMPresets[LLMGemini].BaseURL || fast != LLMPresets[LLMGemini].FastModel || strong != "my-strong" {
		t.Fatalf("gemini = %s %s %s", base, fast, strong)
	}
	if b, _, _ := (LLMConfig{Preset: LLMCloudflare}).Resolved(); b != "" {
		t.Fatalf("cloudflare without account resolved to %q", b)
	}
	if (LLMConfig{Preset: LLMOpenAI}).Ready() {
		t.Fatal("ready without a key")
	}
	if !(LLMConfig{Preset: LLMOpenAI, APIKey: "k"}).Ready() {
		t.Fatal("openai preset + key not ready")
	}
}

func TestLoadOverridesDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)
	yaml := `
engine: soniox
lang: vi
soniox:
  api_key: sk-test
translate:
  host: http://localhost:1234
  live_model: custom-live
  batch_model: custom-batch
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Engine != "soniox" || c.Lang != "vi" || c.Soniox.APIKey != "sk-test" {
		t.Fatalf("overrides not applied: %+v", c)
	}
	if c.Translate.Host != "http://localhost:1234" || c.Translate.LiveModel != "custom-live" || c.Translate.BatchModel != "custom-batch" {
		t.Fatalf("translate overrides not applied: %+v", c.Translate)
	}
	// Unset fields still get their defaults.
	if c.UILanguage != defaultUILanguage || c.WhisperURL != defaultWhisperURL || c.Soniox.URL != defaultSonioxURL {
		t.Fatalf("defaults not preserved alongside overrides: %+v", c)
	}
}

func TestConfigSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("CATCHM_HOME", t.TempDir())
	want := &Config{
		Setup:      SetupCloud,
		Engine:     "soniox",
		Lang:       "vi",
		UILanguage: "vi",
		WhisperURL: "http://localhost:9999",
		Soniox:     SonioxConfig{APIKey: "sk-secret", URL: "wss://example.com/ws"},
		Cloud:      CloudConfig{APIBaseURL: defaultCloudURL},
		Translate: TranslateConfig{
			Provider:   TranslateNone,
			Host:       "http://localhost:1234",
			LiveModel:  "custom-live",
			BatchModel: "custom-batch",
		},
	}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

// Setup: a fresh install awaits the chooser (""), a config written
// before setups existed loads as custom, and an explicit choice
// round-trips — including a saved-but-unchosen "" (e.g. the UI language
// was changed before the chooser).
func TestSetupMigration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Setup != "" {
		t.Fatalf("fresh install setup = %q, want empty", c.Setup)
	}

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("engine: whisper\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, _ = Load(); c.Setup != SetupCustom {
		t.Fatalf("legacy config setup = %q, want custom", c.Setup)
	}
	if c.Translate.Provider != TranslateOllama {
		t.Fatalf("legacy translate.provider = %q, want ollama", c.Translate.Provider)
	}

	c.Setup = ""
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	if c, _ = Load(); c.Setup != "" {
		t.Fatalf("saved unchosen setup reloaded as %q, want empty", c.Setup)
	}

	c.Setup, c.Translate.Provider = SetupCloud, TranslateNone
	if err := Save(c); err != nil {
		t.Fatal(err)
	}
	if c, _ = Load(); c.Setup != SetupCloud || c.Translate.Provider != TranslateNone {
		t.Fatalf("round trip = %q/%q", c.Setup, c.Translate.Provider)
	}
}

// With Soniox, translation is always Soniox's own.
func TestSonioxLocksTranslation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CATCHM_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("engine: soniox\ntranslate:\n  provider: google\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Translate.Provider != TranslateNone {
		t.Fatalf("soniox + google loaded as %q, want none", c.Translate.Provider)
	}
	c.Engine, c.Translate.Provider = "whisper", TranslateGoogle
	c.Normalize()
	if c.Translate.Provider != TranslateGoogle {
		t.Fatalf("whisper changed the provider to %q", c.Translate.Provider)
	}
}

func TestZAIPreset(t *testing.T) {
	base, fast, strong := LLMConfig{Preset: LLMZAI, APIKey: "k"}.Resolved()
	if base != "https://api.z.ai/api/paas/v4" || fast != "glm-5.1" || strong != "glm-5.1" {
		t.Fatalf("zai = %s %s %s", base, fast, strong)
	}
}
