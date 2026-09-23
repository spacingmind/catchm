# Plan: M17 — Cloud backend hardening: rate limiting + storage quota

- Status: active
- Created: 2026-09-24
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

(fill in: whether Miniflare@installed emulates the ratelimit binding
and the resulting test strategy; exact limits/periods/keys; quota
default; putBlob step ordering and reservation-release path; whether
the params endpoints got limits.)

## Progress

- [ ] Verify ratelimit binding emulation in the installed test pool
- [ ] Rate limiting on /login, /recover, /signup (+ anti-enum test)
- [ ] Migration 0004 + quota enforcement on PUT/DELETE blobs
- [ ] catchm client: ErrRateLimited / ErrQuotaExceeded + server mapping
- [ ] Live wrangler-dev verification

## Validation

(to be filled as criteria are confirmed)
