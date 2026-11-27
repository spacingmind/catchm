package sysinfo

import "testing"

func f(v float64) *float64 { return &v }

func TestJudgeSelfHost(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    Machine
		want string
		has  string
	}{
		{"strong mac", Machine{Info: Info{TotalRAMGB: f(32), FreeDiskGB: f(200)}, CPUCores: 10, AppleSilicon: true}, "good", ""},
		{"8 GB", Machine{Info: Info{TotalRAMGB: f(8), FreeDiskGB: f(200)}, CPUCores: 8, AppleSilicon: true}, "slow", "ram_minimum"},
		{"4 GB", Machine{Info: Info{TotalRAMGB: f(4), FreeDiskGB: f(200)}, CPUCores: 8, AppleSilicon: true}, "insufficient", "ram_low"},
		{"full disk", Machine{Info: Info{TotalRAMGB: f(32), FreeDiskGB: f(5)}, CPUCores: 8, AppleSilicon: true}, "insufficient", "disk_low"},
		{"windows pc", Machine{Info: Info{TotalRAMGB: f(16), FreeDiskGB: f(100)}, CPUCores: 8}, "good", "gpu_unknown"},
		{"2 cores", Machine{Info: Info{TotalRAMGB: f(16), FreeDiskGB: f(100)}, CPUCores: 2, AppleSilicon: true}, "slow", "cpu_few"},
	} {
		got := judge(tc.m)
		if got.SelfHost != tc.want {
			t.Errorf("%s: %s, want %s (%v)", tc.name, got.SelfHost, tc.want, got.Reasons)
		}
		if tc.has != "" {
			found := false
			for _, r := range got.Reasons {
				found = found || r == tc.has
			}
			if !found {
				t.Errorf("%s: reasons %v missing %s", tc.name, got.Reasons, tc.has)
			}
		}
	}
}
