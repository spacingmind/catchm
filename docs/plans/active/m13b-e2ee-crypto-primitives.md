# Plan: M13b — Go E2EE crypto primitives

- Status: active
- Created: 2026-09-20
- Repo: `catchm` (this repo, the Go client) — NOT `catchm-cloud`.

## Goal

The client-side cryptography that makes the whole cloud-sync feature
end-to-end encrypted: key derivation, key wrapping, and recovery-code
wrapping. No networking, no meeting-data integration, no UI in this
milestone — pure, exhaustively-tested primitives that M13c will call.
This is explicitly the highest-stakes correctness surface in the whole
cloud-sync feature per `docs/cloud-sync-architecture.md` — a bug here
means either silent data loss (can't decrypt your own data) or a
security hole (weak/predictable keys), not a UI glitch. Treat test
coverage accordingly: more thorough than typical for this codebase,
including deliberately adversarial cases (wrong key, tampered
ciphertext, wrong salt), not just the happy path.

Read `docs/cloud-sync-architecture.md` in full before starting — it
specifies exactly which primitives compose into which values
(`master_key`, `auth_key`, `auth_hash`, `key_wrap_key`,
`wrapped_master_key`, `recovery_key`, `recovery_blob`) and why each
step exists. Do not deviate from that composition without recording
why in Decisions.

## Scope

New package `internal/e2ee`. Use `golang.org/x/crypto` (already an
indirect dependency of this module via other packages, or add it
directly — check `go.mod` first) for `argon2`, `hkdf`, and
`chacha20poly1305`. Do not hand-roll any cryptographic primitive
(KDF, cipher, MAC) — only compose well-vetted library functions.

- `GenerateMasterKey() []byte` — 32 random bytes via `crypto/rand`
  (not `math/rand`).
- `DeriveAuthKey(password, salt []byte) (authKey []byte, params
  KDFParams)` — Argon2id. `KDFParams` is a small struct (time,
  memory, parallelism, key length — whatever Argon2id needs) that gets
  JSON-marshaled as the opaque `kdf_params` string the Worker stores;
  define sane default parameters (look up currently-recommended
  Argon2id parameters — e.g. OWASP's current guidance — rather than
  guessing round numbers) and export them as a package-level default so
  callers don't have to invent their own.
- `DeriveAuthHash(authKey []byte) []byte` (or fold into
  `DeriveAuthKey` if that reads cleaner — your call, record it) — what
  actually gets sent to the Worker as `auth_hash`. Per the architecture
  doc, this must NOT be `authKey` itself reused verbatim as both the
  wire-sent auth proof AND the local key-wrapping secret (if the
  Worker's stored `auth_hash` were ever leaked, it should not directly
  hand an attacker the key-wrap key) — derive both `auth_hash` and
  `key_wrap_key` from `authKey` via HKDF with distinct `info` strings
  (e.g. `"catchm-cloud/auth-hash"` vs `"catchm-cloud/key-wrap"`), so
  they're cryptographically independent outputs of the same KDF step.
- `WrapKey(plaintextKey, wrappingKey []byte) (wrapped []byte, err
  error)` / `UnwrapKey(wrapped, wrappingKey []byte) (plaintextKey
  []byte, err error)` — XChaCha20-Poly1305 AEAD (24-byte random nonce
  prepended to the ciphertext, standard construction — check
  `golang.org/x/crypto/chacha20poly1305`'s actual API, particularly
  `NewX` for the extended-nonce variant, don't guess the constructor
  name). `UnwrapKey` must return an error (not panic, not silently
  return garbage) on: wrong `wrappingKey`, truncated/corrupted input,
  or tampered ciphertext (AEAD auth tag failure) — write a test for
  each of these three failure modes specifically, not just "wrong key
  fails."
- `GenerateRecoveryCode() (code string, err error)` — high-entropy,
  human-transcribable. Per the architecture doc's still-open question
  (word-list mnemonic vs. raw random string), default to a word-list
  mnemonic if you have (or can vendor/generate) a reasonable wordlist
  without pulling in a heavy dependency; if that's impractical without
  a new dependency you're not confident about, fall back to a
  formatted random string (e.g. groups of 4-6 alphanumeric chars
  separated by dashes, all uppercase to reduce transcription
  ambiguity) and record which you picked and why in Decisions — either
  is acceptable, just make the call rather than blocking.
- `DeriveRecoveryKey(recoveryCode string, salt []byte) (recoveryKey
  []byte, params KDFParams)` — same Argon2id treatment as the password
  path (recovery codes are high-entropy already, but consistency with
  the password path is simpler to reason about and test than a
  different KDF for this one case).
- Wire it together in two small end-to-end helper functions that
  mirror exactly what the signup/login flow in M13c will need, so
  M13c is mostly plumbing, not crypto decisions:
  - `SetupAccount(password string) (*AccountSecrets, error)` — a
    struct bundling everything a signup call needs to send:
    `MasterKey` (kept, never sent), `AuthSalt`, `AuthHash`,
    `KDFParams`, `WrappedMasterKey`, `KeyWrapParams`, `RecoveryCode`
    (shown to the user once, not stored), `RecoveryBlob`,
    `RecoveryKDFParams`.
  - `UnlockWithPassword(password string, salt []byte, params
    KDFParams, wrappedMasterKey []byte, keyWrapParams KDFParams)
    ([]byte, error)` — returns the unwrapped `master_key` or an error;
    this is what a login response's fields feed into.
  - `RecoverMasterKey(recoveryCode string, salt []byte, params
    KDFParams, recoveryBlob []byte) ([]byte, error)` — same idea for
    the recovery path.

## Out of scope for M13b

Any HTTP client code, any interaction with `internal/meeting`'s actual
data, any UI, device-linking crypto (M14), storing anything in
`~/.catchm/config.yaml` (M13c's job).

## Acceptance criteria

1. `go build ./... && go vet ./... && gofmt -l .` clean; `go test
   ./internal/e2ee/...` green.
2. `TestSetupAccountThenUnlockRoundTrips` — `SetupAccount` followed by
   `UnlockWithPassword` with the correct password recovers the exact
   same `MasterKey` bytes.
3. `TestUnlockFailsWithWrongPassword` — wrong password →
   `UnlockWithPassword` returns an error, never a wrong-but-plausible
   key.
4. `TestUnwrapFailsOnTamperedCiphertext` — flip a bit in the wrapped
   key's ciphertext → `UnwrapKey`/`UnlockWithPassword` returns an
   error (AEAD tag check catches it), doesn't silently return garbage
   bytes.
5. `TestUnwrapFailsOnTruncatedInput` — a wrapped value shorter than a
   valid nonce+ciphertext → clean error, no panic/index-out-of-range.
6. `TestRecoveryRoundTrips` — `SetupAccount`'s `RecoveryCode` +
   `RecoveryBlob` recover the same `MasterKey` via `RecoverMasterKey`.
7. `TestRecoveryFailsWithWrongCode`.
8. `TestDeriveAuthHashAndKeyWrapKeyAreIndependent` — given the same
   `authKey`, the derived `auth_hash` and `key_wrap_key` are different
   values (proving the HKDF `info`-string separation actually works,
   not just calling the same function twice by accident).
9. `TestSameInputsDeterministic` — same password+salt+params always
   derives the same `authKey`/`auth_hash` (login depends on this being
   true — a non-deterministic KDF call would make login impossible).
10. `TestDifferentSaltsProduceDifferentKeys` — same password, different
    salt, different `authKey` (catches an accidentally-ignored salt
    parameter).

## Decisions

(fill in: exact Argon2id parameters chosen and source for "current
recommended" figures, whether `auth_hash` is a separate HKDF output or
folded differently, recovery code format chosen (wordlist vs. random
string) and why, exact XChaCha20-Poly1305 wrapped-value wire format —
nonce-prepended-to-ciphertext or a small struct with explicit fields,
your call, just be consistent and document it since M13c depends on
the exact byte layout.)

## Progress

- [ ] GenerateMasterKey
- [ ] DeriveAuthKey / DeriveAuthHash (Argon2id + HKDF separation)
- [ ] WrapKey / UnwrapKey (XChaCha20-Poly1305)
- [ ] GenerateRecoveryCode / DeriveRecoveryKey
- [ ] SetupAccount / UnlockWithPassword / RecoverMasterKey wiring
- [ ] Full adversarial test suite (tamper, truncation, wrong key/password/code)

## Validation

(to be filled as criteria are confirmed)
