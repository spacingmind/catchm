// Package sysinfo reads the host facts the setup UI uses to recommend a
// local summary model: total physical memory and free disk space where
// Ollama keeps its models. It only uses golang.org/x/sys and OS APIs.
package sysinfo

import (
	"math"
	"os"
	"path/filepath"
)

// Info is the "system" block of GET /api/summary/status. Sizes are GiB
// rounded to one decimal; a field is nil (omitted) when it can't be read.
type Info struct {
	TotalRAMGB *float64 `json:"total_ram_gb,omitempty"`
	FreeDiskGB *float64 `json:"free_disk_gb,omitempty"`
}

// Read reports total RAM and free space on the Ollama models volume.
func Read() Info {
	return read(totalRAM, freeDisk, OllamaModelsDir())
}

// read is Read with injectable readers; dir "" means unknown.
func read(ram func() (uint64, error), disk func(string) (uint64, error), dir string) Info {
	var info Info
	if n, err := ram(); err == nil && n > 0 {
		info.TotalRAMGB = gb(n)
	}
	if dir != "" {
		if n, err := disk(dir); err == nil {
			info.FreeDiskGB = gb(n)
		}
	}
	return info
}

func gb(bytes uint64) *float64 {
	v := math.Round(float64(bytes)/(1<<30)*10) / 10
	return &v
}

// OllamaModelsDir returns the nearest existing directory on the path
// where Ollama stores models: $OLLAMA_MODELS if set, else
// ~/.ollama/models, walking up to an existing ancestor; failing that the
// home directory. It returns "" when none of these can be resolved.
func OllamaModelsDir() string {
	home, _ := os.UserHomeDir()
	want := os.Getenv("OLLAMA_MODELS")
	if want == "" && home != "" {
		want = filepath.Join(home, ".ollama", "models")
	}
	if want != "" {
		if abs, err := filepath.Abs(want); err == nil {
			if dir := existingAncestor(abs); dir != "" {
				return dir
			}
		}
	}
	return home
}

// existingAncestor returns p or its nearest ancestor that is a
// directory, or "" if there is none.
func existingAncestor(p string) string {
	for {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return ""
		}
		p = parent
	}
}
