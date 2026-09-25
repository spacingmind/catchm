// version.go holds the app's version. The default "dev" is never
// stamped by ldflags outside real release builds (Taskfile's
// desktop:release / release.yml) — Dev is deliberately treated as
// "unknown age" by internal/update.IsNewer so a locally-built dev
// binary never sees itself as older than a release and nags about
// updates.
package main

// Set via -ldflags "-X main.version=..." at release-build time, from
// the git tag (Taskfile desktop:release, .github/workflows/release.yml).
var version = "dev"

// AppVersion returns the running version ("dev" for local builds).
func AppVersion() string { return version }
