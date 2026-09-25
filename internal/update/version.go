// Package update implements CatchM's self-update against GitHub
// Releases: comparing the running version to the latest tag, and
// downloading + SHA-256-verifying the NSIS installer before anything
// executes it. Pure Go — no Wails, no Windows API — so all of it is
// unit-testable on any OS (the security boundary lives here: with no
// code signing, the published checksum is the ONLY integrity check
// between "bytes arrived over the network" and "elevated process runs
// them").
package update

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseVersion parses "v1.2.3" / "1.2.3" into comparable components.
// Pre-release suffixes ("1.2.3-beta") are accepted and ignored for
// ordering — tags here are plain x.y.z today (the release pipeline
// rejects anything else), so the suffix only needs to round-trip, not
// satisfy full semver precedence rules.
func ParseVersion(s string) ([3]int, error) {
	var v [3]int
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("version %q: want x.y.z", s)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || (len(p) > 1 && p[0] == '0') {
			return v, fmt.Errorf("version %q: part %q is not a plain number", s, p)
		}
		v[i] = n
	}
	return v, nil
}

// IsNewer reports whether latest > current. "dev" (a local build never
// stamped by ldflags) and any unparsable current version are treated as
// unknown age — never older — so a dev build is never nagged about
// being behind. An unparsable *latest* tag is also false: a malformed
// release on GitHub should not drive an update prompt.
func IsNewer(current, latest string) bool {
	if current == "dev" {
		return false
	}
	cur, err := ParseVersion(current)
	if err != nil {
		return false
	}
	lat, err := ParseVersion(latest)
	if err != nil {
		return false
	}
	for i := range cur {
		if lat[i] != cur[i] {
			return lat[i] > cur[i]
		}
	}
	return false
}
