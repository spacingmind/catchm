# macOS release (Apple Silicon, unsigned)

## Problem

CatchM ships only a Windows installer. `release.yml` runs on
`windows-latest`, and `cmd/catchm-desktop/build/` has only `windows/`:
there's no `darwin/Info.plist`, so a packaged Mac app has no
`NSMicrophoneUsageDescription`. macOS kills an app that opens the mic
without one. Updates are Windows-only too (`update_other.go`:
"not supported on this OS"). So Mac users have nothing to install, and
M25 (macOS system audio) would have no users.

Reference: `refs/my-translator` ships notarized Apple Silicon + Intel
`.dmg` builds (`scripts/build-notarized.sh`, `src-tauri/Entitlements.plist`,
`Info.plist` usage strings).

## Decisions (user, 2026-10-03)

- **Unsigned for now.** Ad-hoc signed (`codesign -s -`, which Apple
  Silicon requires to run at all); no Developer ID and no notarization.
  First launch needs the Gatekeeper bypass, documented for users.
- **Apple Silicon only** (`darwin/arm64`).
- **Updates on Mac: notify + open the download.** The app checks the
  same manifest and shows the update banner. "Download" opens the
  `.dmg` URL in the browser. No in-place `.app` replacement in this plan.
- **Manifest: add assets, keep the old ones.** `latest.json` keeps its
  shape and its Windows assets, so installed Windows v0.4.0 clients keep
  working. The `.dmg` and its `.sha256` join `assets`. Each client picks
  the asset for its OS by name.

## Design

### Build (`cmd/catchm-desktop/build/darwin/`)

- `Info.plist` (and Wails' `Info.dev.plist`): bundle id, version from
  `wails.json`, `LSMinimumSystemVersion` 13.0, `NSMicrophoneUsageDescription`.
  `NSAudioCaptureUsageDescription` is added by M25 when taps land.
- Taskfile `desktop:release:darwin VERSION=… UPDATE_BASE_URL=…`: `wails
  build -platform darwin/arm64` with the same ldflags as Windows (version,
  update base URL); `codesign --force --deep -s -` on the `.app`;
  `hdiutil create -volname CatchM -srcfolder … -format UDZO` →
  `catchm-desktop-arm64.dmg`; a `sha256sum`-format `.sha256`. Same
  version precondition as `desktop:release` (plain x.y.z).

### CI (`release.yml`)

- Split into jobs: `windows` (today's job, unchanged steps) and `macos`
  (`runs-on: macos-14`, arm64). Both upload their artifacts.
- A `publish` job (needs both) does today's publish steps: GitHub
  release with all four assets, then `latest.json` with all four in
  `assets`, then R2 upload with `latest.json` last. A dry run builds
  both platforms and publishes nothing.
- If the macOS build fails, nothing is published, so a release is
  all-or-nothing.

### catchm-api Worker (repo `spacingmind/catchm-cloud`)

- `GET /update/download/:tag/:name` allowlists exactly two names
  (M24). Add `catchm-desktop-arm64.dmg` and its `.sha256`. **Deploy
  this before the first release that contains them**, or Mac downloads
  404.
- `/update/releases/latest` needs no change: it already decorates every
  asset in `latest.json` with a `browser_download_url`.

### Client

- `internal/update`: asset names by OS (`windows` → the installer
  `.exe`, `darwin` → the `.dmg`), replacing the hard-coded installer
  name in `checksum.go`. The Windows path is unchanged.
- `cmd/catchm-desktop/update_other.go` → darwin: "install" = open the
  asset's `browser_download_url` with `runtime.BrowserOpenURL`; no
  babysitter or relaunch. The update banner / Settings → System show
  "Download" instead of "Install & restart" when the platform can't
  self-install. A new capability flag comes from the bridge; strings go
  through i18n.

### Docs

- README / docs: Mac install steps for an unsigned app (open the `.dmg`,
  drag to Applications, then first launch via right-click → Open, or on
  macOS 15+ System Settings → Privacy & Security → Open Anyway). Also:
  the mic permission may be asked again after each update, because the
  ad-hoc signature changes.
- CONTRIBUTING release runbook: the macOS job, and the Worker allowlist
  deploy order.

## Out of scope

- Developer ID signing + notarization (revisit when there's an Apple
  Developer account; then in-place auto-update becomes viable).
- Intel / universal builds.
- M25 system audio (separate plan; this plan only makes its plist key
  possible).

## Acceptance criteria

- `release.yml` dry run builds both the Windows installer and
  `catchm-desktop-arm64.dmg` (+ `.sha256`); publishes nothing.
- The `.dmg` mounts; the `.app` launches on an Apple Silicon Mac after
  the documented Gatekeeper bypass; starting a meeting prompts for mic
  access with the plist string and records.
- `latest.json` for a release lists all four assets. A Windows v0.4.0
  client still finds its installer and updates (checked against the
  generated manifest in a unit test, and once for real).
- Worker: download allowlist accepts the two new names, still rejects
  anything else (Worker tests), deployed before the release.
- A Mac build on an older version shows the update banner. "Download"
  opens the `.dmg` URL, and checksum and name selection are covered by
  `internal/update` tests.
- `go test ./...`, vet, gofmt, web build/lint green.
