# CatchM Cloud — E2EE account sync architecture

- Status: proposal (not yet an active milestone)
- Date: 2026-09-20
- Supersedes: `docs/research-sync.md` (BYOS) for the sync feature —
  BYOS is explicitly not what's being built; kept for reference only.

## What this is

A shared cloud service, operated by the CatchM maintainer on Cloudflare
(Workers + D1 + R2), that lets one person's meetings sync end-to-end
encrypted across their own devices (phone/laptop/desktop). The operator
runs the infrastructure but — because everything is E2EE client-side —
never has the keys to read anyone's meeting content, only ciphertext
blobs and account metadata (email, device list, timestamps, sizes).

This is a genuine architecture pivot from CatchM's local-only posture
so far: it introduces a real backend the maintainer runs and pays for,
account signup/login, and new non-engineering surface area (privacy
policy, ToS, abuse handling, ongoing hosting cost) that's out of scope
for this doc but real and worth the user's awareness before shipping.

## Security model (pattern: Bitwarden "log in with device" + 1Password
recovery code, both verified against their real docs/whitepapers)

Two secrets exist per account, kept strictly separate:

1. **Auth secret** — proves "this is really the account owner" to the
   Worker, so it'll hand back the account's encrypted data. Derived
   client-side from the master password; the Worker only ever sees the
   already-derived `auth_hash`, never the password itself.
2. **Master key** — a random 32-byte key, generated once on the first
   device, that actually encrypts meeting data. It's never sent to the
   Worker in the clear — only wrapped (encrypted) copies exist server
   -side, and only a device holding the master key can unwrap them.

```
Signup (first device):
  master_password
    → Argon2id → auth_key → (client) → auth_hash  ──▶ stored server-side
    → HKDF(auth_key) → key_wrap_key
  master_key := random 32 bytes                    (never leaves client unwrapped)
  wrapped_master_key := AEAD_seal(master_key, key_wrap_key)  ──▶ stored server-side

Login (same device, or a device that already has the password):
  re-derive auth_key/auth_hash → Worker verifies → returns wrapped_master_key
  re-derive key_wrap_key → unwrap master_key → decrypt blobs
```

**Why Argon2id runs client-side, not in the Worker**: Cloudflare
Workers have strict per-request CPU time limits, and Argon2id is
*deliberately* slow (that's what makes it resistant to offline
brute-force) — running it server-side risks hitting the CPU limit and
also gives the Worker something it should never have anyway (the
password). The client always does the KDF; the Worker only ever
compares/stores derived values.

### Linking a new device (no password re-entry required)

Mirrors Bitwarden's "Log in with device" flow, confirmed against their
security whitepaper — simpler than a QR/Signal-style mutual-signing
flow and sufficient for "sync my own devices," not a multi-party
messaging trust model:

```
New device                         Existing (already-unlocked) device
-----------                        -----------------------------------
generate ephemeral keypair
(link_pub, link_priv)
POST /device-link-requests
  {link_pub}                  ──▶  shows "Link new device?" prompt
                                    (poll or a short-lived request id
                                    the user types/scans from the new
                                    device — a 6-digit code is enough,
                                    no QR library needed for this use
                                    case)
                                    on approve: wraps master_key with
                                    link_pub (HPKE or ECDH+AEAD)
                             ◀──  PUT /device-link-requests/:id
                                    {encrypted_master_key}
GET /device-link-requests/:id
decrypt with link_priv
→ has master_key, registers
  its own long-term device
  keypair
```

The Worker only ever relays `link_pub` and `encrypted_master_key` — it
cannot unwrap either.

### Recovery code (no second device available)

Generated once at signup, shown to the user exactly once (they must
save it themselves — this is the 1Password "Emergency Kit" pattern,
confirmed against their support docs):

```
recovery_code := 24 random chars (or a BIP-39-style word list, TBD)
recovery_key := HKDF(recovery_code)
recovery_blob := AEAD_seal(master_key, recovery_key)   ──▶ stored server-side

Recovery flow:
  user enters email + recovery_code at /recover
  → client derives recovery_key, fetches recovery_blob, unwraps master_key
  → user sets a NEW master password
  → client re-wraps master_key under the new password's key_wrap_key
  → old wrapped_master_key replaced; other devices must re-link
```

## Data model (D1, sketch — will firm up in the M12 spec)

```
users(id, email, auth_hash, auth_salt/params, created_at)
e2ee_profile(user_id, wrapped_master_key, key_derivation_params,
             recovery_blob, recovery_salt/params)
devices(id, user_id, device_pubkey, name, created_at, last_seen_at)
device_link_requests(id, user_id, link_pub, encrypted_master_key,
                      status, expires_at)
blobs(id, user_id, size_bytes, r2_key, version, checksum, created_at)
```

Blob bytes themselves live in R2, not D1 — D1 only holds metadata,
matching R2's intended use (this is the same "server is dumb storage"
principle the BYOS research already established, just with the
maintainer's Cloudflare account as the storage instead of the user's).

## Sync protocol (reuses the BYOS research's conflict model — still the
right call, only the transport/hosting changed)

Whole-file blobs, optimistic concurrency via a version/checksum on each
blob, last-writer-wins with a divergent local copy saved on conflict
(`<name>.catchm-conflict-<ts>.md`) rather than a CRDT — a meeting is
single-writer in practice, so this is proven-sufficient (Obsidian
Sync/Joplin both do the same). No delta sync in v1.

## Proposed milestone breakdown

This is too large for one milestone — splitting the way every prior
CatchM feature has been split, each independently shippable and
testable:

- **M12 — Cloudflare backend skeleton + auth**: Worker + D1 schema,
  `/signup`, `/login`, session handling. No E2EE crypto yet, no client
  integration — just prove the backend works in isolation (its own
  test suite, deployed to a real Cloudflare account for a live check
  the same way Soniox/Ollama got a live check).
- **M13 — Client-side E2EE crypto + blob sync**: Go-side key
  generation/wrapping (`internal/cloudsync` or similar), meeting data
  encryption, push/pull against M12's Worker, conflict handling. This
  is the highest-stakes milestone from a correctness standpoint — bugs
  here mean data loss or a security hole, not just a UI glitch.
- **M14 — Device linking**: the existing-device-approval flow above,
  both Worker endpoints and the two-sided Go/web UI.
- **M15 — Recovery codes**: generation, display-once UX, the
  `/recover` flow and re-wrap-on-recovery logic.

Each milestone gets its own spec doc (same format as M1–M11) when we
get to it, with hermetic tests plus a live-account smoke test before
being marked complete — matching how every STT/translate integration
in this project has actually shipped.

## Open questions / risks worth flagging now (not blocking, but real)

- **Repo layout**: the Worker is TypeScript, a different
  language/runtime from the rest of CatchM (Go + a Vite web app). Does
  it live in `cloud/` inside this repo, or a separate `catchm-cloud`
  repo (matching how some of the user's other projects are split)?
  Proposed default: `cloud/` in this repo for now, since it's small
  and tightly coupled to the client's crypto assumptions — easy to
  split out later if it grows. Flag for confirmation, not blocking M12.
- **Ongoing hosting cost & limits**: Cloudflare's free tier covers a
  meaningful amount of D1/R2/Workers usage, but a "shared cloud service
  for everyone" has no natural ceiling — will need usage limits per
  account eventually (not in M12–M15, but worth having a rough number
  in mind, e.g. "N MB free, then ask users to bring their own R2
  bucket" as a later hybrid option).
- **Non-engineering surface**: a real privacy policy and terms of
  service become necessary once you're custodying other people's
  (encrypted) data and email addresses — outside this doc's scope but
  real before a public launch.
- **Recovery code format**: raw random string vs. a word-list mnemonic
  (easier to write down/read back correctly) — leaning word-list,
  final call deferred to M15.

## What happens to `docs/research-sync.md` (BYOS)

Kept as reference, not deleted — the conflict-resolution model it
worked out (LWW + conflict copies, whole-file-first) carries over
directly to this design. Its `target_fs`/`target_webdav`/`target_s3`
pluggable-backend idea does not apply here (this is one fixed backend,
the maintainer's Cloudflare account) but could resurface later as a
"bring your own bucket" option once there's a usage-limit story to
solve, per the risk note above.
