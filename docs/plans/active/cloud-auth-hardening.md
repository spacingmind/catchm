# Plan: CatchM Cloud auth hardening

- Status: in progress
- Created: 2026-11-27
- Repos:
  - `catchm-api` (`../catchm-cloud`, the Cloudflare Worker + D1) holds the
    protocol and storage;
  - `catchm` (`internal/cloudsync`, `internal/server/cloud.go`, web UI)
    holds the client.

## Why

Today an email and a password are enough to log in:

- nothing checks that the email is real;
- nothing tells the owner when a new device signs in;
- there's no second factor;
- signup answers "email already registered", so anyone can check
  whether an address has an account;
- session tokens are stored as-is in D1;
- the server compares the client's `auth_hash` directly, so a leaked
  database is enough to log in.

## Constraints

CatchM Cloud is end-to-end encrypted. The password derives the master
key on the device, and the server only ever sees `auth_hash`, which the
client derives from the password. So hosted auth (Clerk, Auth0, Supabase
Auth, social login) is out: none of them can produce the encryption key.
We keep the existing password scheme and add layers on top.

## Decisions (owner, 2026-11-27)

- **Email goes through an adapter with a fallback chain**
  (`EMAIL_PROVIDERS="resend,cloudflare"`, in order; owner, 2026-11-27).
  - **Resend** is primary (free tier: 3,000 emails/month, 100/day).
  - **Cloudflare Email Sending** is the fallback. On the Workers Free
    plan it only reaches addresses verified in the Cloudflare account,
    so it can't reach users yet. A failed fallback is logged and the
    request answers 503; it never reports success. Once the account is
    on Workers Paid ($5/month, 3,000 emails included), the fallback
    works without code changes.
  - The order and the provider set are config only.
- **An unverified email can't log in at all.** This covers accounts
  created before this change: they verify with an emailed code at their
  next login.
- **Scope of this round:**
  - hygiene fixes;
  - new-device alerts;
  - optional TOTP two-factor.
- **Not this round:** Turnstile (the desktop app has no web origin to put
  it on) and passkeys (they can't derive the E2EE key without PRF).

## Worker design (`catchm-api`)

### Email adapter (`src/email/`)

```ts
interface EmailMessage { to: string; subject: string; text: string; html?: string }
interface EmailSender { send(msg: EmailMessage): Promise<void> }
```

- `ResendSender` posts to `https://api.resend.com/emails` with
  `Authorization: Bearer RESEND_API_KEY` and `from` set to `EMAIL_FROM`.
- `CloudflareSender` uses the Email Service `send_email` binding
  (`EMAIL`), following developers.cloudflare.com/email-service.
- `LogSender` is for local dev and tests: it records messages and
  sends nothing.
- `FallbackSender(senders)` tries each sender in order:
  - **when it moves on:** only on errors that mean "not sent" (network
    failure, 429, 5xx, a provider that isn't configured);
  - **when it stops:** a 4xx such as a rejected address stops the chain,
    since the next provider would reject it too;
  - **idempotency:** every attempt for a message carries the same
    `Idempotency-Key`, which Resend honours, so a retry after an
    ambiguous failure can't send twice;
  - **logging:** each failure is logged with its provider.
- `createEmailSender(env)` builds the chain from `EMAIL_PROVIDERS`
  (default `resend,cloudflare`), skipping providers that have no config
  (no `RESEND_API_KEY`, no `EMAIL` binding). An empty chain fails
  closed: endpoints that need email answer 503.

Templates are plain text with a minimal HTML twin, in English (the app's
UI language isn't known server-side yet):

- verification code;
- new-device alert;
- "someone tried to sign up with your email";
- 2FA turned on or off.

### Storage (migration `0006_auth_hardening.sql`)

- **`users`:**
  - `email_verified_at INTEGER` (NULL means unverified);
  - `auth_hash_v INTEGER DEFAULT 1`. Version 1 is the raw client value;
    version 2 is `HMAC-SHA256(AUTH_PEPPER, auth_hash)`. Rows are rehashed
    at the next successful login, and new signups store v2.
  - `totp_secret_enc TEXT` (AES-GCM under the `TOTP_KEY` secret);
  - `totp_enabled_at INTEGER`.
- **`sessions`:**
  - stored as `token_hash` (SHA-256 of the token) instead of the token
    itself. Existing rows are rehashed in the migration (SHA-256 in SQL
    isn't available, so the migration drops them and users log in
    again: documented);
  - plus `id`, `device_id`, `device_name`, `last_seen_at`.
- **`email_codes`:** `user_id`, `purpose` (verify | login), `code_hash`,
  `expires_at` (15 min), `attempts` (max 5), `sent_at`.
- **`known_devices`:** `user_id`, `device_id`, `first_seen_at`.

### Protocol

**`POST /signup`:**
- Same body as today, plus `device_id` and `device_name`.
- It creates an *unverified* account (auth_hash stored as v2) and sends
  a verification code. It returns **202 `{verification_required:true}`**
  and no session.
- An email that already has an account gets the **same 202**. The owner
  receives "someone tried to sign up" and nothing is created
  (no enumeration).

**`POST /login`** body: `{email, auth_hash, device_id, device_name,
email_code?, totp_code?}`.

1. Wrong credentials → `401 invalid credentials`, as today.
2. Email unverified:
   - no `email_code` → send a code (at most one per 60 s) and answer
     **`403 {error:"verification_required", challenge:"email_code"}`**;
   - valid code → mark the email verified and continue;
   - wrong code → `401 {challenge:"email_code", error:"invalid_code"}`.
     After 5 attempts the code is burned.
3. TOTP on:
   - no `totp_code` → **`401 {challenge:"totp"}`**;
   - wrong code → `401 {challenge:"totp", error:"invalid_code"}`. The
     ±1 step window and a replay check use the last step stored on the
     user.
4. Success:
   - a session is created (only `token_hash` stored, with device fields);
   - `device_id` is recorded in `known_devices`. If it's new and the
     user already had another device, a new-device alert goes out.

**Other endpoints:**
- `POST /verify/resend` `{email, auth_hash}`: after the credentials
  pass, resends the code. Rate-limited.
- `POST /recover` and `/account/reset-password` stay. Recover also
  requires a verified email; when it isn't verified, recover returns
  the same `email_code` challenge.
- **Sessions** (authenticated):
  - `GET /sessions` lists `{id, device_name, created_at, last_seen_at,
    current}`;
  - `DELETE /sessions/{id}`;
  - `POST /sessions/revoke-others`.
- **TOTP** (authenticated):
  - `POST /totp/setup` returns `{secret, otpauth_uri}` (not enabled
    yet);
  - `POST /totp/enable {code}`;
  - `POST /totp/disable {code}`, which also emails the owner.
- Every new endpoint goes through the existing IP and account rate
  limiters.

### Secrets and config

- **Secrets:** `RESEND_API_KEY`, `AUTH_PEPPER`, `TOTP_KEY`.
- **Vars:** `EMAIL_PROVIDERS="resend,cloudflare"`, `EMAIL_FROM="CatchM
  <no-reply@spacingmind.com>"`.
- **Binding:** `send_email` binding `EMAIL` for the Cloudflare fallback.
  The sending domain is onboarded in Email Service.
- **DNS:** add the domain to Resend and its DNS records to the
  spacingmind.com zone (SPF/DKIM). That's done by the owner, with
  help.

## App design (`catchm`)

- **Device identity:** `config.cloud.device_id` is a random id made once
  and stored. `device_name` is the hostname.
- **`internal/cloudsync`:**
  - `Login` takes the device fields plus optional codes;
  - a typed `ChallengeError{Kind: "email_code"|"totp", Reason}`;
  - `Signup` handles the 202;
  - `ResendCode`, `ListSessions`, `RevokeSession`, `RevokeOthers`,
    `TOTPSetup`, `TOTPEnable` and `TOTPDisable`.
- **`internal/server/cloud.go`:**
  - `/api/cloud/login` and `/api/cloud/signup` pass the codes through
    and answer `{challenge}` so the UI can ask for the code;
  - new endpoints for resend, sessions and TOTP.
- **Breached passwords:** on signup and password reset, the local server
  checks Have I Been Pwned's range API. Only the first 5 hex characters
  of the SHA-1 leave the machine (k-anonymity). A hit warns and needs a
  confirm; it never blocks.
- **Web (Settings → Cloud sync):**
  - an email-code step after signup and login, with Resend;
  - a TOTP step when asked;
  - a **Security** block:
    - two-factor setup with a QR code from the `otpauth://` URI,
      a confirm code, and turning it off;
    - signed-in devices with Sign out and "Sign out all other devices".

## Acceptance criteria

1. **Signup** sends a code and creates no session. Login without the
   code is refused with `verification_required`; with the code it
   succeeds. An existing (pre-change) account is asked for a code at
   its next login.
2. **Duplicate signup** answers exactly like a new one. The owner gets
   the "someone tried" email.
3. **Codes:** after 5 wrong codes the code stops working, and codes
   expire after 15 min. Resend is limited to one per 60 s.
4. **Storage:** D1 holds only `token_hash` for sessions and v2
   `auth_hash` after a login. A copied v2 `auth_hash` from the database
   doesn't log in.
5. **New device:** logging in on a second device emails the owner.
   Signing in again on the same device doesn't.
6. **TOTP:**
   - setup → enable needs a valid code;
   - login then asks for TOTP, and a wrong or replayed code is refused;
   - disable needs a code and emails the owner.
7. **Sessions:** they're listed. Revoking one ends it, and
   revoke-others keeps only the current one.
8. **Email adapter:**
   - `EMAIL_PROVIDERS` sets the order;
   - a Resend 5xx, 429 or network error falls back to Cloudflare, while
     a 4xx address rejection doesn't;
   - attempts share one Idempotency-Key;
   - tests use fake senders;
   - nothing configured gives 503, never a silent success.
9. **Breached passwords:** the check warns on a known-breached password
   during signup/reset, and nothing but a 5-character prefix is sent.
10. **Checks:**
    - Worker: `npm test`, `npm run typecheck`;
    - App: `task lint`, `task test`.
    - The release order is: Worker deployed (with secrets and DNS)
      **before** an app release that speaks the new protocol. Old
      clients keep working until their user hits a challenge, and then
      they see a clear error to update.
