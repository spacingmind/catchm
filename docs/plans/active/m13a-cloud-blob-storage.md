# Plan: M13a — catchm-cloud blob storage + auth-params lookup

- Status: active
- Created: 2026-09-20
- Repo: `catchm-cloud` (not the main `catchm` Go repo).

## Goal

Two additions to the M12 backend, both prerequisites for the Go client
work in M13b/M13c:

1. **Blob storage** — authenticated PUT/GET/LIST/DELETE for encrypted
   meeting blobs, with optimistic-concurrency conflict detection (the
   LWW model from `docs/cloud-sync-architecture.md`).
2. **`GET /auth-params`** — a gap M12 left: `/login` only accepts
   `{email, auth_hash}`, but the *client* can't compute `auth_hash`
   without first knowing the `auth_salt`/`kdf_params` it used at
   signup. A brand-new device logging in with just email+password has
   no local record of those — it must fetch them first. This is the
   real design used by Bitwarden's "prelogin" endpoint (confirmed
   pattern from the M12-era research): read `docs/cloud-sync-architecture.md`
   again if needed, then note the anti-enumeration requirement below
   carefully — this is the part most likely to be implemented
   naively and leak account existence.

## Scope

### D1 migration (new file, `migrations/0002_blobs.sql`)

```sql
CREATE TABLE blobs (
  id TEXT NOT NULL,              -- client-chosen (a CatchM meeting id), not server-generated
  user_id TEXT NOT NULL REFERENCES users(id),
  r2_key TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  checksum TEXT NOT NULL,        -- sha256 hex of the ciphertext, client-computed and verified server-side
  size_bytes INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, id)
);
```

### `GET /auth-params?email=...`

Returns `{auth_salt, kdf_params}` for the given email. **Anti-
enumeration requirement**: this must NOT reveal whether the email is
actually registered. For an unknown email, return a deterministic,
plausible-looking but meaningless `auth_salt`/`kdf_params` pair rather
than a 404 or an error — e.g. derive a fake salt deterministically from
the email itself (so repeated lookups for the same unknown email are
stable, avoiding a timing/consistency tell) using something like
`SHA-256(email + ":no-such-user-salt")`, paired with the same default
`kdf_params` real accounts use. Same response shape, same status code
(200), whether or not the account exists. Do not skip this — it's the
entire point of this endpoint's design, not a nice-to-have.

### Blob endpoints (all behind `requireSession` from M12's `src/auth.ts`)

- `PUT /blobs/:id` — body is the raw ciphertext (arbitrary bytes,
  `Content-Type: application/octet-stream`); headers
  `X-Checksum: <sha256 hex>` and `X-Expected-Version: <int>` (0 for
  "this is a new blob, must not already exist"). Server: verify the
  actual received body's sha256 matches `X-Checksum` (400 on mismatch —
  catches transport corruption, not a security control since the
  client already trusted its own ciphertext); look up the current
  stored `version` for `(user_id, id)`; if `X-Expected-Version` doesn't
  match the current version (or the row doesn't exist and expected
  isn't 0, or it does exist and expected is 0), return **409** with
  the current server-side metadata `{version, checksum, size_bytes,
  updated_at}` in the body so the client can decide how to reconcile
  (per the architecture doc's conflict-copy design — reconciliation
  itself is the Go client's job in M13c, this endpoint just needs to
  refuse silently-clobbering writes and hand back enough info to act
  on). On a version match, store bytes in R2 at a `r2_key` of the form
  `<user_id>/<id>`, upsert the D1 row with `version = X-Expected-Version + 1`,
  return `{version, checksum}` 200.
- `GET /blobs/:id` — returns the raw ciphertext bytes with
  `X-Version`/`X-Checksum` response headers; 404 if missing, and
  crucially 404 (not 403) if it exists but belongs to a different
  user — do not confirm existence-for-someone-else via a different
  status code.
- `GET /blobs` — lists the authenticated user's blob metadata (no
  bodies): `[{id, version, checksum, size_bytes, updated_at}, ...]` —
  this is what the Go client will diff its local manifest against in
  M13c to decide what to push/pull, so the shape matters; get it right
  now rather than adjusting it later.
- `DELETE /blobs/:id` — removes both the D1 row and the R2 object;
  404 if missing or not owned by the caller (same non-leaking
  reasoning as GET).

### Out of scope for M13a

Any actual encryption/decryption (this Worker only ever sees/stores
ciphertext bytes it cannot interpret), the Go client, device linking,
recovery, quotas/size limits per account (flagged as a known future
need in the architecture doc, not blocking here), pagination on
`GET /blobs` (fine for a personal-account meeting count for now).

## Acceptance criteria

1. `npm run typecheck && npm test` pass.
2. Hermetic tests (vitest-pool-workers, real D1 + R2 emulation):
   - `auth-params` returns the same shape/status for a real registered
     email and a made-up one; the fake-account response is
     deterministic across repeated calls for the same unknown email
     (assert two calls return identical `auth_salt`).
   - PUT a new blob (`X-Expected-Version: 0`) succeeds, version becomes 1.
   - PUT again with `X-Expected-Version: 1` succeeds, version becomes 2,
     R2 content actually updated (fetch and check).
   - PUT with a stale `X-Expected-Version` returns 409 with the real
     current metadata in the body.
   - PUT with a checksum that doesn't match the body returns 400.
   - GET returns the right bytes + headers; GET for another user's
     blob id (or a nonexistent one) returns 404, not 403.
   - `GET /blobs` lists exactly the caller's own blobs, correct shape.
   - DELETE removes the row and the R2 object (verify a subsequent GET
     404s and the R2 object is actually gone, not just the D1 row).
   - All blob endpoints reject an unauthenticated request the same way
     `requireSession` already does (401), and reject an expired/unknown
     session token.
3. Manual/live verification: `npm run dev`, curl through a real
   put→get→put(conflict)→list→delete round trip, paste the actual
   output into Validation.
4. Migration `0002_blobs.sql` applies cleanly on top of `0001_init.sql`
   via `wrangler d1 migrations apply catchm-cloud --local`.

## Decisions

- **Anti-enumeration fake salt** (done): unknown email →
  `hex(SHA-256(email + ":no-such-user-salt"))` via
  `crypto.subtle.digest` (64-char hex, same shape as real salts);
  deterministic per email, so repeated lookups are stable. Same 200
  status and `{auth_salt, kdf_params}` shape as the registered path.
- **Fake `kdf_params` default** (done):
  `{"algo":"argon2id","memory_kib":19456,"iterations":2,"parallelism":1}`
  (OWASP-recommended tier) — purely for shape-consistency of the fake
  response; M13b independently chooses the real client-side KDF
  parameters, no need to match.
- **R2 key naming** (done, PUT): `<user_id>/<id>` exactly as specced.
- **Error precedence** (done, PUT): header shape (400) → checksum-vs-body
  (400) → version logic (409). Checksum before version so a corrupted
  upload never 409s with metadata; all three 409 cases (exists+0,
  missing+non-zero, version mismatch) use the same conflict shape —
  existing rows return their real current metadata verbatim, the
  missing-row case returns `{error: "no such blob", version: 0, ...}`.
- **D1/R2 two-write ordering** (done, PUT): R2 write first, D1
  upsert second, no two-phase commit. A failure after the R2 write
  but before the D1 upsert leaves an orphaned R2 object unreachable
  via the API (no D1 row references it) — acceptable for this
  milestone, a future cleanup job could sweep unreferenced keys.
  The ordering protects the invariant that matters: a D1 version is
  never bumped without the new bytes already durable, so a stale D1
  row still points at the previous intact R2 content.

## Progress

- [ ] D1 migration 0002 (blobs table)
- [x] GET /auth-params (anti-enumeration)
- [x] PUT /blobs/:id (optimistic concurrency, checksum validation)
- [x] GET /blobs/:id
- [x] GET /blobs (list)
- [ ] DELETE /blobs/:id
- [ ] tests + live smoke test

## Validation

(to be filled as criteria are confirmed)
