# Plan: M12 — catchm-cloud backend skeleton + auth

- Status: complete
- Created: 2026-09-20
- Repo: `spacingmind/catchm-cloud` (separate repo, already scaffolded:
  Cloudflare Worker + D1 + R2, `wrangler dev` verified working locally
  with a healthz endpoint). This spec's code lands there, not in the
  main `catchm` repo.

## Goal

Prove the Cloudflare backend works in isolation: account signup and
login, with the D1 schema this whole feature needs. No E2EE crypto is
implemented by this milestone — the client (M13, in the main `catchm`
Go repo) does all key generation/wrapping and sends the *results* as
opaque blobs; this Worker only stores and returns them. Read
`docs/cloud-sync-architecture.md` in the `catchm` repo in full before
starting — it defines the security model (auth secret vs. master key,
why Argon2id runs client-side not in the Worker) this API contract
implements.

## Scope

### D1 schema (new migration in `catchm-cloud`)

```sql
CREATE TABLE users (
  id TEXT PRIMARY KEY,              -- random, not auto-increment (don't leak signup order/count)
  email TEXT NOT NULL UNIQUE,
  auth_hash TEXT NOT NULL,          -- client-derived; server never sees the password
  auth_salt TEXT NOT NULL,
  kdf_params TEXT NOT NULL,         -- JSON: {algo, iterations/memory/parallelism, ...} client used
  created_at INTEGER NOT NULL
);

CREATE TABLE e2ee_profiles (
  user_id TEXT PRIMARY KEY REFERENCES users(id),
  wrapped_master_key TEXT NOT NULL,   -- base64 ciphertext, opaque to this Worker
  key_wrap_params TEXT NOT NULL,      -- JSON, opaque, client needs it to re-derive key_wrap_key
  recovery_blob TEXT NOT NULL,
  recovery_kdf_params TEXT NOT NULL
);

CREATE TABLE sessions (
  token TEXT PRIMARY KEY,           -- random opaque token, not a JWT (see Decisions)
  user_id TEXT NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
```

`devices` and `device_link_requests` (M14) and `blobs` (M13) are NOT
part of this migration — add only what M12 needs, don't pre-build
schema for later milestones speculatively.

### Endpoints (`src/index.ts` or split into route handler files — your
call on file layout, this is a small enough API that one router file
may be cleanest, don't over-architect it)

- `POST /signup` — body: `{email, auth_hash, auth_salt, kdf_params,
  wrapped_master_key, key_wrap_params, recovery_blob,
  recovery_kdf_params}` (all opaque strings except `email`; this Worker
  never validates or interprets the crypto fields' contents, only
  stores them). Validates: `email` is a plausible email shape and not
  already registered (409 if it is), all other fields are non-empty
  strings (400 otherwise). Creates the `users` + `e2ee_profiles` rows
  in one transaction (D1 supports batched statements — use that rather
  than two separate un-transactional writes), creates a session, and
  returns `{session_token, expires_at}`.
- `POST /login` — body: `{email, auth_hash}`. Looks up the user by
  email (404-equivalent: return a generic 401 "invalid credentials"
  for both "no such email" and "wrong auth_hash" — do not let a client
  distinguish "email doesn't exist" from "wrong hash" via response
  differences, that's a user-enumeration leak). Compares `auth_hash`
  to the stored value using a constant-time comparison (see Decisions
  — do not use `===`/`==` on secret-derived strings). On success,
  creates a new session and returns `{session_token, expires_at,
  wrapped_master_key, key_wrap_params}` (the client needs the wrapped
  key immediately to unwrap locally — don't make it a second
  round-trip).
- `GET /healthz` — already exists, unchanged.
- Auth middleware for future authenticated endpoints (none exist yet
  in M12 beyond signup/login themselves, but M13 will need it): a
  small helper that reads `Authorization: Bearer <token>`, looks it up
  in `sessions`, checks `expires_at`, and returns the `user_id` or a
  401 — write it now as a reusable function even though nothing calls
  it yet in this milestone, since M13 will need it immediately and the
  shape should be settled here alongside the sessions table it reads.

### Out of scope for M12

Email verification / sending any email at all (no email provider
wired up — signup succeeds immediately, verifying real ownership of
the address is a later concern, not blocking for a personal sync
account), password reset via email, rate limiting / abuse prevention,
CORS configuration beyond what's needed for local `wrangler dev`
testing, the `/recover` endpoint (M15), device linking (M14), blob
storage endpoints (M13), JWT (see Decisions for why opaque tokens
were chosen instead), any UI (this is a backend-only milestone, no web
frontend in this repo).

## Acceptance criteria

1. `npm run typecheck` and `npm test` pass in `catchm-cloud`.
2. Hermetic tests via `@cloudflare/vitest-pool-workers` (already a
   devDependency) — real D1 (Miniflare-backed, no mocking of SQL)
   against the actual migration, no real Cloudflare account needed:
   - `TestSignupCreatesUserAndSession` (name illustrative — use
     vitest's `it`/`describe`, not Go-style names): valid signup
     returns a session token; the user and e2ee_profile rows exist
     with exactly the submitted opaque values.
   - `TestSignupRejectsDuplicateEmail`: second signup with the same
     email returns 409, doesn't overwrite the first account.
   - `TestSignupRejectsMissingFields`: 400 for a missing/empty
     required field.
   - `TestLoginSucceedsWithCorrectHash`: returns `wrapped_master_key`/
     `key_wrap_params` matching what signup stored.
   - `TestLoginFailsIndistinguishably`: both "unknown email" and
     "correct email, wrong auth_hash" return the same 401 status and
     the same generic error message/body shape (assert the bodies are
     byte-identical or structurally identical, not just "both 401").
   - `TestSessionMiddlewareRejectsExpiredOrUnknownToken`.
3. Manual/live verification: `npm run dev` (wrangler dev, local D1/R2
   emulation — this doesn't require a real Cloudflare account,
   Miniflare emulates D1 locally), then `curl` through a real
   signup→login round trip and confirm the responses match the
   contract above.
4. Migration applies cleanly via `wrangler d1 migrations apply
   catchm-cloud --local` (and the command for `--remote` is documented
   in the README, even though actually running it against a real
   Cloudflare account is the user's own step, not part of this run).

## Decisions

- **Opaque session tokens, not JWT.** Nothing in M12–M15 needs
  claims, cross-service verification, or statelessness — the Worker is
  the only consumer. A stateful random token (32 bytes hex, ~256-bit
  entropy) stored in the `sessions` table is the simplest thing that
  works and comes with free revocability (delete the row; a JWT needs
  a denylist to get the same property, which reintroduces state
  anyway) and free server-controlled TTL semantics (`expires_at`
  column, no clock-skew tolerance games). This matches the spec's own
  hint. Sessions last 30 days.
- **Constant-time compare.** Web Crypto in Workers has no
  string-compare primitive, so `src/crypto.ts` implements
  `timingSafeEqual(a, b)`: UTF-8 encode both sides, XOR-accumulate
  over the *longer* length (shorter side zero-padded via `?? 0`), fold
  the length difference into the same accumulator, single `=== 0`
  check at the end — no early exit on content or length. Login's
  "no such user" path runs the same comparison against a fixed
  64-char dummy hash before returning its 401 so both paths do
  comparable work; both failure paths return the *same*
  `invalidCredentials()` Response (same function ⇒ provably
  byte-identical body).
- **D1 batch API.** Signup inserts `users` + `e2ee_profiles` +
  `sessions` via one `env.DB.batch([...])` call (D1 documents batch as
  transactional, all-or-nothing). The pre-check `SELECT` for duplicate
  email stays for the friendly 409, and the batch is wrapped in a
  try/catch matching D1's actual UNIQUE-violation error text
  (`UNIQUE constraint failed: users.email`, probed empirically) so a
  TOCTOU race returns 409 instead of leaking a 500.
- **File layout.** `src/index.ts` (router + signup + login — the API
  is small enough that splitting per-route files would be
  over-architecting), `src/crypto.ts` (constant-time compare, random
  IDs/tokens), `src/auth.ts` (`requireSession(request, env):
  Promise<{userId} | Response>` for M13+). Tests mirror this:
  `test/{crypto,signup,login,auth}.test.ts` plus `test/setup.ts`
  applying migrations.
- **ID generation.** `crypto.getRandomValues`-based hex: user IDs are
  128-bit random hex (not UUIDv4 — deliberately unstructured so the
  format leaks nothing), session tokens are 256-bit hex.
- **Test infrastructure.** vitest bumped 2.1.9 → 3.2.7:
  `@cloudflare/vitest-pool-workers@0.7.8` crashes on vitest 2 despite
  advertising peer support for `2.0.x`. Migrations are read Node-side
  via `readD1Migrations()` in `vitest.config.ts` (async config
  function) and injected as a `TEST_MIGRATIONS` binding; `test/setup.ts`
  applies them via `applyD1Migrations(env.DB, env.TEST_MIGRATIONS)` —
  `readD1Migrations` uses `node:fs` and cannot run inside the worker
  isolate. Config keeps the `wrangler.toml` reference so D1/R2
  bindings and compat date flow from one source of truth, with
  `singleWorker: true` so test files share one migrated D1.
  Local Miniflare falls back to compat date 2025-03-10 (installed
  workerd caps below the 2026-09-20 in wrangler.toml) — warning only,
  harmless for these tests.

## Progress

- [x] D1 migration (users, e2ee_profiles, sessions)
- [x] POST /signup
- [x] POST /login (indistinguishable failure modes)
- [x] Session auth middleware helper (unused by other endpoints yet, ready for M13)
- [x] Hermetic tests (vitest-pool-workers)
- [x] Local wrangler dev + curl smoke test

## Validation

All confirmed 2026-09-20.

**Hermetic tests** (`npm run typecheck && npm test` in `catchm-cloud`):
typecheck clean; 17/17 tests pass across 4 files
(crypto 6, signup 3, login 3, auth 5), real Miniflare-backed D1
against the actual `0001_init.sql` migration. Login's two failure
paths are asserted byte-identical (`await res.text()` equality), not
just both-401.

**Migration**: `npx wrangler d1 migrations apply catchm-cloud --local`
applied `0001_init.sql` cleanly (4 commands, ✅). Remote equivalent
(when a real database_id is set in wrangler.toml): `npx wrangler d1
migrations apply catchm-cloud --remote`.

**Live smoke test** (`npm run dev` → wrangler dev on localhost:8787,
local D1/R2 emulation; opaque fields below are realistic-shaped
dummy values — server never interprets them):

```
$ curl -s -w "\nHTTP %{http_code}\n" -X POST http://localhost:8787/signup \
    -H "content-type: application/json" -d '{...email smoke@example.com,
    auth_hash a665a...27ae3, wrapped_master_key base64:6Lz+...G6hI=, ...}'
{"session_token":"e27c8983497c52690f3bdbe45b28ceec6efb47b0d144c5761d168555a9ae7815","expires_at":1792496131799}
HTTP 200

$ # same body again
{"error":"email already registered"}
HTTP 409

$ curl -s -w "\nHTTP %{http_code}\n" -X POST http://localhost:8787/login \
    -H "content-type: application/json" \
    -d '{"email":"smoke@example.com","auth_hash":"a665a...27ae3"}'
{"session_token":"6e45b0180eba75cef7703e1039cabf4415d3f8f385f288438ad8930f01649089","expires_at":1792496131839,"wrapped_master_key":"base64:6Lz+v7K7XyQwR9uM1oPq4sT8vW2xYzA5bC3dE1fG6hI=","key_wrap_params":"{\"algo\":\"aes-256-gcm\",\"nonce\":\"base64:QW5vdGhlck5vbmNl\"}"}
HTTP 200
  → wrapped_master_key / key_wrap_params byte-match what signup sent.

$ # wrong auth_hash for known email:
{"error":"invalid credentials"}
HTTP 401

$ # unknown email:
{"error":"invalid credentials"}
HTTP 401

$ diff <(wrong-hash body) <(unknown-email body) && cmp ...
DIFF: byte-identical
CMP: byte-identical

$ curl http://localhost:8787/healthz
{"status":"ok"}
HTTP 200
```

wrangler request log confirms the same: `POST /signup 200`, `POST
/signup 409`, `POST /login 200`, `POST /login 401` ×4 (two smoke +
two diff runs), `GET /healthz 200`.

**Commits** (catchm-cloud, repo `spacingmind/catchm-cloud`):
`7853008` migration · `105f152` crypto utils (+vitest 3 bump) ·
`5cd888b` POST /signup · `cd99418` POST /login + signup race fix ·
`2fc9736` session auth helper.
