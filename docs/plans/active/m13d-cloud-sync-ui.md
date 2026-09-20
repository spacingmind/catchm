# Plan: M13d — Cloud sync web UI

- Status: active
- Created: 2026-09-21
- Repo: `catchm` (web frontend only; no backend changes expected —
  `/api/cloud/*` from M13c already covers everything this UI needs).

## Goal

Make M13c's cloud sync actually usable from the app: a settings screen
to sign up or log in, a way to see sync status, a button to trigger a
sync, and — critically — a clear, hard-to-miss display of the
one-time recovery code at signup, since losing it means permanent data
loss with no other recovery path (per `docs/cloud-sync-architecture.md`).
Read `docs/plans/completed/m13c-cloud-sync-client.md` in full first —
its Decisions section documents the exact `/api/cloud/*` request/
response shapes from the real implementation.

## Scope

### 1. `web/src/lib/api.ts` additions

Following the existing pattern (`getConfig`/`updateConfig`,
`apiFetch<T>` helper, typed interfaces) add: `cloudSignup(email,
password): Promise<{email: string, recovery_code: string}>`,
`cloudLogin(email, password): Promise<{email: string}>`,
`cloudLogout(): Promise<void>`, `getCloudStatus(): Promise<{logged_in:
boolean, email?: string, logged_in_at?: string, last_sync_at?:
string}>`, `cloudSync(): Promise<{pushed: string[] | null, pulled:
string[] | null, conflicted: string[] | null}>`. Match the exact JSON
field names `/api/cloud/*` actually returns (check `internal/server/cloud.go`
directly, don't guess from the spec prose alone — field names must be
byte-exact).

### 2. `web/src/components/CloudSync.tsx` (new component)

A card in Settings (alongside `SettingsConfig`), state machine:
- **Logged out**: tabs or a toggle between "Sign up" and "Log in",
  each a small email+password form (password field type="password",
  no confirm-password field needed — this isn't a web account with a
  password-reset-by-email flow, losing the password without the
  recovery code loses the data, say so in copy near the form).
- **Just signed up**: show the `recovery_code` in a monospace,
  copy-to-clipboard box with a clear warning that it is shown once,
  will not be shown again, and must be saved somewhere safe (a
  password manager, printed, etc.) — this is the single most important
  UX moment in this milestone, don't undersell it visually. Require an
  explicit "I've saved this" acknowledgment (a checkbox or button)
  before the box goes away and the UI moves to the logged-in state —
  don't auto-dismiss it on a timer or on navigating away.
- **Logged in**: show the email, last sync time (human-relative, e.g.
  "2 minutes ago" — check if the repo already has a relative-time
  helper before adding a new dependency), a "Sync now" button
  (disabled while a sync is in flight, shows a spinner/loading state),
  and the result of the last sync (counts of pushed/pulled/conflicted,
  or the actual meeting titles if that's easy — your call), a
  "Log out" button. If `conflicted` is non-empty after a sync, surface
  it plainly (which meetings, and that a `conflict-*` file was left in
  that meeting's folder for manual review) — don't bury this in a toast
  that disappears.
- Loading and error states throughout (network errors, wrong
  password, duplicate email on signup, etc.) — reuse whatever
  error-display convention `SettingsConfig.tsx` already uses.

### 3. Wiring

Add `CloudSync` to wherever `SettingsConfig` is rendered (find that
page/route) so both appear together as "Settings". Match existing
spacing/layout conventions in that page — look at how multiple config
cards already stack, if any do.

### 4. Manual test plan (this is UI — no meaningful unit-test coverage
   is expected, per house convention for other UI milestones; instead
   verify by hand)

Run the app (`go run ./cmd/catchm` or however it's normally started
for manual testing) plus `catchm-cloud`'s `wrangler dev`, and actually
click through: sign up, verify the recovery code display and its
one-time behavior (refresh the page — is the code gone? correct,
per design), sync a real meeting, verify status updates, log out, log
back in with just email+password, verify the previously-synced meeting
appears (may need the M13c sync-pull path, or may just already exist
locally — note which). Force a conflict (edit a meeting through two
different means, or via curl, matching M13c's own test approach if
useful) and confirm the conflict is visible and non-silent in the UI.
Record what you actually clicked and saw in Validation — screenshots
if the tooling supports capturing them, otherwise a precise textual
walkthrough.

## Out of scope

Device linking UI (M14), recovery-code-based account recovery UI
(M15), any visual redesign of existing Settings beyond adding this
card, desktop-tray integration with cloud sync status.

## Acceptance criteria

1. `npm run build` (or the repo's equivalent, check `web/package.json`)
   and any lint/typecheck script clean.
2. `go build ./... && go vet ./... && gofmt -l .` clean (embedding the
   rebuilt web assets shouldn't break the Go build).
3. Manual walkthrough per section 4 completed and recorded in
   Validation, including the forced-conflict scenario.
4. The recovery code is never re-displayable after the initial
   acknowledgment — confirm by reading the code that it isn't cached
   in component state beyond that screen, local storage, or anywhere
   else client-side.

## Decisions

(fill in: exact api.ts function signatures added, where CloudSync.tsx
was mounted, relative-time approach, how the conflict scenario was
manually forced and verified.)

## Progress

- [ ] api.ts cloud endpoints
- [ ] CloudSync.tsx component (signup/login/recovery-code/status/sync)
- [ ] Wired into Settings
- [ ] Manual end-to-end walkthrough incl. forced conflict, recorded

## Validation

(to be filled as criteria are confirmed)
