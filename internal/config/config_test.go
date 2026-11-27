package config

import (
	"os"
	"path/filepath"
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
		OpenAI:       OpenAICompatConfig{BaseURL: "http://127.0.0.1:11435/v1", Model: "gpt-5", APIKey: "sk-sum"},
		CloudConsent: true,
	}
	if c.Summary != want {
		t.Fatalf("summary = %+v, want %+v", c.Summary, want)
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
	if *got != *want {
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

	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("engine: soniox\n"), 0o600); err != nil {
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
