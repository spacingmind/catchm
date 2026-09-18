// Package config loads CatchM's small YAML config file: STT engine
// defaults and the Soniox API key.
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
	Host       string `yaml:"host"`
	LiveModel  string `yaml:"live_model"`
	BatchModel string `yaml:"batch_model"`
}

// Config is CatchM's on-disk configuration.
type Config struct {
	Engine     string          `yaml:"engine"`
	Lang       string          `yaml:"lang"`
	WhisperURL string          `yaml:"whisper_url"`
	Soniox     SonioxConfig    `yaml:"soniox"`
	Translate  TranslateConfig `yaml:"translate"`
}

const (
	defaultEngine     = "whisper"
	defaultWhisperURL = "http://127.0.0.1:8080"
	defaultSonioxURL  = "wss://stt-rt.soniox.com/transcribe-websocket"
	defaultOllamaHost = "http://127.0.0.1:11434"
	defaultLiveModel  = "translategemma:4b"
	defaultBatchModel = "translategemma:12b"
)

func defaults() *Config {
	return &Config{
		Engine:     defaultEngine,
		WhisperURL: defaultWhisperURL,
		Soniox:     SonioxConfig{URL: defaultSonioxURL},
		Translate: TranslateConfig{
			Host:       defaultOllamaHost,
			LiveModel:  defaultLiveModel,
			BatchModel: defaultBatchModel,
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
	return c, nil
}

// Save writes cfg as YAML to the path Path() resolves, creating the
// parent directory (0o700) if needed. The file is written 0o600 because
// it contains the Soniox API key.
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
