# Plan: M20 — UI shell redesign: sidebar, settings, forms, start flow

- Status: complete
- Created: 2026-09-24
- Completed: 2026-09-25
- Repo: `catchm`, **`web/` only** (plus `web/dist` build output). No Go
  changes: M19 is changing the backend in parallel. Build against the
  M19 API contract (`docs/plans/active/m19-audio-sources.md` §4) and
  the already-shipped M18 cloud-server endpoints.
- Branch/worktree: `m20-ui-shell` in `../catchm-m20` (never commit to
  `develop` directly; never push).

## Why

User test on real Windows (2026-09-24): "UI/UX chưa tốt — tab layout,
các input, form". Today: header ghost buttons for navigation, Settings
as a centered shadcn `Tabs` bar with a stack of full-width Cards and an
explicit Save, and a many-field "New meeting" dialog. That was built
milestone by milestone with no cross-screen design.

## Research (Perplexity 2026-09-24, cross-checked against
`refs/my-translator/src/index.html` + `app.js`)

- **Navigation**: persistent, collapsible **left sidebar** (Linear-
  style) instead of header buttons. 216–240 px expanded, 56–64 px
  collapsed, 36–40 px rows, 16–18 px icons. Destinations: Meetings,
  Live (stateful: shows a recording indicator + title when a meeting
  is live), Action items, Settings. A prominent **Start meeting**
  button (bottom of sidebar), `Ctrl/⌘+N`, `Ctrl/⌘+,` for settings.
- **Settings**: its own section nav (macOS System Settings / Windows
  11 style) + content pane (~680–760 px), **not** centered top tabs.
  Pages are made of sections (heading + short description) and
  **setting rows** (label + description on the left, control on the
  right). Cards only for real boundaries, not every field.
- **Save model**: autosave safe, local, reversible prefs with a quiet
  "Saved" status. Keep explicit Save only for credentials and cloud
  config. Never make the user Save unrelated fields together.
- **Forms**: shadcn `Field`/`FieldLabel`/`FieldDescription`/`FieldError`
  pattern (check what's installed under `web/src/components/ui/`; add
  via the project's shadcn convention if missing). Labels are never
  placeholders. **Inputs sized to content** (language select
  ~180–240 px, device select ~280–360 px, URL/API key ~360–480 px),
  not all full-width. Validate on blur, and no red errors on first
  render.
- **Secret fields** (Soniox API key): explicit state. "Not set" →
  "Add key"; "Set" → masked + "Replace". Never echo the stored key
  back. Say "Stored locally on this device".
- **Choice controls**: 2–4 options = segmented control / radio cards,
  not a dropdown. Audio source = `[Microphone] [System audio] [Both]`.
- **Start flow**: one click. "Start meeting" opens a compact popover/
  sheet with **remembered defaults** (title optional, auto-generated
  from date/time; audio source; language pair; engine only if more
  than one is usable) and a primary "Start meeting" button. Remember
  last-used values in `localStorage` (non-secret UI prefs only).
- my-translator's live status row (engine · state · language pair ·
  source select) and mid-session source switching are **M21** (live
  view), not this milestone.

## Scope

1. **App shell**: replace the header nav with the sidebar
   (collapsible, remembers collapsed state). Live item shows a pulsing
   indicator + meeting title when one is recording. Keyboard:
   `Ctrl/⌘+N` start, `Ctrl/⌘+,` settings.
2. **Settings redesign**: section nav + row layout. Pages:
   - **Recording**: default audio source (segmented; "System audio"
     and "Both" disabled with the reason from `GET
     /api/audio/sources` when `system.supported` is false), default
     microphone device.
   - **Transcription**: engine choice, Soniox API key (secret-field
     states), Soniox URL + Whisper URL under an "Advanced" disclosure.
   - **Translation**: Ollama host, live/batch models, reachability +
     model-status (reuse today's `SettingsTranslate` logic, restyled).
   - **Cloud sync**: today's `SettingsCloud` content restyled into
     rows. Add the **server URL** control that M18 part B left for UI
     (`PUT /api/cloud/server`, editable only while logged out, "Log
     out first" otherwise, inline validation errors; `GET
     /api/cloud/status` returns `api_base_url`). Keep every M13d/M15
     invariant: RecoveryCodeDialog explicit-ack only, recovery code
     never persisted, "Forgot password?" flow.
   Map today's `PUT /api/config` fields onto these pages without
   changing the API. Autosave where safe (per the research), with a
   Save for the API key and the cloud forms.
3. **Start meeting flow**: replace `NewMeetingDialog`'s form with the
   compact popover/sheet + remembered defaults. Send `source` per the
   M19 contract (the backend ignores it until M19 merges; `device`
   still means the mic device). **Show `capture_error`** from the
   start response prominently (e.g. a banner on the live view: "Not
   recording: Soniox API key is not set — [Open settings]"), never
   swallow it.
4. **Form polish sweep** for every remaining form (rename dialog,
   notes, search): field pattern, widths, validation timing.

## Out of scope

Live view status row / level meters / mid-session source switch /
dual-pane / compact always-on-top overlay (M21), command palette
(later), dark mode, Go changes.

## Acceptance criteria

1. `cd web && bun run build` (tsc + vite) clean; lint script if any;
   `go build ./...` still clean (embedded assets).
2. Every screen reachable through the new sidebar; Settings uses
   section nav + rows; no Settings field lost from the old UI (list the
   old → new mapping in Decisions).
3. Visual verification in a real browser/WebView: this environment
   has no browser automation, so build the desktop app on the Windows
   machine (M5/M11 toolchain) or run `catchm serve` + open the UI, and
   capture **window-only** screenshots of each page (never full-desktop:
   the user's screen may show unrelated content, which happened
   before in M11). Put them under `docs/screenshots/m20/` (committed).
   If you can't capture screenshots, say so explicitly; don't claim
   visual verification you didn't do.
4. The recording-start error path is tested by hand: an empty Soniox
   key produces the visible banner, not a silent no-op (needs M19's
   `capture_error`; if M19 isn't merged yet, verify against a stubbed
   response and note it).

## Decisions

- **Step 1 (shell)**: hand-rolled sidebar (no shadcn `sidebar` primitive — would drag in tooltip/breadcrumb deps); 224px↔64px, `localStorage` `catchm.sidebar.collapsed`; Ctrl/⌘+N / Ctrl/⌘+, ignore alt/shift. Live meeting polled from `listMeetings()` every 3s (server is source of truth, same rule as M11 tray) with optimistic set on start/end. `web/dist/index.html` never committed (review decision: hash-churning artifact).
- **Step 2 (settings)**: section nav 208px + `max-w-4xl` pane; `settings/` dir with `pieces.tsx` (SettingsSection/SettingRow/SaveStatus) — old components deleted (SettingsConfig→Transcription, SettingsTranslate→Translation, SettingsCloud→CloudSync), field mappings in commit messages. `useConfigSave` sends PATCH-ONLY bodies (server PUT /api/config is setIfNonEmpty partial). Soniox key secret states, URLs blur-save + absolute-URL validation. Recording prefs in `lib/recordPrefs.ts`; `effectiveSource()` falls back to mic when system audio unavailable, stored choice kept + notice shown.
- **Step 3 (start flow)**: `StartMeetingPopover` anchored to the sidebar Start button; title optional (client-generated `Meeting <date> <time>`, backend needs non-empty); source segmented via effectiveSource; `StartMeetingResponse.capture_error` → role=alert banner on the live view + Open settings. Stub-verified via throwaway SSR render (M19 not merged).
- **Step 4a (audit fixes)**: `--muted-foreground` `#A9826D`→`#6A4C3A` — computed WCAG ratios **7.24:1 on background/card, 4.62:1 on muted** (#D9C4B4); audit's suggested #7D5A48 was computed at only 3.65:1 on muted so rejected. MeetingList cards → real `<button>` (Enter/Space, ring). Languages → shared `lib/languages.ts` Selects (value=code) in popover + translation test. Cloud forms: autocomplete (email/current-password/new-password/one-time-code), recovery primary is type=submit, per-field errors via aria-describedby + focus-first-invalid; submit buttons enabled until request (validate on submit). `SegmentedControl` rebuilt on shadcn ToggleGroup single (disabled-with-reason kept). ActionItems raw checkboxes → shadcn Checkbox + clickable labels (id/htmlFor). `motion-safe:animate-pulse`, `type="url"` on URL inputs, `break-words` utterances, `cursor-pointer` rows, no-op ternary fixed. Rejected by design: ui-ux-pro-max teal/orange palette + Plus Jakarta (would erase the chocolate/cream brand), title-only start popover.

- **Step 4b (P1 IA)**: (1) MeetingDetail — Overview tab reorders to Decisions + Action items (inline Checkbox check-off via updateActionItemStatus, optimistic, revert on error) then Summary/Participants/Notes as `Section` headings (stacked Cards dropped); Transcript tab lazy-loads on first open by walking getTranscript's cursor to exhaustion (500-page cap), speaker-grouped consecutive utterances with translation lines. (2) MeetingList cards add a truncated preview line (first decision, else summary). (3) ActionItems — grouped-by-meeting default + By-due-date flat toggle (parseable dates ascending, no-date last), owner/meeting filter chips (composable), Overdue badge + red due text (past parseable dates, open items only), 'Done' normal-case. (4) Settings moved to sidebar bottom utility area beside Start meeting. (5) Start popover: title+source+mic+Start on top; language pair/two-way/engine under an Options disclosure with one-line value summary ("en → vi · two-way · Soniox").

## Progress

- [x] Sidebar shell + keyboard shortcuts
- [x] Settings: section nav + row layout + save model
- [x] Settings pages (Recording, Transcription, Translation, Cloud incl. server URL)
- [x] Start-meeting popover + remembered defaults + capture_error surfacing
- [x] Form polish sweep (4a audit: contrast, a11y, ToggleGroup/Checkbox, language Selects, validation UX)
- [x] Visual verification (headless Chromium screenshots + capture_error flow)

## Validation

**How verified (honest framing):** headless Chromium via Playwright driving
`catchm serve` directly, in a `m20-verification` worktree —
**not** the Wails WebView2 desktop window (M5/M11 toolchain). This
environment has no Windows/Wails build available, so the WebView2-specific
chrome (title bar, native menu, DPI scaling) was not exercised. Everything
below is the web UI as `catchm serve` renders it.

Setup: `cd web && bun install --frozen-lockfile && bun run build` (clean);
`CATCHM_HOME=/tmp/catchm-m20v-home CATCHM_ADDR=127.0.0.1:44983 go run
./cmd/catchm serve`. Playwright + headless Chromium installed standalone
under `/tmp/catchm-m20v-pw` (not in the repo; `--with-deps` needed sudo and
was unavailable non-interactively, so only the Chromium binary was
installed — it launched and rendered fine without the extra OS libs).
Seed data: 4 meetings created via `POST /api/meetings` (3 ended, 1 left
live), tags/notes/action-item-status set through their real API endpoints
(`PATCH /api/meetings/{id}`, `POST .../notes`, `PATCH
.../action-items/{index}`), decisions/action items/participants/summary
hand-written into `meeting.md` frontmatter and `utterances.jsonl` per the
`internal/meeting` schema (no app code touched — verified with `git status`
on `web/`, `internal/`, `cmd/` before committing).

**AC3 (visual verification).** 16 screenshots in `docs/screenshots/m20/`,
1280×800 plus a 900px-wide pass for the sidebar/list:
`sidebar-expanded(-narrow)`, `sidebar-collapsed(-narrow)`,
`meetings-list(-narrow)`, `meeting-detail-overview`,
`meeting-detail-transcript`, `action-items-grouped`,
`action-items-by-due-date`, `settings-recording`, `settings-transcription`,
`settings-translation`, `settings-cloud-logged-out-advanced`,
`start-meeting-popover-collapsed`, `start-meeting-popover-expanded`. All
pages render without overflow/clipping at both widths; no missing Settings
fields versus the old → new mapping in Decisions.

**AC4 (capture_error, real end-to-end).** `PUT /api/config
{"engine":"soniox"}` with no key configured
(`soniox_api_key_set: false`); Playwright opened the sidebar Start-meeting
popover and submitted it (mic/default device, no manual field changes —
`configEngine` resolves to `soniox` automatically since it's the only
usable engine when no key is set). Live view showed
`role="alert"` with the exact text "Not recording: Soniox API key is not
set — add it in Settings → Transcription." plus an "Open settings" action;
clicking it navigated to Settings. Screenshots: `live-capture-error-banner`,
`live-capture-error-open-settings-landed`. **No microphone was touched**:
confirmed by reading `internal/server/server.go`'s `startPipeline` —
`s.newEngine(...)` runs first and returns `errSonioxNoKey` before
`s.newSource(device)` (mic open) is ever reached, so the failure short-circuits
ahead of any audio-source open. `GET /api/audio/sources` was still called
once by the popover on open (device enumeration for the Microphone
dropdown, not a recording open) — expected, not a violation of this claim.

**UX findings from clicking through** (ranked; none blocking, none fixed —
verification only):

1. **Medium — "Open settings" from the capture_error banner lands on
   Settings → Recording, not Transcription.** The banner text says "add it
   in Settings → Transcription," but `onOpenSettings` only switches the
   view to Settings, which defaults to its first section (Recording). The
   Soniox key field the user needs is one more click away.
   (`live-capture-error-open-settings-landed.png`; `App.tsx`'s
   `openSettings` / `web/src/components/settings/Settings.tsx`'s
   `useState<Section>("recording")` default.)
2. **Low — collapsed sidebar nav buttons lose their accessible name.**
   `AppSidebar.tsx`'s `NavButton` only renders visible text when
   `!collapsed`; the collapsed icon-only button falls back to a `title`
   attribute, which isn't a reliable accessible-name equivalent to
   `aria-label` for screen readers. The sidebar's own Start-meeting and
   collapse-toggle buttons already carry `aria-label` — the plain nav
   items (Meetings/Live/Action items/Settings) don't.
   (`sidebar-collapsed.png`; `AppSidebar.tsx:32-54`.)
3. **Low — meeting-count copy doesn't pluralize.** `MeetingList.tsx` prints
   `"{n} decisions"` / `"{n} action items"` unconditionally, so a
   single-item meeting reads "1 decisions" / "1 action items"
   (`meetings-list.png`, Weekly Standup card; `MeetingList.tsx:165-166`).
4. **Low — Translation status takes ~5s to settle, with only a bare
   skeleton in between.** `GET /api/translate/status` genuinely takes
   about 5 seconds server-side when nothing listens on the configured
   Ollama host (confirmed by timing the request in Playwright, and it's
   not a client-side hang — the endpoint itself is just slow to fail this
   dial). The Skeleton placeholder gives no indication anything is still
   in progress versus stuck. First screenshot attempt caught the
   mid-flight skeleton state; a longer wait resolved it to the correct
   "Unreachable" / "Not installed" badges (kept in
   `settings-translation.png`).
5. **Cosmetic — Start-meeting popover overlaps the Live view for ~1s
   after submit.** The popover's close transition takes about a second to
   finish after `onOpenChange(false)`, so a screenshot taken immediately
   after starting a meeting catches it still open over the freshly-shown
   capture_error banner. Purely a transition-timing artifact (confirmed:
   gone by 1s, not stuck) — the first AC4 capture caught this moment; the
   kept `live-capture-error-banner.png` waits it out.
6. **Nit — `AppSidebar.tsx:82`'s `transition-[width]` animates a layout
   property** (width) rather than transform/opacity per the project's own
   web-design-guidelines skill; likely fine at this size/frequency but
   flagged since the guideline is explicit about it.

`web-design-guidelines` skill run against `AppSidebar.tsx`,
`StartMeetingPopover.tsx`, `LiveMeeting.tsx`, `MeetingList.tsx`,
`ActionItems.tsx`, `Settings.tsx` (+ subpages): the two AppSidebar items
above were the only findings; everything else passed (labeled controls,
`aria-pressed`/`aria-label` where needed, proper ellipsis character,
`truncate`/`min-w-0` on card text).
