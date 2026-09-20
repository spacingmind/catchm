# Plan: M15 — Recovery-code account recovery (`/recover` flow)

- Status: active
- Created: 2026-09-21
- Repos: **both** `catchm` (this repo — client crypto, HTTP client,
  server endpoint, web UI) and `catchm-cloud` (separate repo —
  migration + two new endpoints). Read `docs/cloud-sync-architecture.md`
  and all of `docs/plans/completed/m12-*` through `m13d-*` before
  starting; this milestone extends already-completed work rather than
  starting fresh.

## Goal

Let a user who has lost their password (and has no other logged-in
device) get back into their account using only their recovery code,
set a new password, and keep their data — without ever giving the
server anything that lets it decrypt their meetings. Per the
architecture doc's M15 line item: "generation, display-once UX, the
`/recover` flow and re-wrap-on-recovery logic." Generation and
display-once UX already shipped in M13b/M13d; this milestone is the
`/recover` flow and re-wrap.

Note the architecture doc also flagged "recovery code format: raw vs.
word-list, deferred to M15" — that's moot, M13b already shipped and
tested a 32-symbol unambiguous-alphabet format (`GenerateRecoveryCode`)
and it's live in M13d's UI. Don't revisit it here.

## Why this needs a crypto change, not just new endpoints

Read `internal/e2ee/e2ee.go`'s current `SetupAccount` closely first.
Today `RecoveryBlob = WrapKey(masterKey, recoveryKey)` — the recovery
blob is wrapped directly with the raw key derived from the recovery
code, with no HKDF split. That's fine for *decrypting* (what M13c/M13d
use it for locally) but there is no way for the server to verify "this
client actually knows the recovery code" the way it verifies a
password today via `auth_hash` — and `/recover` needs exactly that
check, symmetrically to how `/login` checks `auth_hash` before handing
back `wrapped_master_key`. Reusing the raw `recoveryKey` as both a
server-verifiable secret *and* a local decryption key would mean
sending something derived from it to the server for verification,
weakening the key doing double duty — the same reason `auth_hash` and
`key_wrap_key` are already HKDF-split from one `authKey` instead of
reusing `authKey` directly.

Mirror that split for recovery: derive two independent sub-keys from
`recoveryKey` via `e2ee.DeriveSubKey` (already added in M13c) with
distinct info strings — one for a server-verifiable
`recovery_auth_hash` (never used to decrypt anything), one to actually
wrap/unwrap `recovery_blob` (never sent to the server). No real
accounts exist yet (nothing is deployed for real users), so changing
`AccountSecrets`' shape and `SetupAccount`/`RecoverMasterKey`'s
internals here is a normal in-place evolution, not a breaking-change/
migration problem — update `m13b`'s and `m12`'s completed spec docs
with a short note pointing here if their Decisions sections describe
the old shape, rather than leaving them describing dead code.

## Scope

### 1. `internal/e2ee` changes

- Add `DeriveRecoveryAuthHash(recoveryKey []byte) []byte` and
  `DeriveRecoveryWrapKey(recoveryKey []byte) []byte` (both via
  `DeriveSubKey` with distinct info strings, mirroring
  `DeriveAuthHash`/`DeriveKeyWrapKey`'s relationship to `authKey`).
- `SetupAccount`: wrap `RecoveryBlob` with `DeriveRecoveryWrapKey(recoveryKey)`
  instead of the raw `recoveryKey`; add `RecoveryAuthHash []byte` to
  `AccountSecrets`, computed via `DeriveRecoveryAuthHash(recoveryKey)`.
- `RecoverMasterKey`: unwrap using `DeriveRecoveryWrapKey(recoveryKey)`
  to match.
- New `RewrapForPassword(masterKey []byte, password string) (*PasswordWrap, error)`
  extracting the password-wrapping half of `SetupAccount` into its own
  function (returns `{AuthSalt, AuthHash, KDFParams, WrappedMasterKey}`
  — name it what fits) — refactor `SetupAccount` to call it, so the
  reset-password step (which already *has* a master key, from a
  successful recovery, and doesn't need a new one) reuses the exact
  same derivation instead of a parallel hand-rolled copy.
- New `RewrapForRecovery(masterKey []byte) (*RecoveryWrap, error)`,
  same idea for the recovery half — generates a **fresh** recovery
  code/salt/blob/auth-hash. Reset-password rotates the recovery code
  (single-use in practice: after a real recovery, the old code should
  stop working, same reasoning as rotating a backup 2FA code after
  use) — refactor `SetupAccount` to call this too.
- Full test coverage for all of the above, including a regression test
  that `RecoverMasterKey` still round-trips end-to-end through
  `SetupAccount`'s new shape, and that the old raw-`recoveryKey`-wrap
  behavior is gone (a blob wrapped with the raw key must NOT unwrap
  with the new function, proving the sub-key split is real, not
  cosmetic).

### 2. `catchm-cloud` changes (separate repo)

- Migration `0003_recovery_auth.sql`: add `recovery_auth_hash` column
  to `e2ee_profiles`.
- `POST /signup`: accept and store the new `recovery_auth_hash` field
  (alongside the existing `recovery_blob`/`recovery_kdf_params`).
- `GET /recovery-params?email=` — mirrors `/auth-params`'s
  anti-enumeration shape exactly (deterministic fake salt/params for
  unknown emails), returns `{recovery_salt, recovery_kdf_params}`.
- `POST /recover` — body `{email, recovery_auth_hash}`. Constant-time
  compare against the stored hash (reuse the existing `timingSafeEqual`
  helper from `src/crypto.ts`). Unknown email or wrong hash → the same
  401 shape as `/login`'s wrong-credentials case (indistinguishable,
  same anti-enumeration reasoning already applied to login/auth-params).
  On match: issue a normal session token (same mechanism as login) and
  return it plus the account's `recovery_blob` and `recovery_kdf_params`
  (the client needs the blob to unwrap the master key locally — it
  isn't exposed anywhere else).
- `POST /account/reset-password` — session-authenticated (reuse
  `requireSession`). Body carries the **new** `auth_hash`, `auth_salt`,
  `kdf_params`, `wrapped_master_key` (from `RewrapForPassword`) **and**
  the **new** `recovery_blob`, `recovery_salt`, `recovery_kdf_params`,
  `recovery_auth_hash` (from `RewrapForRecovery`) — overwrites all of
  it in one transaction. This is the "re-wrap-on-recovery" step from
  the architecture doc: old password stops working, old recovery code
  stops working, both replaced atomically.
- Hermetic `@cloudflare/vitest-pool-workers` tests for every new/changed
  route, covering: successful recover→reset-password round trip, wrong
  recovery hash rejected, unknown email indistinguishable from wrong
  hash, reset-password requires a valid session, old auth_hash/old
  recovery_auth_hash both rejected after a reset.

### 3. `internal/cloudsync` (this repo) — new `Client` methods

`RecoveryParams(email) (salt, kdfParams string, err error)`,
`Recover(email, recoveryAuthHash string) (sessionToken, recoveryBlob, recoveryKdfParams string, err error)`,
`ResetPassword(sessionToken string, fields ResetPasswordRequest) error`
— field names must match whatever `catchm-cloud` actually implements
(check the real code, same discipline as M13c). Hermetic httptest-based
tests per method, success + failure paths.

### 4. `internal/server` — one orchestration endpoint

`POST /api/cloud/recover {email, recovery_code, new_password}`:
`RecoveryParams` → derive `recoveryKey` locally → derive
`recovery_auth_hash` locally → `Recover` → `RecoverMasterKey`-equivalent
unwrap of the returned `recovery_blob` (wrong code fails here, cleanly,
before any reset-password call is even attempted) → `RewrapForPassword`
+ `RewrapForRecovery` on the now-known master key → `ResetPassword` →
store the new session in `s.cloud` (logged in immediately, no separate
login step needed) → respond with `{email, recovery_code}` (the **new**
rotated code, same one-time-display contract as signup — the caller,
i.e. the web UI, must treat it identically). Test with a fake
`cloudsync.Client` / httptest backend: successful recovery, wrong
recovery code fails before touching reset-password, and a full
signup→recover→login-with-new-password chain proving the new password
actually works and the old one doesn't.

### 5. Web UI — `web/src/components/SettingsCloud.tsx`

Add a "Forgot password?" link/toggle on the logged-out form that swaps
in a recovery form: email, recovery code, new password. On success,
show the **new** recovery code in the exact same locked-dialog
component/UX already built in M13d for signup (reuse it, don't
duplicate) — same three constraints (explicit-ack-only dismissal, no
timer, never persisted) — then transition to the logged-in view.
Wrong recovery code shows an inline error, doesn't clear the email/
recovery-code/new-password fields the user already typed (matches
`SettingsTranslate`/existing form conventions on retry-without-losing-
input, check how login already handles this).

## Out of scope

Device linking (M14, separate milestone, different problem — that's
for a *currently logged-in* device authorizing a new one; recovery is
for when there's no logged-in device left at all). Rate-limiting
`/recover` attempts server-side (worth flagging as a real gap in
Decisions if not done, but a backend hardening concern that can follow
later rather than block this milestone). Recovering when the account
itself doesn't exist (already covered by existing anti-enumeration
behavior — no new work).

## Acceptance criteria

1. `go build ./... && go vet ./... && gofmt -l . && go test ./...`
   clean in `catchm`; `npm run typecheck`/`vitest run`/`tsc --noEmit`
   (whichever `catchm-cloud`'s `package.json` defines) clean there too.
2. `internal/e2ee` tests prove the sub-key split is real (see section 1's
   last bullet), not just that the happy path still works.
3. `catchm-cloud` hermetic tests cover the full recover→reset-password
   flow plus every failure mode listed in section 2's last bullet.
4. `internal/cloudsync` and `internal/server` tests cover the
   orchestration, including "wrong recovery code never reaches
   reset-password."
5. Live verification against a real `wrangler dev`: signup → note the
   recovery code → simulate "lost password" (don't call `/login` with
   the real password again) → call `/api/cloud/recover` with the
   recovery code and a new password → confirm login with the new
   password succeeds and the *old* password now fails → confirm the
   *old* recovery code now fails a second `/recover` call (proving
   rotation) → confirm a meeting synced before recovery is still
   readable after (proving the master key itself never changed, only
   its wrapping). Paste real commands/output into Validation.

## Decisions

(fill in: exact info strings for `DeriveRecoveryAuthHash`/
`DeriveRecoveryWrapKey`, exact request/response JSON shapes for the
three new/changed `catchm-cloud` routes, whether reset-password is one
DB transaction or sequential writes, how `internal/server`'s recover
handler reports "wrong code" vs. "wrong new-password constraints" vs.
network errors distinctly to the UI, note on updating M12/M13b's
completed docs to reflect the crypto shape change here.)

## Progress

- [ ] internal/e2ee: sub-key split, RewrapForPassword, RewrapForRecovery
- [ ] catchm-cloud: migration + recovery-params + recover + reset-password
- [ ] internal/cloudsync: RecoveryParams/Recover/ResetPassword client methods
- [ ] internal/server: POST /api/cloud/recover orchestration
- [ ] web UI: recovery form + reused recovery-code dialog
- [ ] Live wrangler-dev verification (full recover→rotate→re-verify chain)

## Validation

(to be filled as criteria are confirmed)
