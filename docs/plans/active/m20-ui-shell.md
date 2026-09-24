# Plan: M20 — UI shell redesign: sidebar, settings, forms, start flow

- Status: active
- Created: 2026-09-24
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

(fill in, including the old → new settings field mapping)

## Progress

- [ ] Sidebar shell + keyboard shortcuts
- [ ] Settings: section nav + row layout + save model
- [ ] Settings pages (Recording, Transcription, Translation, Cloud incl. server URL)
- [ ] Start-meeting popover + remembered defaults + capture_error surfacing
- [ ] Form polish sweep
- [ ] Visual verification (window-only screenshots)

## Validation

(to be filled)
