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
	if c.Engine != defaultEngine || c.WhisperURL != defaultWhisperURL || c.Soniox.URL != defaultSonioxURL {
		t.Fatalf("defaults not applied: %+v", c)
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
	// Unset fields still get their defaults.
	if c.WhisperURL != defaultWhisperURL || c.Soniox.URL != defaultSonioxURL {
		t.Fatalf("defaults not preserved alongside overrides: %+v", c)
	}
}
