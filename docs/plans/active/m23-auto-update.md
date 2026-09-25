# M23: Auto-update (self-built, no code signing)

## Decision context

- **Windows code signing: explicitly skipped.** ZCode and Paseo (both OSS Electron
  apps researched for this decision) ship Windows builds unsigned too and rely on
  users clicking through the SmartScreen "Run anyway" prompt; only macOS is signed
  in both, because Gatekeeper has no easy bypass. CatchM has no macOS build today,
  so signing isn't needed at all right now. Consequence for this milestone: every
  release's installer hash is new, so **SmartScreen reputation never accumulates
  release-to-release** — this is expected, not a bug to chase.
- **No signing also means no code-signature integrity check.** The only integrity
  control available is a **SHA-256 checksum published alongside the release**,
  fetched over HTTPS from GitHub. Verify it before ever executing a downloaded
  installer. This is not optional — skipping it means silently executing whatever
  bytes arrived over the wire.
- **Library evaluation (already done, don't redo):** `wails-kit`'s `updates`
  package was rejected (1 GitHub star, single maintainer, days old — too
  unproven for something that replaces the running binary). Velopack was
  rejected too — mature (2.3k stars) but no real Go SDK (only a CLI you'd shell
  out to) and it uses its own installer format, not NSIS, so adopting it means
  throwing away the NSIS work from M22 (protocol registration, AUMID shortcut,
  single-instance). **Decision: build it ourselves**, thin and testable.
- **Existing install is per-machine, not per-user.** `project.nsi` has no
  `WAILS_INSTALL_SCOPE` override anywhere in this repo, so it defaults to
  `$PROGRAMFILES64\spacingmind\CatchM` — confirmed to match the user's real
  installed copy. A per-machine install means **the update installer needs
  admin elevation to run**, so a silent, invisible background update is not
  possible without an elevation prompt. Given that plus the lack of code
  signing, **don't try to hide the update from the user** — surface it, let
  them confirm, accept the UAC prompt when it appears. This shapes the UI
  design below (a confirmed action, not a silent background swap).

## Module boundaries (per user's request — keep these separate, no cross-cutting)

```
internal/update/              (pure Go, no Wails/Windows import, fully unit-testable)
  client.go       — GitHubClient: fetch latest release from the GitHub Releases
                     API (spacingmind/catchm), injectable *http.Client + base URL
                     so tests hit a local httptest.Server, never the real network.
  version.go       — semver parse + compare (current running version vs latest
                     tag). No third-party semver lib needed for x.y.z tags —
                     keep it small, but write real tests for edge cases (v-prefix,
                     equal versions, pre-release suffixes if we ever use them).
  checksum.go      — download the asset to a temp file via streaming io.Copy
                     (don't buffer the whole installer in memory), then verify
                     its SHA-256 against the checksum published in the same
                     release (see release pipeline below) before returning a
                     "verified" path. Return a distinct error type for mismatch
                     vs. network failure vs. missing checksum asset — the
                     caller needs to tell these apart (mismatch = don't ever
                     run this file, whereas a flaky download can just retry).
  update.go        — ties the three together: CheckForUpdate() (Client +
                     version.go) and DownloadAndVerify() (checksum.go),
                     returning small plain structs (UpdateInfo{Version,
                     Notes, PublishedAt}, VerifiedInstaller{Path, SHA256}) —
                     no Wails types leak into this package.

cmd/catchm-desktop/update.go   (Wails-bound orchestration — the only file that
                                 knows about runtime.EventsEmit / bound methods)
  - Wraps internal/update with the app's actual version (see "version plumbing"
    below), exposes bound methods the frontend calls: CheckForUpdate(),
    DownloadUpdate(), ApplyUpdate(). Emits progress events during download
    (reuse the pattern from M19's level-meter polling — don't invent a new
    event mechanism).
  - Background check: once on startup (a few seconds after OnStartup, not
    blocking window show) and once per ~24h while running. Bound methods let
    the frontend trigger a manual check too — same code path, no duplication.
  - Owns the actual process-launch step: given a VerifiedInstaller, run it
    with the NSIS silent flag and quit the app so the installer can replace
    files. Research whether this repo's NSIS build needs `/S` alone or also
    needs to relaunch the app after — the current project.nsi has no
    `.onInstSuccess` relaunch hook (checked: only has `.onInit` with the
    architecture check macro), so after a silent update the app does NOT
    restart itself unless you add that. Decide: either add a minimal
    `.onInstSuccess` section that relaunches the installed exe, or have
    ApplyUpdate() itself wait for the installer process to exit and then
    launch the new exe from Go. Pick whichever is less NSIS-macro-fragile;
    document the choice in a comment, don't leave it implicit.
  - Elevation: launching the per-machine installer needs UAC. Use
    `ShellExecute` with the `runas` verb (there's already a
    `golang.org/x/sys/windows` dependency in this repo from M19/M22 — check
    before adding a new one) so Windows shows its own native elevation
    prompt. If the user cancels UAC, treat that as "update deferred", not an
    error state — no retry loop, no repeated nagging in the same session.

web/src (frontend)
  - A Settings section (or a slot in the existing Settings page structure —
    match M20's page layout, don't invent a new nav item) showing current
    version, "Check for updates" button, and last-checked time.
  - An unobtrusive banner/notification when a background check finds an
    update (similar treatment to the capture-error banner from M20/M21 —
    reuse that visual pattern rather than a new component), with "View
    details" (release notes link) and "Update now" actions. "Update now"
    shows a confirmation (mentions the app will quit, and Windows may ask
    for permission) before calling ApplyUpdate() — no surprise app-quit.

.github/workflows/release.yml   (new — doesn't exist yet, checked: only ci.yml
                                  exists in this repo currently)
  - Triggered on `v*` tag push (this repo doesn't tag releases at all today —
    confirm with the user before this milestone ships whether `develop` merges
    should also get tagged, or whether tagging is a separate manual step).
  - Builds the NSIS installer (reuse the Taskfile target already used for
    local builds, don't duplicate its logic in YAML).
  - Computes SHA-256 of the installer and writes a checksum file (plain
    `sha256sum`-compatible format) as a second release asset.
  - Publishes both as a GitHub Release via `gh release create` or the
    equivalent action — check what auth scope is available in this repo's
    Actions (GITHUB_TOKEN should suffice for creating releases in the same
    repo, no new secret needed).

Version plumbing (currently missing entirely — checked: no ldflags, no
runtime/debug.ReadBuildInfo usage, no version constant anywhere in
cmd/catchm-desktop; wails.json's "productVersion": "0.1.0" is build metadata
only, never read at runtime)
  - Add a `var version = "dev"` in cmd/catchm-desktop/main.go (or a small
    dedicated file), set via `-ldflags "-X main.version=..."` in the release
    build step, sourced from the git tag. Wire this into internal/update's
    CheckForUpdate() comparison and expose it to the frontend (a bound
    getter, and/or embed it in the existing settings/about surface if one
    exists).
```

## Explicitly out of scope for this milestone

- Delta/differential updates (ship the full installer every time — fine at
  this app's size).
- Auto-installing without any user confirmation (ruled out above: no signing
  + per-machine elevation makes a truly silent update both risky and
  technically blocked by UAC anyway).
- Rollback/downgrade UI (if a bad release ships, the fix is a new release).

## Validation

- Unit tests for `internal/update`: version compare edge cases, checksum
  mismatch is rejected (construct a deliberately-corrupted test fixture),
  GitHub API client against `httptest.Server` fixtures (a real "latest
  release" JSON shape, a 404/no-releases-yet case, a malformed-JSON case).
- Real Windows test (same machine/pattern as M19-M22): build two versions
  differing only in the embedded version string, publish the "newer" one's
  installer+checksum to a throwaway location (not the real GitHub release —
  point `GitHubClient`'s base URL at a local test server or a scratch repo),
  run the older build, confirm check → banner → confirm → UAC prompt appears
  → app quits → installer runs → new version launches. Confirm a checksum
  *mismatch* case is rejected and never executed (test this deliberately,
  don't just assume the code path is unreachable).
- No mic/audio testing needed for this milestone — don't start real
  meetings during this work.
