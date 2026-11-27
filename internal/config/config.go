// Package config loads CatchM's small YAML config file: STT engine
// defaults, the Soniox API key, and the summary provider settings.
package config

import (
	"os"
	"path/filepath"

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
	// before it existed) or "none" (only the engine's own translation, e.g.
	// Soniox). Cloud translation providers join in a later phase
	// (docs/plans/active/cloud-mode.md).
	Provider   string `yaml:"provider"`
	Host       string `yaml:"host"`
	LiveModel  string `yaml:"live_model"`
	BatchModel string `yaml:"batch_model"`
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
	Provider     string             `yaml:"provider"`     // "ollama" (default) | "catchm-cloud" | "openai"
	OllamaModel  string             `yaml:"ollama_model"` // default "qwen3.5:9b"; host = translate.host
	OpenAI       OpenAICompatConfig `yaml:"openai"`
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
	return c, nil
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
