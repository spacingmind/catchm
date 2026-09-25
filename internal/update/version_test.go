package update

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in      string
		want    [3]int
		wantErr bool
	}{
		{"1.2.3", [3]int{1, 2, 3}, false},
		{"v1.2.3", [3]int{1, 2, 3}, false},
		{"10.20.30", [3]int{10, 20, 30}, false},
		{"1.2.3-beta.1", [3]int{1, 2, 3}, false}, // suffix tolerated, ignored
		{"v1.2.3+build.5", [3]int{1, 2, 3}, false},
		{"1.2", [3]int{}, true},
		{"1.2.3.4", [3]int{}, true},
		{"1.2.x", [3]int{}, true},
		{"01.2.3", [3]int{}, true}, // leading zero: not canonical
		{"", [3]int{}, true},
		{"dev", [3]int{}, true},
	}
	for _, tc := range cases {
		got, err := ParseVersion(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseVersion(%q) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseVersion(%q) error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseVersion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		// Major/minor/patch ordering, each direction.
		{"1.2.3", "1.2.4", true},
		{"1.2.3", "1.3.0", true},
		{"1.2.3", "2.0.0", true},
		{"1.2.3", "v1.2.4", true},  // v-prefixed tag
		{"v1.2.3", "1.2.3", false}, // equal
		{"1.2.4", "1.2.3", false},  // older latest
		{"2.0.0", "1.9.9", false},
		{"1.2.3", "1.2.3-beta", false}, // equal modulo ignored suffix
		// Multi-component ordering must not be lexicographic.
		{"1.9.0", "1.10.0", true},
		// Dev builds are never "behind".
		{"dev", "99.0.0", false},
		// Unparsable inputs never drive an update prompt.
		{"1.2.3", "not-a-version", false},
		{"garbage", "1.2.3", false},
	}
	for _, tc := range cases {
		if got := IsNewer(tc.current, tc.latest); got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}
