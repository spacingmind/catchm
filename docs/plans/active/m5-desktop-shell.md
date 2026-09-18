# Plan: M5 — desktop shell (Wails, minimal native window)

- Status: verified on Windows (see Validation)
- Created: 2026-09-18

## Goal

CatchM currently requires running `catchm serve` and opening a browser
at `localhost:4649`. This milestone adds a second binary that opens a
native desktop window showing the exact same UI, with **zero
duplication** of the web frontend or the API/handler code: the same
`internal/server.New(...)` `http.Handler` that `cmd/catchm` uses is
reused as the window's content source. Scope, per the decision already
made: a minimal native window only — no system tray, no auto-update, no
menu bar customization. Those are explicit follow-ups, not this
milestone.

## Why this is a clean fit for the current architecture

M4's web UI is already a static SPA (`web/dist`) served by `internal/
server`'s embedded assets alongside the JSON API — nothing about it
assumes a network round-trip to a *remote* server, just an HTTP
`Handler`. Wails v2's `AssetServer` option
(`github.com/wailsapp/wails/v2/pkg/options/assetserver`) accepts an
arbitrary `http.Handler` to serve the window's content — point it at
`server.New(tools.New(store), cfg)`, the same value `cmd/catchm`'s
`serve()` already constructs, and the native window renders the exact
same app with the exact same API, no TCP port opened, no CORS, no code
fork.

## Scope

- New binary `cmd/catchm-desktop/main.go`: constructs a `meeting.Store`
  and `config.Config` the same way `cmd/catchm/main.go` does (read that
  file first and mirror its setup — `CATCHM_HOME` resolution, store
  path, config loading), builds `server.New(...)`, and calls
  `wails.Run(&options.App{...})` with:
  - `Title: "CatchM"`
  - `Width`/`Height` reasonable defaults (e.g. 1180×760)
  - `AssetServer: &assetserver.Options{Handler: <the server.Server>}`
  - No `Bind` structs needed — the frontend already talks to the app
    exclusively over the HTTP API baked into that handler, so there is
    no separate Go<->JS binding surface to wire up in this milestone.
- Wails project scaffolding: run `go install
  github.com/wailsapp/wails/v2/cmd/wails@latest` (or whatever the
  current stable v2 tag is) and use it to scaffold the minimum Wails
  project metadata needed (`wails.json`, `build/` platform assets) for
  `cmd/catchm-desktop`. Since the actual frontend is already built and
  embedded via `internal/server` (not through Wails' own frontend
  pipeline), the `wails.json` frontend build commands can be no-ops
  (empty strings) or can point at `../web` for consistency — pick
  whichever the `wails` CLI tolerates with the least friction and
  record which in Decisions. Do not create a second, duplicate frontend
  under a Wails-generated `frontend/` directory.
- System dependencies: building a Wails v2 app on Linux requires
  `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` (or `4.0-dev` on older
  distros) plus `pkg-config`, because Wails uses cgo bindings to the
  system webview. **Try** `sudo apt-get update && sudo apt-get install
  -y libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config` (or the `4.0-dev`
  package name if `4.1-dev` isn't available in this environment's apt
  sources). **If this fails** (no sudo access, no network to apt
  mirrors, package not found), that is an acceptable, expected outcome
  in a sandboxed environment — do not treat it as a milestone failure.
  In that case: write all the Go source and Wails config correctly
  (reviewed carefully against Wails v2's documented API/examples from
  your own knowledge, since you won't be able to compile-check it),
  commit it, and clearly document in the plan doc's Validation section
  exactly what remains unverified and what a developer with proper
  system packages needs to run to finish verifying it (`sudo apt-get
  install ...` + `wails build`). Do not fabricate a fake "build
  succeeded" result.
- `Taskfile.yml`: add `desktop:dev` (`wails dev` from `cmd/catchm-
  desktop`) and `desktop:build` (`wails build`), following whatever
  working directory / `-d` flag convention `wails` expects. Don't wire
  these into the default `build`/`test` tasks — the desktop binary is
  additive, opt-in, and may not build in every environment.
- `AGENTS.md`/`README.md` (whichever already documents how to build/run
  the project): add a short note on the new binary and its system
  dependency, so this isn't a surprise to the next person.

Out of scope (explicitly, per the scoping decision already made): system
tray icon, auto-launch on login, native menu bar, auto-updater, code
signing/notarization, Windows/macOS builds or packaging (cgo means no
cross-compiling from this Linux sandbox regardless — those platforms'
builds happen on their own OS or via CI later, not in this run).

## Acceptance criteria

1. `cmd/catchm-desktop/main.go` compiles *if* the system webview
   dependencies are present; if they are not (expected in this sandbox),
   `go vet`/`gofmt` still pass on the new Go source (those don't require
   cgo linking the same way `go build` does — confirm this holds; if
   `go vet` also needs the cgo headers, fall back to careful manual
   review against Wails v2's real API surface and say so plainly in
   Validation).
2. The rest of the existing Go module (`cmd/catchm`, all `internal/...`
   packages) is completely unaffected: `task build && task test && task
   lint` for the existing (non-desktop) code path must still pass
   exactly as before — the new binary is additive, isolated in its own
   `cmd/catchm-desktop` package so a cgo-dependency failure there cannot
   break the rest of the module's `go build ./...`. (Go modules compile
   per-package/per-binary; confirm `go build ./...` behavior when one
   `cmd/` package needs cgo headers that aren't present — if it makes
   the whole `go build ./...` fail rather than just that one package,
   note that tension and how you resolved it, e.g. a build tag.)
3. Plan doc's Decisions/Progress/Validation sections reflect exactly
   what was and wasn't verified, honestly.

## Decisions

- **Wails version**: `github.com/wailsapp/wails/v2 v2.16.0`, installed via
  `go install github.com/wailsapp/wails/v2/cmd/wails@latest` (resolved to
  that tag). Added as a direct dependency in `go.mod`.
- **System webview deps**: attempted, did not succeed — see Validation.
  The Go source in `cmd/catchm-desktop/main.go` was written and verified
  against the actual `wailsapp/wails/v2@v2.16.0` source in the local
  module cache (not just from memory): `wails.Run(*options.App)` in
  `wails.go`, `options.App{Title, Width, Height, AssetServer}` in
  `pkg/options/options.go`, and `assetserver.Options{Handler http.Handler}`
  in `pkg/options/assetserver/options.go` were all read directly to
  confirm field names/types before writing the call site. High confidence
  this is correct, independent of the inability to link the cgo webview
  binary in this sandbox.
- **`wails.json` frontend commands**: set `"frontend:dir": "../../web"`
  (pointing at the real, already-built `web/` — not a new Wails-generated
  `frontend/`), with `"frontend:install"` and `"frontend:build"` both
  `""`. This was not an arbitrary pick: `BaseBuilder.BuildFrontend` in
  `pkg/commands/build/base.go` hard-fails with `frontend directory '...'
  does not exist` if `frontend:dir` doesn't resolve to a real directory,
  *even when* install/build commands are empty — so pointing at `../../web`
  (which exists and is already built by `task web:build`) was the only
  way to satisfy that check without creating a duplicate frontend
  directory. Empty install/build commands mean `wails build`/`wails dev`
  never touch `web/` themselves (confirmed in that same file: empty
  string short-circuits to "Skipping."). Also set `"wailsjsdir":
  "bindings"` (local to `cmd/catchm-desktop`, gitignored) so that even if
  `-skipbindings` is ever omitted, generated JS bindings can't land
  inside `web/`. `Taskfile.yml`'s `desktop:dev`/`desktop:build` tasks
  always pass `-s -skipbindings` regardless, since there's no Go<->JS
  bind surface in this milestone.
- **`go build ./...` and the cgo dependency**: no build tag was needed.
  Confirmed by reading `internal/app/app_default_unix.go` in the Wails
  source: `wails.Run` dispatches through `pkg/application` ->
  `internal/app.CreateApp`, which has three build-tag-gated
  implementations (`app_production.go` under `-tags production`,
  `app_dev.go` under `-tags dev`, and a no-op stub under `!dev &&
  !production && !bindings` that just returns an error at runtime). Only
  the `production`/`dev`-tagged files import
  `internal/frontend/desktop`, which is what pulls in the cgo
  `#cgo pkg-config: gtk+-3.0` / `webkit2gtk-4.0`/`4.1` directives. Plain
  `go build` (no tags) — which is what `go build ./...` and `task build`
  use — compiles the stub, so it links successfully with **no cgo/gtk/
  webkit dependency at all**. Only `wails build`/`wails dev` (which set
  `-tags production`/`-tags dev` themselves) actually pull in the cgo
  path. This was verified empirically, not just reasoned about: `go build
  ./...` from repo root succeeds today with the wails dependency in
  go.mod. The plain-`go build` binary is not a working desktop app (its
  stub `CreateApp` just returns an error at runtime) — it's only
  meaningful via `wails build`/`wails dev`, which is the documented,
  opt-in path.

## Progress

- [x] Wails CLI installed / attempted — succeeded (`go install
      github.com/wailsapp/wails/v2/cmd/wails@latest`, resolved to v2.16.0)
- [x] System webview dependencies installed / attempted (document outcome)
      — attempted, did not succeed (no passwordless sudo in this sandbox);
      see Validation for the exact failure and what's needed to finish it
- [x] cmd/catchm-desktop/main.go written (AssetServer reusing internal/server.New)
- [x] wails.json scaffolded, no duplicate frontend
- [x] Taskfile desktop:dev / desktop:build tasks
- [x] Docs updated (README.md)
- [x] Existing (non-desktop) build/test/lint confirmed unaffected

## Validation

- **Wails CLI**: installed successfully — `wails version` reports
  `v2.16.0`.
- **System webview deps**: `sudo -n true` and `sudo -n apt-get update`
  both fail with "sudo: interactive authentication is required" / "sudo:
  a terminal is required to authenticate" — this sandbox has no
  passwordless sudo and no way to supply a password, so `sudo apt-get
  install -y libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config` could not be
  run. Confirmed via `wails doctor`, which reports (on this Ubuntu 26.04
  "resolute" sandbox): `pkg-config` and `libgtk-3-dev` are "Available" in
  apt sources but not installed; `libwebkit` is "Not Found" under the
  package name `libwebkit2gtk-4.0-dev` it probes for. A direct `apt-cache
  search webkit2gtk` confirms this system's apt sources only carry
  `libwebkit2gtk-4.1-dev` (2.52.x), not the `-4.0-dev` package the spec's
  fallback names — i.e. on this OS the *first* command in the spec
  (`libwebkit2gtk-4.1-dev`) is the right one, not the fallback. Either
  way, `sudo` itself is what blocks it, not the package name.
  **A developer with real sudo access should run**: `sudo apt-get update
  && sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
  pkg-config`, then `task desktop:build` (or `wails build -s
  -skipbindings` from `cmd/catchm-desktop`) to finish verifying the
  window actually opens and renders the UI.
- **`go build ./...` / `go vet ./...` / `gofmt`**: all pass cleanly for
  the whole module, including `cmd/catchm-desktop`, with no build tag
  needed — see Decisions for why (Wails' own default build produces a
  no-op stub without the `production`/`dev` build tags, so plain `go
  build` never touches the cgo webview path). Verified by running each
  command directly against the repo with the new dependency in go.mod.
- **`task build && task test && task lint`**: all pass, unaffected by
  this change — ran and confirmed directly (`web:build` via bun/vite,
  `go build ./cmd/catchm`, `go test ./...` across all packages, `go vet
  ./...` + `gofmt -l` clean).
- **`wails build -s -skipbindings` (from `cmd/catchm-desktop`, via `task
  desktop:build`)**: attempted directly to see exactly where it fails.
  It correctly picked up `Frontend Directory: .../catchm/web`, `Skip
  Bindings: true`, `Skip Frontend: true` from `wails.json` and the CLI
  flags — i.e. the wails.json scaffolding itself is validated as correct
  by the CLI accepting it and proceeding to the compile step. It then
  failed at Go-compiling the cgo webview package with: `github.com/
  wailsapp/wails/v2/pkg/assetserver/webview: exec: "pkg-config":
  executable file not found in $PATH` — precisely the expected failure
  given no system deps are installed, and precisely at the cgo boundary
  the plain `go build ./...` above never reaches. This is strong
  evidence the `wails.json`/Taskfile wiring is correct; only the actual
  window rendering (which needs the real gtk/webkit libraries) remains
  unverified.
- **Update — verified on real Windows**: built and ran the actual
  `catchm-desktop.exe` on a real Windows 11 machine (Go 1.27.0 + WinLibs
  mingw-w64 GCC installed via `winget`, WebView2 already present;
  `wails doctor` reported "Your system is ready for Wails development!").
  `wails build` succeeded end-to-end (one harmless linker warning:
  `.rsrc merge failure: multiple non-default manifests`, build still
  completed). Launched the binary, confirmed the process and its
  `MainWindowTitle` ("CatchM"), and captured the actual window contents
  via the Win32 `PrintWindow` API (bypasses focus/z-order issues):
  the native window renders the real CatchM UI — header with the mascot
  logo, "Meetings" list, the empty-state mascot + "Your burrow is quiet."
  copy, and the "Record a meeting" button — at the configured 1180×760
  size, served entirely through `AssetServer.Handler` (no separate
  frontend, no TCP port). The Go-level wiring is now runtime-verified,
  not just code-reviewed, closing the one gap left after the Linux
  sandbox attempt above.
