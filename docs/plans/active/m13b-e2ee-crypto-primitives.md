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

- **Argon2id parameters**: `DefaultKDFParams` = t=2, m=19456 KiB (19 MiB), p=1, keyLen=32 — OWASP Password Storage Cheat Sheet's current minimum-recommended Argon2id configuration (https://cheatsheetseries.owasp.org, Argon2id section). Used for both the password and recovery-code paths.
- **auth_hash / key_wrap_key separation**: both are HKDF-SHA256 expansions of the same Argon2id `authKey` (`hkdf.New(sha256.New, authKey, nil, info)`, no salt, 32 bytes each) with distinct `info` strings: `"catchm-cloud/auth-hash"` and `"catchm-cloud/key-wrap"`. `DeriveAuthHash` is kept as a separate exported function rather than folded into `DeriveAuthKey` so M13c can re-derive `auth_hash` alone for login.
- **Recovery code format**: formatted random string, not a word-list mnemonic — five groups of five characters from a 32-symbol unambiguous alphabet (`A-Z` minus `I`/`O`, digits minus `0`/`1`), dash-separated, ≈125 bits of entropy. A mnemonic would require vendoring a ~2048-word list or a new dependency; the alphabet is exactly 32 symbols so one random byte maps to one character with zero modulo bias.
- **Wrapped-value wire format**: `24-byte random XChaCha20 nonce || AEAD ciphertext || 16-byte Poly1305 tag` — nonce prepended, no struct. Documented in the `WrapKey` doc comment; M13c's `wrapped_master_key` and `recovery_blob` are exactly these bytes.
- **KeyWrapParams dropped as redundant**: the spec listed a `KeyWrapParams` field on `AccountSecrets`, but `key_wrap_key` is HKDF-derived from the same Argon2id run as `auth_hash` — there are no separate wrap-KDF parameters, so the field would be a fake copy of `KDFParams`. Dropped; `UnlockWithPassword` takes only `AuthSalt` + `KDFParams`.
- **RecoverySalt added to `AccountSecrets`**: recovery needs its own salt later (the Worker stores it alongside `recovery_blob`), so the struct carries `RecoverySalt` explicitly rather than leaving M13c to invent one. Salts are 16 random bytes.
## Progress

- [x] GenerateMasterKey
- [x] DeriveAuthKey / DeriveAuthHash (Argon2id + HKDF separation)
- [x] WrapKey / UnwrapKey (XChaCha20-Poly1305)
- [x] GenerateRecoveryCode / DeriveRecoveryKey
- [x] SetupAccount / UnlockWithPassword / RecoverMasterKey wiring
- [x] Full adversarial test suite (tamper, truncation, wrong key/password/code)
## Validation

`go build ./... && go vet ./... && gofmt -l .` clean. `go test ./internal/e2ee/... -v`: 17/17 pass.

All ten acceptance criteria covered:
1. build/vet/gofmt/test green
2. TestSetupAccountThenUnlockRoundTrips
3. TestUnlockFailsWithWrongPassword
4. TestUnwrapFailsOnTamperedCiphertext (4 tamper positions incl. tag byte)
5. TestUnwrapFailsOnTruncatedInput (4 sub-nonce lengths)
6. TestRecoveryRoundTrips
7. TestRecoveryFailsWithWrongCode
8. TestDeriveAuthHashAndKeyWrapKeyAreIndependent
9. TestSameInputsDeterministic
10. TestDifferentSaltsProduceDifferentKeys

Commits: 534f0f1 (GenerateMasterKey/KDFParams), ed23413 (DeriveAuthKey/HKDF), 8738dfb (WrapKey/UnwrapKey), c8f9721 (recovery code/key), cacdf1c (wiring).
