package sysinfo

import "runtime"

// Machine is GET /api/system: the host facts plus a verdict on running
// everything locally (Whisper + Ollama), shown by the Self-host setup.
type Machine struct {
	Info
	CPUCores     int      `json:"cpu_cores"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	AppleSilicon bool     `json:"apple_silicon"`
	SelfHost     string   `json:"selfhost"`         // "good" | "slow" | "insufficient"
	Reasons      []string `json:"selfhost_reasons"` // codes the UI words; most serious first
}

// Self-host thresholds. Whisper's model (~1.5 GB) plus a translation
// model (3–8 GB) and the summary model (~6 GB) want ~15 GB of disk and
// enough RAM to hold one model at a time next to the meeting apps.
const (
	minRAMGB     = 8
	goodRAMGB    = 16
	minDiskGB    = 12
	comfyDiskGB  = 20
	slowCPUCores = 4
)

// ReadMachine reads the host and judges it for self-hosting.
func ReadMachine() Machine {
	return judge(Machine{
		Info:         Read(),
		CPUCores:     runtime.NumCPU(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		AppleSilicon: runtime.GOOS == "darwin" && runtime.GOARCH == "arm64",
	})
}

func judge(m Machine) Machine {
	level := 0 // 0 good, 1 slow, 2 insufficient
	bump := func(l int, reason string) {
		if l > level {
			level = l
		}
		m.Reasons = append(m.Reasons, reason)
	}
	if r := m.TotalRAMGB; r != nil {
		switch {
		case *r < minRAMGB:
			bump(2, "ram_low")
		case *r < goodRAMGB:
			bump(1, "ram_minimum")
		}
	} else {
		bump(1, "ram_unknown")
	}
	if d := m.FreeDiskGB; d != nil {
		switch {
		case *d < minDiskGB:
			bump(2, "disk_low")
		case *d < comfyDiskGB:
			bump(1, "disk_tight")
		}
	}
	if m.CPUCores > 0 && m.CPUCores < slowCPUCores {
		bump(1, "cpu_few")
	}
	if !m.AppleSilicon {
		// Can't see the graphics card from here; without one, local
		// summaries take minutes. Advisory only.
		m.Reasons = append(m.Reasons, "gpu_unknown")
	}
	m.SelfHost = [...]string{"good", "slow", "insufficient"}[level]
	if m.Reasons == nil {
		m.Reasons = []string{}
	}
	return m
}
