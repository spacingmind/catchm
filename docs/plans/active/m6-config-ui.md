# Plan: M6 — Settings UI for engine config (Soniox key, whisper/translate hosts)

- Status: active
- Created: 2026-09-18

## Goal

`internal/config` only supports reading `~/.catchm/config.yaml` (`Load()`).
There is no way to set the Soniox API key, whisper server URL, or
translate host/models except by hand-editing that YAML file on disk.
Add a `Save` path and a Settings UI screen so a user can configure these
from the app itself.

## Scope

- `internal/config/config.go`: add `Save(cfg *Config) error` — writes
  `cfg` back to the same path `Load`/`Path()` resolve, as YAML, creating
  the parent directory (`0o700`) if needed, file mode `0o600` (contains
  a secret). Round-trip: `Save` then `Load` must reproduce the same
  `Config` (add a test for this).
- `internal/server`: two new endpoints:
  - `GET /api/config` — returns the current config as JSON, but **never
    echoes the raw Soniox API key**: replace `Soniox.APIKey` in the
    response with a boolean `soniox_api_key_set` field (true/false)
    instead of the key value. Reuse `Config`'s existing YAML field names
    for JSON where there's no secrecy concern (`engine`, `lang`,
    `whisper_url`, `soniox.url`, `translate.host`, `translate.live_model`,
    `translate.batch_model`).
  - `PUT /api/config` — accepts the same shape (with an optional
    `soniox_api_key` field in the request body only, never present in
    responses); if `soniox_api_key` is omitted/empty in the request, the
    existing stored key is preserved (an empty PUT must not silently
    wipe a previously configured key — this is the main correctness trap
    in this milestone, write a test for it explicitly). Validates
    non-empty `engine`/`whisper_url` are still reasonable (don't over
    -validate; empty string is fine here, real validation happens when
    the engine is actually used). Calls `config.Save`, then re-loads and
    returns the sanitized config (same shape as GET).
  - The `Server` needs to hold its live `*config.Config` in a way these
    handlers can read/mutate safely across concurrent requests — it
    already has `s.cfg *config.Config`; guard reads/writes of `s.cfg`
    with the existing `s.mu` mutex (already used for `s.pipelines`) or a
    dedicated one if that reads cleaner, your call, record which.
- Web: add a new `web/src/components/SettingsConfig.tsx` screen (or a
  section within the existing Settings area — see the Decisions note
  below on how `SettingsTranslate` is currently reached from `App.tsx`)
  with a form: default engine (`Select`: whisper/soniox), whisper URL
  (`Input`), Soniox WebSocket URL (`Input`), Soniox API key (`Input
  type="password"`, placeholder shows "Currently set" vs "Not set" based
  on `soniox_api_key_set` rather than ever displaying a real key),
  translate host/live-model/batch-model (three `Input`s). A "Save"
  button calling `PUT /api/config`, with a brief inline "Saved"
  confirmation matching the pattern already used in `LiveMeeting.tsx`'s
  note-save confirmation (local state + `setTimeout`, no toast library).
  Restructure `App.tsx`'s Settings entry point so both this new screen
  and the existing `SettingsTranslate` (Ollama status/pull/test) are
  reachable — simplest approach: a `Tabs` component (shadcn `Tabs`,
  already installed dependencies cover Radix tabs — check if the
  `tabs.tsx` ui component already exists under `web/src/components/ui/`;
  if not, add it following the same hand-written-shadcn-style pattern
  used for the other `ui/` components in this repo) with two tabs
  "Engines" (this new form) and "Translation" (the existing
  `SettingsTranslate` content, unchanged).
- `web/src/lib/api.ts`: add `getConfig()`/`updateConfig(body)` typed
  functions matching the new endpoints' exact JSON shapes.

Out of scope: any change to how `newEngine`/`newTranslator` factories in
`internal/server` pick up config changes for an *already-running* live
pipeline (config changes apply to the *next* meeting started, not
retroactively to one in progress); encrypting the API key at rest
(plain YAML file, `0o600`, matches the rest of this local-first app's
security model); multi-user/profile config.

## Acceptance criteria

1. `task build && task test && task lint` pass (Go side).
2. `task web:install && task web:build` succeeds, no TypeScript errors.
3. `TestConfigSaveLoadRoundTrip` (internal/config) — `Save` then `Load`
   reproduces the same values, including the Soniox API key.
4. `TestConfigEndpointGetSanitizesKey` (internal/server) — `GET
   /api/config` with a configured Soniox key never contains the literal
   key string in the response body; `soniox_api_key_set` is `true`.
5. `TestConfigEndpointPutPreservesKeyWhenOmitted` (internal/server) —
   set a key via one `PUT`, then `PUT` again without `soniox_api_key` in
   the body (or with it explicitly empty): a subsequent `GET` still
   reports `soniox_api_key_set: true`, and (reach into the store's
   config file or add a test-only accessor, your call) the actual saved
   key is unchanged.
6. `TestConfigEndpointPutUpdatesFields` — a `PUT` with new
   `whisper_url`/`translate` fields is reflected in a subsequent `GET`.
7. Manual verification: build the real binary, `curl` the two endpoints
   with/without a key to confirm the behaviors above hold end-to-end,
   and if you have a headless browser available, screenshot the new
   Settings/Engines tab.

## Decisions

(fill in: exact `App.tsx`/Settings restructuring approach chosen, mutex
strategy for `s.cfg`, whether `tabs.tsx` already existed or was added.)

## Progress

- [ ] internal/config.Save + round-trip test
- [ ] GET/PUT /api/config endpoints with key-sanitization and preserve-on-omit semantics
- [ ] web: SettingsConfig screen + Tabs restructuring of Settings entry point
- [ ] tests

## Validation

(to be filled as criteria are confirmed)
