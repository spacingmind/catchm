package sysinfo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadSaneOnThisMachine(t *testing.T) {
	info := Read()
	if info.TotalRAMGB == nil || *info.TotalRAMGB < 0.1 || *info.TotalRAMGB > 1<<16 {
		t.Fatalf("total_ram_gb = %v", info.TotalRAMGB)
	}
	if info.FreeDiskGB == nil || *info.FreeDiskGB < 0 {
		t.Fatalf("free_disk_gb = %v", info.FreeDiskGB)
	}
}

func TestReadOmitsUnreadableFields(t *testing.T) {
	fail := errors.New("boom")
	info := read(func() (uint64, error) { return 0, fail }, func(string) (uint64, error) { return 0, fail }, "/")
	if info.TotalRAMGB != nil || info.FreeDiskGB != nil {
		t.Fatalf("failing readers: %+v", info)
	}

	info = read(func() (uint64, error) { return 16 << 30, nil }, func(string) (uint64, error) { return 1, nil }, "")
	if info.TotalRAMGB == nil || *info.TotalRAMGB != 16 || info.FreeDiskGB != nil {
		t.Fatalf("unknown dir should omit free_disk_gb: %+v", info)
	}
}

func TestReadRoundsToOneDecimal(t *testing.T) {
	info := read(func() (uint64, error) { return 16_700_000_000, nil }, func(string) (uint64, error) { return 1 << 29, nil }, "/")
	if *info.TotalRAMGB != 15.6 || *info.FreeDiskGB != 0.5 {
		t.Fatalf("rounding: ram=%v disk=%v", *info.TotalRAMGB, *info.FreeDiskGB)
	}
}

func TestOllamaModelsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	t.Setenv("OLLAMA_MODELS", "")
	if got := OllamaModelsDir(); got != home {
		t.Fatalf("no ~/.ollama: got %q, want home %q", got, home)
	}

	models := filepath.Join(home, ".ollama", "models")
	if err := os.MkdirAll(models, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := OllamaModelsDir(); got != models {
		t.Fatalf("default: got %q, want %q", got, models)
	}

	custom := t.TempDir()
	t.Setenv("OLLAMA_MODELS", filepath.Join(custom, "not", "yet"))
	if got := OllamaModelsDir(); got != custom {
		t.Fatalf("OLLAMA_MODELS walk-up: got %q, want %q", got, custom)
	}
}
