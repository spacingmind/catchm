# Plan: M18 — CI for both repos + cloud server URL setting

- Status: complete (server-URL UI moved to M20)
- Created: 2026-09-24
- Completed: 2026-09-24
- Repos: `catchm` (CI + setting) and `catchm-cloud` (CI only).

## Goal

Two independent, pre-deploy pieces:

- **A. CI.** Both repos are now on GitHub (`spacingmind/catchm`,
  default branch `develop`; `spacingmind/catchm-cloud`, default
  `main`, both private) with no CI. Every milestone so far was
  verified by hand. Add GitHub Actions so regressions get caught on
  push.
- **B. Cloud server URL setting.** `Cloud.api_base_url` defaults to
  `http://127.0.0.1:8787` (wrangler dev) and today can only be changed
  by editing `config.yaml` (M13c decision). Once `catchm-cloud` is
  deployed, users need to point CatchM at the real URL from the
  Cloud settings tab.

## Research notes (Perplexity + local verification, 2026-09-24)

- pplx: as of 2026-09-23, GitHub runners removed Node 20 for
  JavaScript actions. Use current majors (`actions/checkout@v6`,
  `actions/setup-go@v6`, `actions/setup-node@v6`, current
  `oven-sh/setup-bun`). Watch the first run for deprecation warnings.
- **pplx was wrong about systray.** It said `energye/systray` needs
  CGO + GTK/AppIndicator on Linux. Checked in the module cache:
  `energye/systray@v1.0.3` only has `import "C"` in
  `systray_darwin.go`; on Linux it's pure Go (DBus). The real CGO
  dependency is `malgo` (miniaudio) in `internal/capture`
  (`CGO_ENABLED=0 go build ./...` fails there). miniaudio loads audio
  backends at runtime, so gcc (preinstalled on ubuntu-latest) should
  be enough. Prove it with a real CI run; don't add apt packages
  unless the run shows they're needed.
- `internal/agent` tests already `t.Skip` when the `claude` CLI isn't
  on PATH. CI runners don't have it, so these skip cleanly. Don't
  add credentials to CI.
- workerd / `@cloudflare/vitest-pool-workers` runs on ubuntu-latest
  with no extra system packages.

## Scope

### A1. `catchm/.github/workflows/ci.yml`

- Triggers: push to `develop` and `main`, and `pull_request`.
- `permissions: contents: read`; concurrency group per workflow+ref
  with `cancel-in-progress: true`.
- Steps: checkout → setup-go (`go-version-file: go.mod`, currently go
  1.27.0, cache on) → setup-bun → `bun install --frozen-lockfile` +
  `bun run build` in `web/` (so the embedded UI is real and web type
  errors fail CI) → the same checks as `Taskfile.yml`'s `lint` and
  `test` (`go vet ./...`, gofmt check over `git ls-files '*.go'`,
  `go test ./...`). Mirror the Taskfile commands rather than
  inventing different ones.
- Optional `-race` job: decide whether it's worth it (the pipeline
  and server have goroutines and mutexes). If it passes locally, add
  it; if it surfaces real races, record them and stop for review
  rather than silently dropping `-race`.

### A2. `catchm-cloud/.github/workflows/ci.yml`

- Triggers: push to `main`, `pull_request`. Same permissions and
  concurrency.
- setup-node (pin an LTS, e.g. 24, and record why), `npm ci`, `npm run
  typecheck`, `npm test`.
- No deploy job. Deployment is a separate, later step and needs
  Cloudflare secrets.

### B. Cloud server URL setting (`catchm`)

- **Endpoint:** a dedicated `PUT /api/cloud/server {api_base_url}`,
  not `PUT /api/config`, because changing servers has side effects.
  `GET /api/cloud/status` also returns the current `api_base_url`.
- **Only while logged out.** Session token, master key, stored account
  params, and the sync manifest all belong to one server. Return 409
  if `s.cloud != nil` and make the UI say "Log out first".
- **Validation (security):** absolute URL. `https` for any host;
  plain `http` **only** for loopback (`localhost`, `127.0.0.0/8`,
  `::1`). Otherwise 400. The `auth_hash` is replayable for login and
  session tokens are bearer credentials, so they must never cross the
  network in plaintext to a remote host. Normalize by trimming the
  trailing slash. Reject userinfo (`https://user:pass@…`) and
  query/fragment.
- **Side effects on change:** clear `Cloud.email`, `auth_salt`, and
  `kdf_params`, since they identify an account on the *old* server,
  then persist via the existing config save path.
- **Manifest per server:** `cloud-sync-state.json` currently assumes
  one server. Key it by server, e.g.
  `cloud-sync-state-<sha256(normalized url)[:16]>.json`. That way,
  switching A → B → A neither pushes against B with A's remote
  versions nor forgets A's state. Decide what to do with an existing
  unkeyed legacy file (pre-launch, dev data only; any reasonable
  choice is fine) and record it.
- **UI (`SettingsCloud.tsx`):**
  - Logged-out view: a small "Server" field showing the current URL,
    editable, with Save. An inline error shows validation failures.
    Keep it visually secondary (an "Advanced" disclosure is fine),
    since most users use the default hosted server once it exists.
  - Logged-in view: show the server read-only in muted text next to
    the email.
- **Tests:** handler validation table (https ok, http://127.0.0.1:8787
  ok, http://localhost ok, http://example.com → 400, userinfo → 400,
  garbage → 400); 409 while logged in; persistence plus clearing of the
  account params; manifest isolation (sync vs fake server A, switch to
  B, sync pushes with expectedVersion 0; switch back to A, A's
  manifest is intact).

## Out of scope

Deploy jobs or Cloudflare secrets in CI, release/packaging workflows,
desktop (Wails) builds in CI, multi-account support.

## Acceptance criteria

1. Both workflows committed; a real GitHub Actions run on each repo is
   green (`gh run watch` / `gh run view` output in Validation), with
   no Node-20 deprecation warnings.
2. `go build ./... && go vet ./... && gofmt -l . && go test ./...` and
   `bun run build` are clean locally. Tests for B cover every bullet
   above.
3. Live check: with `catchm serve` + `wrangler dev`, change the server
   URL through the API while logged out (ok), try while logged in
   (409), try `http://example.com` (400). Paste the real output.

## Decisions

- **catchm CI** (`.github/workflows/ci.yml`): checkout@v6, setup-go@v6
  (`go-version-file`, cache), `oven-sh/setup-bun@v2` (the first cut
  used `@v3`, which doesn't exist and failed job setup; v2 runs on
  node24), `bun install --frozen-lockfile` + `bun run build`, then
  Taskfile-mirrored lint and **`go test -race ./...`**. The local
  `-race` run found no data races. No apt packages were needed on
  ubuntu-latest (malgo's CGO only needs the preinstalled gcc), which
  confirms the research correction above. `internal/agent` tests skip
  on runners (no `claude` CLI).
- **catchm-cloud CI**: setup-node@v6 Node 24 LTS, `npm ci`, typecheck,
  test. The clean-clone check caught a real bug. `vitest ^3.2.7` was
  outside `@cloudflare/vitest-pool-workers@0.7.8`'s peer range
  (`2.0.x–3.0.x`), so the lockfile carried peer-marked
  `@vitest/snapshot@3.0.9` that `npm ci` skips, and tests failed in a
  fresh clone (`No such module`). Fix: `vitest ~3.0.9` + regenerated
  lockfile. That also silenced the per-run peer-range warning.
- **Server URL backend** (`c3da096`): `PUT /api/cloud/server`, 409
  while logged in, https for any host / http only for loopback
  (`localhost` exact match or `net.IP.IsLoopback`), rejects
  userinfo/query/fragment, trims trailing slash, clears
  email/auth_salt/kdf_params on change, `api_base_url` in `GET
  /api/cloud/status`. Manifest keyed per server as
  `cloud-sync-state-<sha256(url)[:16]>.json`; the legacy unkeyed file
  is ignored (pre-launch dev data only).
- **B UI moved to M20**, which redesigns the whole Settings screen.
  Building a one-off field into the old layout first would be churn.

## Progress

- [x] A1 catchm CI workflow (+ -race decision)
- [x] A2 catchm-cloud CI workflow
- [x] Real green Actions runs on both repos
- [x] B endpoint + validation + side effects + per-server manifest
- [x] B UI → moved to M20 (Settings redesign)
- [x] Validation recorded (live UI check → M20)

## Validation

- catchm-cloud Actions run 35955115845: green on first run.
- catchm Actions run 35955119343: failed at job setup (`Unable to
  resolve action oven-sh/setup-bun@v3`). After the fix, run
  35955225143: **green, build-and-test in 2m17s**, including
  `bun run build`, lint, and `go test -race ./...`.
- `c3da096`: `go build/vet/gofmt` clean; server/cloudsync/config tests
  pass (URL-validation table of 14 cases, 409 while logged in,
  param-clearing + persistence, A→B→A manifest isolation).
