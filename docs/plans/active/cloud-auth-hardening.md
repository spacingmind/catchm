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

## Progress (2026-11-27)

- **Worker:** `catchm-api` PR #7 (194 tests). The deploy waits on the
  secrets, the Resend DNS and the app release.
- **App:** part 1 is in `7972358`: challenges, device identity, resend
  and the HIBP warning. It also works against today's Worker.
- **App part 2** (uncommitted on `feat/cloud-auth-hardening`):
  - **`cloudsync`:**
    - `Recover` takes a `RecoverRequest` (device fields and an
      optional `email_code`), and `LoginWith` returns a `LoginResult`.
      Both now carry `session_id`, and older Workers ignore the new
      fields.
    - New calls: `ListSessions`, `RevokeSession`,
      `RevokeOtherSessions`, `TOTPSetup`, `TOTPEnable` and
      `TOTPDisable`.
    - Typed errors:
      - `ErrInvalidCode` for a 403 `invalid_code`;
      - one error per 409 (`ErrTOTPAlreadyEnabled`,
        `ErrTOTPNotEnabled`, `ErrTOTPNotSetUp`,
        `ErrAlreadyVerified`), each also matching `ErrConflict`;
      - `*ThrottledError{RetryAfter}` for a `resend_throttled` 429
        (also matches `ErrRateLimited`);
      - `ErrEmailUnavailable` for a 503.
  - **Server:**
    - `/api/cloud/recover` passes the device and `email_code`, and
      answers challenges like login does.
    - New routes:
      - `GET /api/cloud/sessions`;
      - `DELETE /api/cloud/sessions/{id}`, which answers
        `signed_out:true` and clears the local login when it was this
        device;
      - `POST /api/cloud/sessions/revoke-others`;
      - `POST /api/cloud/totp/{setup,enable,disable}`.
    - `GET /api/cloud/status` has `totp_enabled`.
    - A 401 on the new session routes means the session ended
      elsewhere. It clears the local login and answers
      `session_ended`.
    - Codes are normalised: spaces and dashes are dropped.
  - **Two-factor state:** no endpoint reports it, so the app tracks it
    in memory with the session:
    - a login that needed a TOTP code means on, and one that didn't
      means off;
    - enable and disable set it;
    - the server's `totp_already_enabled` / `totp_not_enabled`
      answers correct it;
    - after a recover it starts off until one of those corrects it.
  - **Web (Settings → Cloud sync):**
    - after signup, the recovery code and then the emailed-code step,
      with "Send a new code" and its countdown;
    - email-code and authenticator steps for login and recover;
    - the breached-password warning, with "Use it anyway" and "Choose
      another";
    - a **Security** section: two-factor setup (QR code, key, confirm
      code), turning it off, and the signed-in devices list with
      per-device sign-out and "Sign out all other devices".
    - The QR code is drawn with `qrcode.react` (no dependencies, one
      lockfile entry).

Changes from the design made while building:

- **Release order is reversed: app first, Worker second.** The new
  client speaks both protocols, so most people update before the Worker
  changes. Requests with no device fields (apps from before this)
  get **426** "Update CatchM to the latest version to sign in."
- **Signups that are never verified can be replaced.** One older than
  24 h is replaced by the next signup (`users.signup_pending`).
  Accounts from before this change never are.
- **Recover asks for the email-code challenge only, not TOTP.** The
  recovery code is itself the second factor.
- **Wrong TOTP codes in enable/disable get 403,** so a client doesn't
  take them for an expired session (401).
- **Login and recover return `session_id`.**
- **"Someone tried to sign up" goes out at most once an hour** per
  account, which protects Resend's 100/day free quota.
- **Cloudflare can send a message twice.** The Cloudflare binding has
  no idempotency key, so if Resend accepted a message but its reply was
  lost, the fallback could send it again.

## Deployed (2026-11-27)

- **App v0.7.0** shipped first (#46), then the Worker:
  - D1 was backed up to `~/catchm-api-backups/` (owner's machine);
  - catchm-api #7 merged, migration 0006 applied remotely, Worker
    deployed.
  - Smoke checks: `/healthz`, the update manifest, the old-client 426,
    and a real Resend send to `delivered@resend.dev`. Test accounts were
    deleted afterwards.
- **Incident:**
  - **What happened:** every send failed with Resend 403 "This API key
    is not authorized to send emails from catchm.spacingmind.com". The
    key was scoped to another domain on the shared Resend account.
  - **Fix:** a key scoped to `catchm.spacingmind.com`.
  - **Impact:** from the deploy until the key fix, no one could sign in,
    because codes couldn't be sent.
  - **Follow-up** (catchm-api #8): the Worker now logs Resend's error
    message (address-scrubbed) and sends a User-Agent.
- **Sender:** `CatchM <hello@catchm.spacingmind.com>` (catchm-api #9, owner:
  no "no-reply").
  - **Reply-To:** `EMAIL_REPLY_TO` is supported but not set yet; it
    waits for the owner's choice of a monitored address. Cloudflare
    Email Routing on the root domain is preferred over Resend Inboxes,
    since inbound mail counts against the shared Resend quota.
- **Still to watch:** the shared Resend free quota (100/day) during the
  first days, while every existing account verifies at its next login.

## Follow-up: change / forgot password while signed in (2026-11-27)

- **Worker:** `POST /account/change-password` (catchm-api PR #12) takes
  the session plus `current_auth_hash`, so a stolen session token alone
  can't change the password. It re-wraps only the password half; the
  recovery code stays valid. `sign_out_others` ends the other sessions
  in the same batch.
- **Emails:** `/account/change-password` and `/account/reset-password`
  both send `password_changed`, in the shared branded layout. The text
  says how the password changed:
  - "Change password": the recovery code still works, so "Forgot
    password?" takes the account back;
  - recovery code: the code was replaced, so the first step is to
    reply and lock the account.
- **App:** Settings → Cloud sync → Security → Password:
  - "Change password": current password, new password and its
    confirmation, and "Sign out all other devices" (on by default). A
    breached new password gets the same warning as signup;
  - "Forgot password?" while signed in: recovery code and a new
    password, through the existing recover flow. The new recovery code
    is shown once.
  - The log-in form keeps its own "Forgot password?".
