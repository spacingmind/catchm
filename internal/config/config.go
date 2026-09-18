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

// Config is CatchM's on-disk configuration.
type Config struct {
	Engine     string       `yaml:"engine"`
	Lang       string       `yaml:"lang"`
	WhisperURL string       `yaml:"whisper_url"`
	Soniox     SonioxConfig `yaml:"soniox"`
}

const (
	defaultEngine     = "whisper"
	defaultWhisperURL = "http://127.0.0.1:8080"
	defaultSonioxURL  = "wss://api.soniox.com/transcribe-websocket"
)

func defaults() *Config {
	return &Config{
		Engine:     defaultEngine,
		WhisperURL: defaultWhisperURL,
		Soniox:     SonioxConfig{URL: defaultSonioxURL},
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
