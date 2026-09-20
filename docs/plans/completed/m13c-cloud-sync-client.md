# Plan: M13c — Go cloud sync client (wiring M12/M13a/M13b together)

- Status: complete
- Created: 2026-09-21
- Repo: `catchm` (this repo). Talks to `catchm-cloud` over HTTP; no
  changes to `catchm-cloud` in this milestone.

## Goal

Turn the pieces built so far into an actual working sync: a Go client
that signs up/logs in against `catchm-cloud` (M12), encrypts a
meeting's data with the primitives in `internal/e2ee` (M13b), and
pushes/pulls it as a blob via M13a's endpoints, with the LWW +
conflict-copy reconciliation from `docs/cloud-sync-architecture.md`.
Backend-only for this milestone — no web UI screen yet (that's a
follow-up, matching how translate got its own engine milestone before
its setup-UI milestone). Read `docs/cloud-sync-architecture.md` and
all three completed specs it's built on
(`docs/plans/completed/m12-cloud-backend-auth.md`,
`m13a-cloud-blob-storage.md`, `m13b-e2ee-crypto-primitives.md`) in full
before starting.

## Scope

### 1. `internal/cloudsync` — HTTP client for `catchm-cloud` (no crypto, no meeting logic)

A `Client` struct wrapping the exact endpoints M12/M13a expose:
`AuthParams(email) (salt, kdfParams string, err error)`,
`Signup(req SignupRequest) (sessionToken string, expiresAt int64, err
error)`, `Login(email, authHash string) (sessionToken string, wrappedMasterKey,
keyWrapParams string, err error)`, `PutBlob(sessionToken, id string,
ciphertext []byte, expectedVersion int) (newVersion int, conflict
*BlobMeta, err error)` (return `conflict` non-nil on a 409, parsed from
the response body, `err` nil in that case — a conflict is an expected
outcome the caller must handle, not a Go error), `GetBlob(sessionToken,
id string) (ciphertext []byte, version int, err error)` (a plain `error`
wrapping "not found" is fine for 404 — check `errors.Is`-ability),
`ListBlobs(sessionToken string) ([]BlobMeta, error)`,
`DeleteBlob(sessionToken, id string) error`. Field/JSON shapes must
match M12/M13a's actual handlers exactly — read their real request/
response bodies in the completed specs' Validation sections (which
have real curl output) rather than re-deriving them from memory.
Hermetic tests against `httptest` fake servers for every method,
including the conflict-body-parsing path and 401/404 handling.

### 2. `internal/cloudsync` — meeting bundle encryption (no network)

- `EncryptMeetingBundle(masterKey []byte, meetingID string, files
  map[string][]byte) (ciphertext []byte, checksum string, err error)`
  and the inverse `DecryptMeetingBundle(masterKey []byte, meetingID
  string, ciphertext []byte) (files map[string][]byte, err error)`.
  Derive a per-meeting key via `hkdf` from `masterKey` with `info =
  "catchm-cloud/meeting/" + meetingID` (don't encrypt every meeting
  with the raw master key directly — per-item key derivation is cheap
  and limits blast radius if one derived key were ever mishandled) —
  add this as a small new function in `internal/e2ee` if that's the
  cleaner home for it (it's a generic "derive a sub-key with a custom
  info string" primitive), then reuse `e2ee.WrapKey`/`UnwrapKey` for
  the actual AEAD step — don't reimplement XChaCha20-Poly1305 handling
  here.
- Bundle format: a simple length-prefixed framing of `{filename,
  content}` pairs (filename as a length-prefixed string, content as a
  length-prefixed byte slice, repeated) — no `archive/tar`, no new
  dependency, this is an internal wire format only your own code reads.
  Which files: `meeting.md` and `utterances.jsonl` always; include
  `utterances.v2.jsonl` if it exists on disk. **Not** `audio.wav`
  (explicitly out of scope, per the architecture doc — audio sync is a
  future concern, meeting text/metadata is what this milestone syncs).
- `checksum` is the sha256 hex of the ciphertext — matches what
  `PUT /blobs/:id`'s `X-Checksum` header expects.
- Tests: round-trip encrypt→decrypt recovers the exact original file
  contents; wrong master key fails to decrypt (via the underlying
  `UnwrapKey`'s AEAD check); a meeting with only `meeting.md` +
  `utterances.jsonl` (no v2 file) round-trips correctly (don't assume
  v2 always exists).

### 3. Sync orchestration

`Sync(ctx, store *meeting.Store, client *cloudsync.Client, sessionToken
string, masterKey []byte) (*SyncResult, error)` (exact signature your
call, this is the shape of the idea):

- Maintain a small local manifest — `<CATCHM_HOME>/cloud-sync-state.json`,
  `map[meetingID]{RemoteVersion int, PushedChecksum string}` — tracking
  what this device last successfully pushed for each meeting, so it
  can tell "unchanged since last push" from "needs pushing" without
  re-fetching from the server every time. This file has no secrets in
  it (versions and checksums only), fine to store in plaintext.
- For each local meeting (from `store.List()`): read+bundle its files,
  compute the ciphertext+checksum. If the manifest's `PushedChecksum`
  for this meeting matches the freshly-computed one, skip it (nothing
  changed locally). Otherwise `PutBlob` with `expectedVersion` = the
  manifest's last-known `RemoteVersion` (0 if never synced).
  - On success: update the manifest with the new version/checksum.
  - On a 409 conflict: this means some other device (or a stale local
    state) pushed a version we don't know about. Fetch the real remote
    blob (`GetBlob`), decrypt it, and write the **local** divergent
    version to `<meeting-dir>/conflict-<timestamp>.md` (or a small
    marker file — this is a conflict SIGNAL for the user to notice and
    manually reconcile, not automatic merging, per the architecture
    doc's LWW-plus-conflict-copy design) — then adopt the remote
    version locally (overwrite `meeting.md`/`utterances.jsonl` with
    the decrypted remote content) and update the manifest to the
    remote version. Do not silently discard either side's data.
- After pushing, call `ListBlobs` and pull down any blob whose id
  isn't a local meeting at all yet (a meeting that exists on another
  device but not here) — decrypt and materialize it as a new local
  meeting directory via the store. (Meetings deleted remotely but
  still present locally, and vice versa, are explicitly **out of
  scope** for this milestone — pure create/update sync only; deletion
  propagation is a real design question — LWW-delete needs a tombstone
  — deferred rather than guessed at here.)
- Return a `SyncResult` summarizing what happened (pushed/pulled/
  conflicted meeting IDs) — the eventual UI (M13d) will want this,
  design the shape with that in mind even though nothing renders it yet.

Test this with two independent `meeting.Store` instances (simulating
two devices) both pointed at the same `httptest`-backed fake
`catchm-cloud` server (or, if that's awkward to fake well enough,
against a real `catchm-cloud` `wrangler dev` instance started as part
of the test setup — your call on which is more reliable to get right,
record it): push from device A, sync device B, confirm B receives A's
meeting decrypted correctly; modify on both without syncing in
between, sync both, confirm the conflict-copy file appears on whichever
side lost the race and the winning side's content is what's live.

### 4. `internal/server` wiring

New endpoints, holding a `*cloudsync.Client` plus the current
session/master-key **in memory only** (never written to disk in any
form — a server restart requires the user to log in again; this is a
deliberate simplicity choice, record it in Decisions if you reconsider
it): `POST /api/cloud/signup {email, password}`,
`POST /api/cloud/login {email, password}`, `POST /api/cloud/logout`,
`POST /api/cloud/sync` (runs the orchestration above, returns
`SyncResult`), `GET /api/cloud/status` (logged in? email? last sync
time?). On successful signup/login, persist the non-secret
`{email, auth_salt, kdf_params, api_base_url}` into
`internal/config`'s `Config` (add a `Cloud` section — follow the exact
`setIfNonEmpty`-on-PUT pattern already established for `Soniox`/
`Translate` config in M6 if you add a way to edit `api_base_url` via
`PUT /api/config` too, or keep it signup/login-managed only if that's
simpler — your call, record it). Signup additionally returns the
`RecoveryCode` in the HTTP response **once** (never persisted
server-side or client-side in `config.yaml` — this is the user's
responsibility to save, matching 1Password's Emergency Kit UX from the
architecture doc's research).

## Out of scope for M13c

Any web UI (M13d), device linking (M14, already planned separately),
the `/recover` flow (M15), audio.wav sync, deletion propagation/
tombstones, delta sync (whole-file only, per the architecture doc),
multi-account support (one cloud account per local CatchM install).

## Acceptance criteria

1. `go build ./... && go vet ./... && gofmt -l .` clean; `go test
   ./internal/cloudsync/... ./internal/e2ee/... ./internal/server/...`
   green.
2. `internal/cloudsync`'s HTTP client methods each have a hermetic test
   against a fake `catchm-cloud` (httptest), covering success and at
   least one failure mode (401, 404, or 409 as applicable) per method.
3. `TestEncryptDecryptMeetingBundleRoundTrips`,
   `TestDecryptFailsWithWrongMasterKey`,
   `TestBundleWithoutV2FileRoundTrips`.
4. `TestSyncPushesNewMeeting`, `TestSyncPullsMeetingFromOtherDevice`,
   `TestSyncDetectsConflictAndPreservesBothVersions` (the two-Store
   simulation described above).
5. `internal/server` endpoint tests: signup/login persist the right
   config fields and never leak the master key or session token into
   any response body beyond what's needed; `/api/cloud/sync` triggers
   the orchestration (can use a fake `cloudsync.Client`/httptest
   backend, doesn't need `catchm-cloud` running for this specific
   test).
6. Manual/live verification: with a real `catchm-cloud` `wrangler dev`
   running, exercise signup → create a local meeting → sync → confirm
   (via `catchm-cloud`'s own `GET /blobs`) a blob exists → decrypt it
   manually with the known master key to confirm it's real ciphertext,
   not plaintext, sitting server-side. Paste the actual commands/output
   into Validation.

## Decisions

- **Bundle wire format**: the plaintext fed to `e2ee.WrapKey` (XChaCha20-Poly1305, per M13b) is a sequence of `[4-byte big-endian filename length][filename][4-byte big-endian content length][content]` entries, one per file, files in sorted-name order so the same content always marshals identically. Files bundled: `meeting.md` + `utterances.jsonl` always, `utterances.v2.jsonl` when present; never `audio.wav`. The ciphertext blob is `24-byte nonce || ciphertext || 16-byte tag` (M13b's WrapKey layout).
- **Per-meeting key derivation**: `e2ee.DeriveSubKey(masterKey, "catchm-cloud/meeting/"+meetingID)` — a new exported generalization of M13b's internal HKDF helper (HKDF-SHA256, 32 bytes). Added to `internal/e2ee` rather than cloudsync so all key derivation lives in one audited place; DeriveKeyWrapKey is now `DeriveSubKey(key, "catchm-cloud/key-wrap")` in effect (test asserts equivalence).
- **Manifest**: `<CATCHM_HOME>/cloud-sync-state.json`, `map[meetingID]{remote_version, pushed_checksum}`. `pushed_checksum` is sha256 of the *marshaled plaintext bundle*, not the ciphertext — a fresh AEAD nonce would otherwise make every encryption look changed. Saved only after a fully successful sync run; a mid-run error leaves the previous manifest so the next run re-attempts through the same paths. No secrets (versions + checksums only), plaintext storage is fine.
- **Conflict copies**: on a lost push race, the local divergent files are copied inside the meeting's own directory as `conflict-<unix-milli>.md` / `conflict-<unix-milli>.jsonl` / `conflict-<unix-milli>.v2.jsonl` (v2 included even though the spec named only the .md/.jsonl pair — dropping a diverged batch transcript would be silent data loss), then remote content overwrites the live files. Spec's `conflict-<timestamp>.md` naming kept; Unix-milli chosen to avoid same-second collisions.
- **KeyWrapParams**: dropped as a distinct value everywhere. `key_wrap_key` re-derives from the same Argon2id run as `auth_hash` (M13b Decisions), so signup sends a literal `"{}"` for the Worker's `key_wrap_params` column and login ignores the field. Recorded here because the Worker schema keeps the column.
- **Pull-phase update handling** (beyond spec): the spec's pull loop only covered "remote id with no local meeting"; that fails the B-edits-then-A-syncs case (caught by TestSyncPullsThenPushesRoundTrip). Sync now also adopts remote-newer versions of known meetings when local content matches the last push, and falls back to the conflict path when local content diverged — never a silent overwrite.
- **`api_base_url`**: stayed signup/login-managed (not added to `PUT /api/config`) — simpler, and the field is an account property, not a general setting. Defaults to `http://127.0.0.1:8787` (wrangler dev) in `config.defaults()` until a deployed URL exists.
- **Conflict-test strategy**: two independent `meeting.Store`s with separate `CATCHM_HOME`s against one shared httptest fake Worker (the same stateful fake client_test.go uses), not a live wrangler dev — deterministic, hermetic, and the fake mirrors the real handlers' exact response shapes from M12/M13a's curl-validated specs. Live wrangler dev verification happens once, manually (Validation below), not per-test-run.
- **Session/master-key storage**: `Server.cloud` — a struct holding `{email, sessionToken, masterKey, loggedInAt, lastSyncAt}` guarded by the server's existing mutex, in memory only, nil on logout. A server restart requires login again; deliberate simplicity (persisting the master key reopens the where-does-the-wrapping-key-live question; M14 device-linking is the real answer). A failed login does NOT clear an existing session (a password typo shouldn't log you out); logout does.
- **`adoptRemote` id validation**: added `validMeetingID` as its first check (mirroring `materializeMeeting`) after review flagged it as a latent path-traversal footgun, plus a regression test.
- **Post-hoc note (M15)**: `recovery_salt` was never sent to or stored by the server in this milestone — recovery from a genuinely new device was silently broken since M13b (a fresh device had no way to obtain the salt). Fixed in M15 (migration 0003 + SignupRequest fields + `/recovery-params`); recorded here so the history is honest.
## Progress

- [x] internal/cloudsync HTTP client (signup/login/auth-params/blob CRUD)
- [x] Meeting bundle encrypt/decrypt (+ per-meeting HKDF key derivation)
- [x] Sync orchestration (push/pull/conflict, local manifest)
- [x] internal/server endpoints + config persistence
- [x] Two-device conflict test
- [x] Live verification against real catchm-cloud wrangler dev
## Validation

All confirmed 2026-09-21. `go build ./... && go vet ./... && gofmt -l . && go test ./...` clean across the repo; targeted suites: cloudsync 27/27 (14 client, 6 bundle, 7 sync incl. two-device conflict + no-utterances regression), e2ee 20/20, server cloud endpoints 9/9.

**Live verification (criterion 6)** — real `catchm-cloud` wrangler dev on :8787 (local D1+R2) + real `catchm serve` on :14649 with `CATCHM_HOME=/tmp/catchm-live`; no api_base_url override needed since `config.defaults()` already points at `http://127.0.0.1:8787`:

```
$ curl -s -X POST :14649/api/cloud/signup -d '{"email":"verify@example.com","password":"verify-pass-123"}'
{"email":"verify@example.com","recovery_code":"DFM9C-NJU2C-3UYYD-YVFZ4-G9R8J"}

$ curl -s -X POST :14649/api/meetings -d '{"title":"Live sync test"}'
{"schema_version":1,"id":"6d714a64cbfc9de2","title":"Live sync test","started_at":"2026-09-20T19:05:44.643343Z"}

$ curl -s -X POST :14649/api/cloud/sync
{"pushed":["6d714a64cbfc9de2"],"pulled":null,"conflicted":null}

$ curl -s -X POST :14649/api/cloud/sync   # second run: no-op, manifest skip works
{"pushed":null,"pulled":null,"conflicted":null}
```

Then a throwaway helper (deleted after the run) logged into the Worker with only email+password — the exact second-device path — listed blobs, downloaded the ciphertext, and decrypted:

```
$ go run ./cmd/tmplivedecrypt http://localhost:8787 verify@example.com verify-pass-123 6d714a64cbfc9de2
login: ok (token 883ddbbd...)
GET /blobs: [{"id":"6d714a64cbfc9de2","version":1,"checksum":"d17663f4...b3","size_bytes":277,"updated_at":1789931144664}]
GET /blobs/6d714a64cbfc9de2: 277 bytes, version 1
ciphertext head (hex): a0 58 d2 1e de de b4 d3 27 66 40 e4 6d 58 39 43 9e f3 56 0d f6 55 5e fe
decrypted meeting.md (195 bytes), title present: true
--- decrypted meeting.md ---
---
schema_version: 1
id: 6d714a64cbfc9de2
title: Live sync test
started_at: 2026-09-20T19:05:44.643343Z
---
<!-- catchm meeting document; frontmatter is the query surface -->
# Live sync test
```

The helper also asserted the plaintext title does NOT appear in the ciphertext (it would have exited non-zero). This proves: the Worker holds only ciphertext, the full key chain (password → Argon2id → auth_hash/key_wrap_key → unwrap master key → per-meeting HKDF → XChaCha20 AEAD) round-trips through the real backend, and GET /blobs metadata matches what sync pushed.

**Bug found and fixed by this live run**: a freshly created meeting (no utterances yet → no utterances.jsonl on disk) failed sync with a read error; `readMeetingFiles` now bundles a missing live transcript as empty content. Regression test `TestSyncPushesMeetingWithNoUtterances` added (commit 4eea2f0). Every prior hermetic test created meetings via `store.Append`, so the transcript file always existed — only the live flow caught it.
