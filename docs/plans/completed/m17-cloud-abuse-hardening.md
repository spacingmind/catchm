# Plan: M17 — Cloud backend hardening: rate limiting + storage quota

- Status: complete
- Created: 2026-09-24
- Completed: 2026-09-24
- Repos: **`catchm-cloud`** (main work) and **`catchm`** (client
  handling of the two new error statuses). Read
  `docs/plans/completed/m12-*`, `m13a-*`, `m15-*` first — this
  hardens endpoints those milestones built.

## Goal

Close the two gaps M12/M13a/M15 explicitly deferred as "needed before
real users": (1) nothing stops online brute-forcing of `/login` and
`/recover`, and (2) nothing caps how much a single account can store.
Both must land before the first real Cloudflare deployment.

## Research summary (Perplexity, 2026-09-24 — verify, don't trust)

- The Workers **Rate Limiting binding** (`[[ratelimits]]` in
  `wrangler.toml`, `await env.X.limit({ key })` → `{ success }`) is
  reported GA since wrangler 4.36. Semantics: per-colo, eventually
  consistent, deliberately permissive; `period` is **10 or 60 seconds
  only**. Cloudflare recommends stable keys over raw IPs.
- Durable Objects give strict global counters but add latency/
  complexity — not needed for brute-force protection.
- **Unverified claim to check yourself**: pplx says Miniflare emulates
  the ratelimit binding locally. This repo pins
  `@cloudflare/vitest-pool-workers@^0.7.x` (old). Verify empirically
  against the installed version before designing tests around it — if
  it isn't emulated (or bumping the pool breaks things), put the
  limiter behind a tiny interface with a test double instead, and
  record which path you took.
- Workers platform body limit is 100 MB on Free/Pro (Cloudflare
  returns 413 before the Worker runs).

## Scope

### 1. Rate limiting (`catchm-cloud`)

- Two limiters: **per-account** (key derived from the *normalized
  email*, e.g. `sha256(lowercase(trim(email)))` — not user id, because
  unknown emails have no user id and must be limited identically) and
  **per-IP** (`cf-connecting-ip`, with a deterministic fallback when
  the header is absent, as in tests).
- Apply to `POST /login` and `POST /recover` (per-account + per-IP),
  and `POST /signup` (per-IP only). Pick concrete limits (e.g. 5/60s
  per account, 20/60s per IP) and record them in Decisions.
- **Anti-enumeration is the core constraint**: the limiter check must
  run *before* the user lookup, and must key on the email as submitted
  (normalized) whether or not the account exists — otherwise "gets
  429'd faster" becomes an account-existence oracle. The 429 body
  must be identical for known and unknown emails. Add a test proving
  it.
- 429 response: JSON `{"error": "too many attempts"}` plus a
  `Retry-After` header (the period length is fine).
- Leave `/auth-params` and `/recovery-params` un-limited unless you
  find a concrete reason (they're cheap and already enumeration-safe);
  note the call in Decisions.

### 2. Per-account storage quota (`catchm-cloud`)

- Migration `0004_storage_quota.sql`: add `storage_used_bytes INTEGER
  NOT NULL DEFAULT 0` to `users`, backfilled from
  `SUM(blobs.size_bytes)` per user.
- Quota value from a `[vars]` entry (e.g. `STORAGE_QUOTA_BYTES`),
  default **100 MiB** — meeting bundles are text-only (no
  `audio.wav`), so this is generous; record the number.
- Enforcement on `PUT /blobs/:id`, atomically under concurrent PUTs:
  a **maintained counter** reserved via a conditional
  `UPDATE users SET storage_used_bytes = storage_used_bytes + ?delta
  WHERE id = ? AND storage_used_bytes + ?delta <= ?quota`, checking
  `meta.changes`. **Gotcha**: don't put this in the same `DB.batch()`
  as the metadata write and assume the batch aborts on 0 changes — it
  won't; every statement in a batch still runs. Run the reservation
  as its own statement first and branch on `changes`.
- `delta` = new size for a new blob, `new − old` for an overwrite
  (negative deltas always succeed). `DELETE /blobs/:id` decrements.
- Read the real `putBlob` first and order the steps so a reservation
  is **released** if anything after it fails (version-conflict 409,
  R2 put failure) — no leaked usage. Record the exact ordering.
- Over quota → **507** with JSON `{"error": "storage quota exceeded",
  "used": N, "quota": M}`.
- Tests: fills to exactly the quota, one byte over → 507; overwrite
  shrinking a blob frees space; delete frees space; a rejected PUT
  (409 or 507) leaves `storage_used_bytes` unchanged; two concurrent
  PUTs that together exceed quota can't both succeed.

### 3. Client handling (`catchm`)

- `internal/cloudsync`: map 429 → sentinel `ErrRateLimited` (carry
  `Retry-After` if cheap) and 507 → `ErrQuotaExceeded`; httptest
  tests for both.
- `internal/server/cloud.go` `respondCloudErr`: surface them as 429 /
  507 with human messages ("Too many attempts — try again in a
  minute", "Cloud storage is full"), instead of today's generic 500.
- `web/src/components/SettingsCloud.tsx`: these already render through
  the existing inline error paths — just confirm the messages read
  well; no new UI.

## Out of scope

Real deployment (blocked on the user's Cloudflare credentials),
Durable-Object strict global counters, WAF rules, CAPTCHA, account
lockout/email alerts, per-plan quotas / billing, audio sync.

## Acceptance criteria

1. `catchm-cloud`: `npm run typecheck` + `npm test` green, with new
   tests for every bullet in sections 1–2 (including the
   anti-enumeration 429 test and the concurrent-PUT quota test).
2. `catchm`: `go build ./... && go vet ./... && gofmt -l . && go test
   ./...` green; `cd web && bun run build` clean.
3. Live check against `wrangler dev`: hammer `/login` past the limit
   for a known and an unknown email and show identical 429s; push a
   bundle that exceeds a deliberately tiny `STORAGE_QUOTA_BYTES`
   override and show the 507 surfacing through `/api/cloud/sync`.
   Paste real output into Validation.

## Decisions

- **Test strategy — parse-but-not-emulate trap.** The pinned pool
  (`@cloudflare/vitest-pool-workers@0.7.8`, miniflare `3.20250310.0`)
  accepts `[[ratelimits]]` at the type/schema level but leaves
  `env.X === undefined` at runtime (verified empirically). wrangler
  4.135 ships its own miniflare 5.x, so `wrangler dev` and production
  do have the binding. Bumping the pool to 0.22.0 would force vitest
  3→4; rejected. The limiter sits behind `src/ratelimit.ts`; tests use
  an in-memory double selected **only** by `TEST_FAKE_RATELIMIT=1`,
  set in `vitest.config.ts` and never in `wrangler.toml`. The real
  binding is covered by the live check below.
- **Fail closed.** If neither the real binding nor the explicit test
  flag is present, limited endpoints return 500 `rate limiter
  misconfigured`; a missing binding never silently disables
  protection. Likewise a missing/garbage `STORAGE_QUOTA_BYTES` parses
  to 0 (every PUT 507s) rather than "unlimited".
- **Binding shape bug caught in review.** The first cut declared its
  own `limit(key: string)` interface and reached the binding through an
  `unknown` cast, so tsc never compared it to the real
  `limit({ key }): Promise<{ success }>` — in production every user
  would have shared one undefined-key bucket. Now typed as
  workers-types' `RateLimit`; the live A→429 / B→401 check proves keys
  are distinct.
- **Limits.** `ACCOUNT_LIMITER` 5/60s, key
  `sha256("account:" + lower(trim(email)))`, on `/login` + `/recover`,
  checked **before** the user lookup so unknown emails are limited
  identically. `IP_LIMITER` 20/60s, key `cf-connecting-ip` (fallback
  `"test-ip"`), deliberately **one shared per-IP budget** across
  `/signup`, `/login`, `/recover` (rotating endpoints can't multiply
  it). 429 body `{"error":"too many attempts"}` + `Retry-After: 60`.
  `/auth-params` and `/recovery-params` stay unlimited — cheap and
  already enumeration-safe.
- **Quota.** `users.storage_used_bytes` (migration 0004, backfilled
  from `SUM(blobs.size_bytes)`); `STORAGE_QUOTA_BYTES` default
  104857600 (100 MiB). Note: wrangler `[vars]` arrive as top-level env
  properties, not `env.vars`.
- **putBlob ordering.** validate → version check (409 *before* any
  reservation) → standalone conditional `UPDATE … WHERE used + delta
  <= quota`, branched on `meta.changes` (not batched — D1 batches don't
  abort on zero-change statements) → R2 put to a **unique key per
  write** `<user>/<id>/<version>-<uuid>` (release on failure) →
  version-guarded `UPDATE` or `INSERT` (UNIQUE violation caught) →
  loser releases + deletes its own object and returns 409; winner
  best-effort deletes the superseded object.
- **Races found in review and fixed.** (1) The fixed R2 key
  `<user>/<id>` (since M13a) let a losing concurrent writer overwrite
  the winner's bytes under the winner's metadata — silent loss of the
  winner's edit, no conflict copy. (2) Concurrent first-writes hit the
  `(user_id, id)` PK, D1 *threw*, the reservation leaked and the loser
  got 500. `DELETE` is now version-guarded (it was size-guarded, which
  let a same-size concurrent re-PUT through).
- **409 bodies** are built explicitly from `{version, checksum,
  size_bytes, updated_at}` — a regression briefly serialized the
  internal `r2_key` (leaking user id + storage layout).
- **Client.** `cloudsync.ErrRateLimited` (carries `Retry-After`) and
  `ErrQuotaExceeded`; `respondCloudErr` → 429 "Too many attempts — try
  again in a minute." / 507 "Cloud storage is full."; a 507 mid-sync
  aborts without saving the manifest (M13c's save-only-on-success
  invariant holds). No new web UI — SettingsCloud's inline error
  paths render them.
- **Deferred.** An R2 orphan sweeper (orphans only arise if the Worker
  dies between R2 put and D1 write, or a best-effort delete fails),
  Durable-Object strict global counters, per-plan quotas.

## Progress

- [x] Verify ratelimit binding emulation in the installed test pool
- [x] Rate limiting on /login, /recover, /signup (+ anti-enum test)
- [x] Migration 0004 + quota enforcement on PUT/DELETE blobs
- [x] catchm client: ErrRateLimited / ErrQuotaExceeded + server mapping
- [x] Live wrangler-dev verification

## Validation

Hermetic (2026-09-24): `catchm-cloud` `npm run typecheck` clean, `npm
test` → 10 files / 69 tests passed (incl. `ratelimit.test.ts`,
`quota.test.ts`, `races.test.ts`); `catchm` `go build/vet/gofmt`
clean, `go test ./...` all ok; `bun run build` clean.

Live, real `wrangler dev --var STORAGE_QUOTA_BYTES:400` (real
ratelimit bindings) + isolated `catchm serve` (scratch `CATCHM_HOME`).
Run by the orchestrator: GLM hit its 5-hour usage limit at this step.

```
== signup known@m17.test via catchm ==
{"email":"known@m17.test","recovery_code":"<redacted>"}
== 6x POST /login known@m17.test (wrong hash) ==
call 1-5: 401
call 6: 429
== 6x POST /login nobody@m17.test (wrong hash) ==
call 1-5: 401
call 6: 429
== 429 response (known) ==
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 60
{"error":"too many attempts"}
== diff known vs unknown 429 ==
IDENTICAL
```
Per-IP bucket usage in that window was 15 of 20, so both 429s came
from the per-account limiter.

```
== counter before ==                        "storage_used_bytes": 0
== sync #1 (small meeting) ==
{"pushed":["1f940103c43dd19e"],"pulled":null,"conflicted":null}  [HTTP 200]
== counter after sync #1 ==                 "storage_used_bytes": 254
(add a ~560-byte note to the meeting)
== sync #2 (exceeds 400B quota) ==
{"error":"Cloud storage is full."}  [HTTP 507]
== counter after rejected sync ==           "storage_used_bytes": 254
== blob server-side ==  "id": "1f940103c43dd19e" "version": 1 "size_bytes": 254
== client manifest ==   remote_version 1 (not advanced)
```
The rejected overwrite leaked no reservation, left the server blob at
version 1, and left the client manifest untouched.
