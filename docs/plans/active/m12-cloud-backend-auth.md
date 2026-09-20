# Plan: M12 — catchm-cloud backend skeleton + auth

- Status: active
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

(fill in as the implementer makes calls — expected topics: opaque
session tokens vs JWT and why, exact constant-time-compare
implementation used since Web Crypto has no built-in string-compare
primitive, file/router layout, D1 batch-transaction API specifics,
UUID/random-ID generation approach for user/session IDs.)

## Progress

- [ ] D1 migration (users, e2ee_profiles, sessions)
- [ ] POST /signup
- [ ] POST /login (indistinguishable failure modes)
- [ ] Session auth middleware helper (unused by other endpoints yet, ready for M13)
- [ ] Hermetic tests (vitest-pool-workers)
- [ ] Local wrangler dev + curl smoke test

## Validation

(to be filled as criteria are confirmed)
