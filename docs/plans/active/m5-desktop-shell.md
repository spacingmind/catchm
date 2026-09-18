# Plan: M5 — desktop shell (Wails, minimal native window)

- Status: active
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

(fill in as the implementer makes calls — expected topics: exact Wails
version pinned, whether system deps installed successfully or not and
what was done instead, `wails.json` frontend-command handling, whether
`go build ./...` needed a build tag to isolate the cgo dependency from
the rest of the module.)

## Progress

- [ ] Wails CLI installed / attempted
- [ ] System webview dependencies installed / attempted (document outcome)
- [ ] cmd/catchm-desktop/main.go written (AssetServer reusing internal/server.New)
- [ ] wails.json scaffolded, no duplicate frontend
- [ ] Taskfile desktop:dev / desktop:build tasks
- [ ] Docs updated
- [ ] Existing (non-desktop) build/test/lint confirmed unaffected

## Validation

(to be filled honestly — including "not verified because X" where that's
the true state, not a glossed-over pass)
