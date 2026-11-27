// Package config loads CatchM's small YAML config file: STT engine
// defaults, the Soniox API key, and the summary provider settings.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SonioxConfig holds Soniox connection settings.
type SonioxConfig struct {
	APIKey string `yaml:"api_key"`
	URL    string `yaml:"url"`
}

// TranslateConfig holds settings for the local Ollama translation layer
// used by engines with no native translation support.
type TranslateConfig struct {
	// Provider translates text for engines with no native translation:
	// "ollama" (local; the default, and what "" means for configs written
	// before it existed), "google" (Cloud Translation API), "llm"
	// (the shared OpenAI-compatible LLM block) or "none" (only the engine's
	// own translation, e.g. Soniox). See docs/plans/active/cloud-mode.md.
	Provider   string       `yaml:"provider"`
	Google     APIKeyConfig `yaml:"google"`
	Host       string       `yaml:"host"`
	LiveModel  string       `yaml:"live_model"`
	BatchModel string       `yaml:"batch_model"`
}

// APIKeyConfig is a provider that needs nothing but a key.
type APIKeyConfig struct {
	APIKey string `yaml:"api_key"` // secret: never returned by GET
}

// LLMConfig is the one OpenAI-compatible chat completions provider shared
// by translation (translate.provider "llm", FastModel) and summaries
// (summary.provider "openai", StrongModel). Preset picks the server and
// default models (LLMPresets); "custom" uses BaseURL as given.
type LLMConfig struct {
	Preset      string `yaml:"preset"`
	BaseURL     string `yaml:"base_url"`     // custom preset only; presets resolve theirs
	AccountID   string `yaml:"account_id"`   // cloudflare preset only
	APIKey      string `yaml:"api_key"`      // secret: never returned by GET
	FastModel   string `yaml:"fast_model"`   // live translation; "" = preset default
	StrongModel string `yaml:"strong_model"` // summaries, batch translation; "" = preset default
}

// LLMPreset is one OpenAI-compatible provider's server and default
// models (docs/plans/active/cloud-mode-providers.md).
type LLMPreset struct {
	BaseURL     string // "{account}" is replaced by LLMConfig.AccountID
	FastModel   string
	StrongModel string
}

// LLM presets (LLMConfig.Preset).
const (
	LLMOpenAI     = "openai"
	LLMGemini     = "gemini"
	LLMOpenRouter = "openrouter"
	LLMAnthropic  = "anthropic"
	LLMGroq       = "groq"
	LLMDeepSeek   = "deepseek"
	LLMCloudflare = "cloudflare"
	LLMCustom     = "custom"
)

// LLMPresets maps each preset to its server and default models. Model
// ids are the vendors' current fast/strong picks as of 2026-11; the user
// can override both.
var LLMPresets = map[string]LLMPreset{
	LLMOpenAI:     {BaseURL: "https://api.openai.com/v1", FastModel: "gpt-6-luna", StrongModel: "gpt-6.1-terra"},
	LLMGemini:     {BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", FastModel: "gemini-2.5-flash-lite", StrongModel: "gemini-3.8-flash"},
	LLMOpenRouter: {BaseURL: "https://openrouter.ai/api/v1", FastModel: "anthropic/claude-haiku-5.5", StrongModel: "anthropic/claude-sonnet-5.5"},
	LLMAnthropic:  {BaseURL: "https://api.anthropic.com/v1", FastModel: "claude-haiku-5-5", StrongModel: "claude-sonnet-5-5"},
	LLMGroq:       {BaseURL: "https://api.groq.com/openai/v1", FastModel: "openai/gpt-oss-20b", StrongModel: "openai/gpt-oss-120b"},
	LLMDeepSeek:   {BaseURL: "https://api.deepseek.com", FastModel: "deepseek-flash", StrongModel: "deepseek-v4-pro"},
	LLMCloudflare: {BaseURL: "https://api.cloudflare.com/client/v4/accounts/{account}/ai/v1", FastModel: "@cf/qwen/qwen3-30b-a3b-fp8", StrongModel: "@cf/qwen/qwen3-30b-a3b-fp8"},
}

// Resolved returns the chat completions base URL and the fast/strong
// models after applying the preset's defaults. BaseURL is "" when the
// config can't name a server yet (no preset, custom without a URL,
// cloudflare without an account ID).
func (l LLMConfig) Resolved() (baseURL, fast, strong string) {
	p, ok := LLMPresets[l.Preset]
	switch {
	case ok && l.Preset == LLMCloudflare:
		if l.AccountID != "" {
			baseURL = strings.ReplaceAll(p.BaseURL, "{account}", l.AccountID)
		}
	case ok:
		baseURL = p.BaseURL
	default:
		baseURL = l.BaseURL
	}
	fast, strong = l.FastModel, l.StrongModel
	if fast == "" {
		fast = p.FastModel
	}
	if strong == "" {
		strong = p.StrongModel
	}
	if fast == "" {
		fast = strong
	}
	if strong == "" {
		strong = fast
	}
	return baseURL, fast, strong
}

// Ready reports whether the LLM can be called: a server, a model and a key.
func (l LLMConfig) Ready() bool {
	base, fast, _ := l.Resolved()
	return base != "" && fast != "" && l.APIKey != ""
}

// CloudConfig holds the non-secret cloud-sync account parameters.
// None of these unlock anything without the password-derived keys —
// they identify the account and reproduce its KDF inputs. Secrets
// (session token, master key, recovery code) are never written here.
type CloudConfig struct {
	Email      string `yaml:"email"`
	AuthSalt   string `yaml:"auth_salt"`
	KDFParams  string `yaml:"kdf_params"`
	APIBaseURL string `yaml:"api_base_url"`
}

// SummaryConfig selects and configures the provider that writes meeting
// summaries, decisions and action items (internal/summarize).
type SummaryConfig struct {
	Provider    string `yaml:"provider"`     // "ollama" (default) | "catchm-cloud" | "openai" (= Config.LLM)
	OllamaModel string `yaml:"ollama_model"` // default "qwen3.5:9b"; host = translate.host
	// OpenAI is the pre-LLM-block location of the OpenAI-compatible
	// summary server. Load moves it into Config.LLM; never written again.
	OpenAI       OpenAICompatConfig `yaml:"openai,omitempty"`
	CloudConsent bool               `yaml:"cloud_consent"`
}

// OpenAICompatConfig points the "openai" summary provider at any
// OpenAI-compatible chat completions server.
type OpenAICompatConfig struct {
	BaseURL string `yaml:"base_url"` // e.g. https://openrouter.ai/api/v1
	Model   string `yaml:"model"`
	APIKey  string `yaml:"api_key"` // secret: never returned by GET
}

// Setup values: the provider set the user picked on first run
// (docs/plans/active/cloud-mode.md). SetupCloud hides self-hosted
// options in the UI, SetupSelfHost hides cloud providers, SetupCustom
// shows everything. "" means the user hasn't chosen yet (fresh install).
const (
	SetupCloud    = "cloud"
	SetupSelfHost = "selfhost"
	SetupCustom   = "custom"
)

// Translation providers (TranslateConfig.Provider).
const (
	TranslateOllama = "ollama"
	TranslateGoogle = "google"
	TranslateLLM    = "llm"
	TranslateNone   = "none"
)

// Config is CatchM's on-disk configuration.
type Config struct {
	// Setup is never omitted when saved: its absence is how Load tells a
	// config written before setups existed (→ SetupCustom, so nothing an
	// existing user sees changes) from a fresh one awaiting the chooser.
	Setup      string          `yaml:"setup"`
	Engine     string          `yaml:"engine"`
	Lang       string          `yaml:"lang"`
	UILanguage string          `yaml:"ui_language"` // "system" (default) | "en" | "vi"; UI text language, not the STT Lang
	WhisperURL string          `yaml:"whisper_url"`
	Soniox     SonioxConfig    `yaml:"soniox"`
	Translate  TranslateConfig `yaml:"translate"`
	Cloud      CloudConfig     `yaml:"cloud"`
	Summary    SummaryConfig   `yaml:"summary"`
	LLM        LLMConfig       `yaml:"llm"`
}

const (
	defaultEngine     = "whisper"
	defaultWhisperURL = "http://127.0.0.1:8080"
	defaultSonioxURL  = "wss://stt-rt.soniox.com/transcribe-websocket"
	defaultOllamaHost = "http://127.0.0.1:11434"
	defaultLiveModel  = "translategemma:4b"
	defaultBatchModel = "translategemma:12b"

	defaultUILanguage = "system"

	defaultSummaryProvider = "ollama"
	defaultSummaryModel    = "qwen3.5:9b"

	// defaultCloudURL is the deployed catchm-api Worker. Local dev
	// against `wrangler dev` still works: log out and POST a
	// cloud-server-setting change (internal/server/cloud.go's
	// handleCloudServer) to http://127.0.0.1:8787 or another loopback
	// address — validateCloudServerURL allows plain http for loopback.
	defaultCloudURL = "https://catchm-api.spacingmind.com"
)

func defaults() *Config {
	return &Config{
		Engine:     defaultEngine,
		UILanguage: defaultUILanguage,
		WhisperURL: defaultWhisperURL,
		Soniox:     SonioxConfig{URL: defaultSonioxURL},
		Translate: TranslateConfig{
			Provider:   TranslateOllama,
			Host:       defaultOllamaHost,
			LiveModel:  defaultLiveModel,
			BatchModel: defaultBatchModel,
		},
		Cloud: CloudConfig{APIBaseURL: defaultCloudURL},
		Summary: SummaryConfig{
			Provider:    defaultSummaryProvider,
			OllamaModel: defaultSummaryModel,
		},
	}
}

// Path returns the config file location: $CATCHM_HOME/config.yaml, or
// ~/.catchm/config.yaml when CATCHM_HOME is unset.
func Path() (string, error) {
	home := os.Getenv("CATCHM_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(userHome, ".catchm")
	}
	return filepath.Join(home, "config.yaml"), nil
}

// Load reads the config file, applying defaults for anything unset. A
// missing file is not an error: defaults apply as-is.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	c := defaults()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(raw, c); err != nil {
		return nil, err
	}
	if c.Setup == "" && !hasTopLevelKey(raw, "setup") {
		c.Setup = SetupCustom // written before setups existed
	}
	if c.Translate.Provider == "" {
		c.Translate.Provider = TranslateOllama
	}
	migrateSummaryOpenAI(c)
	c.Normalize()
	return c, nil
}

// Normalize applies rules that tie settings together. With Soniox
// transcribing, translation is always Soniox's own ("none"): the user's
// rule (2026-11-27), so a meeting never mixes two translators.
func (c *Config) Normalize() {
	if c.Engine == "soniox" {
		c.Translate.Provider = TranslateNone
	}
}

// migrateSummaryOpenAI moves a pre-LLM-block summary.openai server into
// the shared llm block (once: only while llm names no server). The
// settings UI only ever wrote Cloudflare Workers AI URLs there, which
// become the cloudflare preset; anything else becomes custom.
func migrateSummaryOpenAI(c *Config) {
	old := c.Summary.OpenAI
	c.Summary.OpenAI = OpenAICompatConfig{}
	if old.BaseURL == "" || c.LLM.Preset != "" || c.LLM.BaseURL != "" {
		return
	}
	c.LLM = LLMConfig{Preset: LLMCustom, BaseURL: old.BaseURL, APIKey: old.APIKey, FastModel: old.Model, StrongModel: old.Model}
	const cfPrefix = "https://api.cloudflare.com/client/v4/accounts/"
	if rest, ok := strings.CutPrefix(strings.TrimRight(old.BaseURL, "/"), cfPrefix); ok {
		if account, tail, ok := strings.Cut(rest, "/"); ok && tail == "ai/v1" {
			c.LLM.Preset, c.LLM.BaseURL, c.LLM.AccountID = LLMCloudflare, "", account
		}
	}
}

// hasTopLevelKey reports whether the YAML document maps key at its top
// level, even to an empty value.
func hasTopLevelKey(raw []byte, key string) bool {
	var top map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &top); err != nil {
		return false
	}
	_, ok := top[key]
	return ok
}

// Save writes cfg as YAML to the path Path() resolves, creating the
// parent directory (0o700) if needed. The file is written 0o600 because
// it contains the Soniox and summary API keys.
func Save(cfg *Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
