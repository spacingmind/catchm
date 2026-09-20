# Plan: M13c — Go cloud sync client (wiring M12/M13a/M13b together)

- Status: active
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

(fill in: exact bundle wire format byte layout, per-meeting key
derivation info-string, manifest file location/format, conflict-copy
file naming/format, whether `api_base_url` became part of `PUT
/api/config` or stayed signup/login-managed, two-Store vs. real-
wrangler-dev test strategy for the conflict test, session/master-key
in-memory storage mechanism in `internal/server`.)

## Progress

- [ ] internal/cloudsync HTTP client (signup/login/auth-params/blob CRUD)
- [ ] Meeting bundle encrypt/decrypt (+ per-meeting HKDF key derivation)
- [ ] Sync orchestration (push/pull/conflict, local manifest)
- [ ] internal/server endpoints + config persistence
- [ ] Two-device conflict test
- [ ] Live verification against real catchm-cloud wrangler dev

## Validation

(to be filled as criteria are confirmed)
