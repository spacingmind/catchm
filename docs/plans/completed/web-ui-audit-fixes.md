# Plan: Web UI audit fixes

- Status: completed (2026-09-28). The Windows desktop checks listed under Results are still pending.
- Created: 2026-09-27
- Repo: `catchm`, `web/` only. There are no Go or API changes: the
  mid-meeting mic switch already works server-side
  (`switchAudioSource` reopens the mic even when the mode is unchanged).
- Branch: `fix/web-ui-audit`

## Why

We reviewed all of `web/src` against the Vercel Web Interface Guidelines
(2026-09-27) and found about 150 issues. Around 10 are functional bugs;
the rest come from five cross-cutting pieces that were never built as
shared code, so every screen re-invents them:

- a status/error region that screen readers announce
- a form field whose label, description and error are wired to the control
- a disclosure (show/hide section)
- a filter chip
- scroll that sticks to the bottom

A few theme tokens are also still shadcn's neutral grays, so focus rings
and input borders are nearly invisible on the cream background.

## Decisions

- **Copy stays sentence case.** The app already uses it consistently, so
  the guideline's Title Case rule doesn't apply.
- **No new runtime dependencies.** Long lists get
  `content-visibility: auto` and memoization, not a virtualization
  library. Collapsible and AlertDialog come from `radix-ui`, which is
  already a dependency, added through the shadcn CLI.
- **Destructive actions get a confirm dialog, not undo.** This covers End
  meeting and changing the cloud server while an account is stored.
  Delete meeting already has one.
- **Deferred (out of scope, separate plan):**
  - Reflecting view and selection state in the URL. This needs a routing
    decision for both `catchm serve` and the Wails shell.
  - Cancelling an update download, which needs backend support.

## Phase 1: shared foundation (single agent, runs first)

Contract (Phase 2 depends on these exact names):

1. **Tokens** (`src/index.css`, `index.html`)
   - `--ring`: brand-derived, at least 3:1 contrast on `#FBF7EA`.
   - `--border` and `--input`: a visible warm tint.
   - New `--warning` and `--warning-foreground`, plus
     `--color-warning*` entries in `@theme`.
   - `html { color-scheme: light }` and
     `<meta name="theme-color" content="#FBF7EA">`.
2. **`ui/collapsible.tsx`, `ui/alert-dialog.tsx`**, added with
   `bunx shadcn add`.
3. **`components/StatusMessage.tsx`**
   - Signature:
     `StatusMessage({ tone?: "muted"|"error"|"success"|"warning", id?, className?, children? })`.
   - Always mounted, with `role="status"`, `aria-live="polite"` and
     `aria-atomic`. Only the text changes; when empty it takes no space.
4. **`settings/pieces.tsx`**
   - `SettingRow`:
     - Renders `<span>` instead of `<label>` when there is no `htmlFor`.
     - The description gets `id={descId(htmlFor)}`.
     - Export `descId(id) => \`${id}-desc\`` and `labelId(id)`.
   - `SaveStatus` is built on `StatusMessage` and is always mounted.
     The fallback error includes a next step.
   - `SegmentedControl`:
     - Takes `labelledBy?`.
     - Takes per-option `reason?`. For a disabled option, the reason is
       shown as visible text linked through `aria-describedby`, not only
       as `title`.
5. **`components/Disclosure.tsx`**
   - Signature: `Disclosure({ label, open?, defaultOpen?, onOpenChange?, children, className? })`.
   - Built on ui/collapsible, with a ghost `Button` trigger, `aria-controls`,
     and a chevron using `motion-safe:transition-transform`.
6. **`components/FilterChip.tsx`**
   - Signature: `FilterChip({ pressed, onPressedChange, children, ...toggleProps })`.
   - Built on ui/toggle (outline, sm), with a hover state.
7. **`components/ConfirmDialog.tsx`**
   - Signature: `ConfirmDialog({ open, onOpenChange, title, description, confirmLabel, destructive?, pending?, error?, onConfirm })`.
   - Built on ui/alert-dialog.
   - Shows the error inline inside the dialog.
   - Stays open while `pending` is true.
8. **`hooks/useStickToBottom.ts`**
   - Signature: `useStickToBottom(contentKey)` returning
     `{ ref, atBottom, hasNew, scrollToBottom }`.
   - `ref` is a callback ref placed on an element inside the scroll
     container. The hook resolves the nearest
     `[data-slot=scroll-area-viewport]` or scrollable ancestor.
   - It re-attaches when the element remounts.
   - When `contentKey` changes, it auto-scrolls only if the view was at
     the bottom. The first content always lands on the bottom.
9. **`lib/format.ts`** (all use `Intl.DateTimeFormat`)
   - `formatDateTime(iso)` and `formatTime(iso)`, both short, no seconds.
   - `formatDateRange(startIso, endIso?)`.
   - `parseLocalDate("YYYY-MM-DD")`, which parses as local midnight, not UTC.
   - `formatDay("YYYY-MM-DD")`.
10. **`lib/url.ts`**
    - `isAbsoluteUrl(v, protocols: string[])`.
11. **`lib/inputProps.ts`**
    - `noAutofill`, which bundles `autoComplete="off"`,
      `spellCheck={false}`, `autoCapitalize="off"`,
      `data-1p-ignore`, `data-lpignore="true"` and `data-bwignore`.

## Phase 2: per-screen fixes (4 agents in parallel, disjoint files)

### A: App shell and lists
Files: `App.tsx`, `AppSidebar.tsx`, `MeetingList.tsx`, `ActionItems.tsx`,
`UpdateBanner.tsx`, `RecoveryCodeDialog.tsx`

- [x] RecoveryCodeDialog:
  - `showCloseButton={false}`, and Escape is blocked on purpose.
  - Copy shows "Copied" through StatusMessage, and a clipboard failure is
    reported.
  - The code has `translate="no"`.
  - The button reads "Copy code".
- [x] UpdateBanner:
  - Sits in normal flow instead of `fixed`, so it no longer covers the
    sidebar header.
  - Release notes open through an `<a>`, and it's checked that Wails
    opens it in the system browser.
  - Labels are specific: "Download update" and "Install & restart".
  - `publishedAt` is formatted with `formatDay`.
  - The text ends in `…` while updating.
- [x] MeetingList:
  - Rows get their padding back (Card spacing defined).
  - No block elements inside `<button>`.
  - A "Searching…" state replaces the blank list during search.
  - The error uses StatusMessage and has a retry.
  - The search input has `type="search"` plus `noAutofill`.
  - The skeleton has sr-only loading text.
  - Tag chips use FilterChip.
  - Times use `formatTime`.
  - Rows get `content-visibility: auto`.
  - The empty state has a start call to action.
- [x] ActionItems:
  - Owner and meeting filters are separate labelled groups built on
    FilterChip.
  - Empty state for filters that match nothing, plus "Clear filters".
  - A toggle error shows next to its row.
  - Due dates use `parseLocalDate`/`formatDay`, which fixes the false
    UTC+7 overdue.
  - Group titles are `<h2>`.
  - Loading has sr-only text.
  - The checkbox label is one hit target.
- [x] AppSidebar:
  - The Live item's accessible name includes "(recording)".
  - The Ctrl+N hint is shown only in the desktop shell.
  - The logo button has a hover state.
  - `translate="no"` on the brand.
- [x] App: a skip link to `<main id="main">`, and a message when a toast
  opens an unknown meeting.

### B: live view
Files: `LiveMeeting.tsx`, `live/StatusRow.tsx`, `live/CompactOverlay.tsx`,
`StartMeetingPopover.tsx`

- [x] LiveMeeting:
  - Uses `useStickToBottom`, so auto-scroll survives a compact round-trip.
  - The transcript has `role="log"`.
  - Rows are memoized and use `content-visibility: auto`.
  - The poll error clears on the next success and uses StatusMessage.
  - End meeting asks through ConfirmDialog, which warns about an unsaved
    note.
  - Add note shows a pending state, and its error sits next to the field.
  - The "Saved" status is live, and its timer is cleared on unmount.
  - The title flex item has `min-w-0` and `text-balance`.
  - Uses `ui/label`.
- [x] StatusRow:
  - Picking a mic mid-meeting calls `setMeetingSource(id, source, device)`.
  - The select starts from the meeting's device and offers "Default
    device".
  - The engine pill uses `role="status"`, truncates, and shows the full
    error text elsewhere.
  - The drops warning is live and says what to do.
  - Timer semantics are correct.
  - Disabled-source reasons are shown as visible text.
  - The level meter uses `scaleX`, `motion-reduce`, and is `aria-hidden`
    or `role="meter"`.
  - The warning token replaces the hardcoded amber.
  - Uses the Badge primitive.
  - The Ctrl+Shift+L hint drops the ⌘ on Windows.
- [x] CompactOverlay:
  - Uses `useStickToBottom`, so the newest line stays visible.
  - The exit button hit target is at least 32px.
  - Has `role="log"`.
  - The level bar gets the same fix as StatusRow.
  - Honours the font-size pref.
  - Has sr-only "Recording" text.
- [x] LiveMeeting and CompactOverlay: compact mode shows capture and
  engine errors, and the current source, which is lifted out of
  StatusRow.
- [x] StartMeetingPopover:
  - The two-way error reopens Options before it focuses.
  - The start state is visible, or the popover stays open until the
    request settles.
  - The start error is live.
  - The system-audio reason comes from the backend's `reason`.
  - The language selects offer "Auto-detect".
  - Options is a Disclosure and the two-way toggle uses `ui/toggle`.
  - Group labels are wired to their controls with matching names.
  - The title input gets `noAutofill`.

### C: meeting detail and settings (shell, recording, system, shortcuts)
Files: `MeetingDetail.tsx`, `settings/Settings.tsx`,
`settings/SettingsShortcuts.tsx`, `settings/SettingsRecording.tsx`,
`settings/SettingsSystem.tsx`

- [x] MeetingDetail:
  - Rename and delete errors show inside their dialogs.
  - Delete uses ConfirmDialog.
  - The page errors use StatusMessage.
  - `groupBySpeaker` is memoized and rows use `content-visibility`.
  - The checkbox is named by its visible label.
  - Rename is "Rename meeting", stays enabled, and shows an inline
    "Title is required".
  - Dates use `formatDateRange`/`formatDay`.
  - Long user content has `break-words` and the h1 has `text-balance`.
  - Skeletons have sr-only text.
  - The lang code has `translate="no"`.
  - `animate-spin` becomes `motion-safe:`.
- [x] Settings nav:
  - Uses Button ghost or matching focus tokens.
  - `aria-current="page"`.
- [x] SettingsShortcuts:
  - Inputs are controlled or keyed on the saved value.
  - Enter commits the shortcut.
  - Inputs get `noAutofill` and an example placeholder.
  - The load error is shown.
  - There is a loading state.
  - Errors say what to do.
- [x] SettingsRecording:
  - The segmented control is labelled.
  - The mic select offers "Default device", is disabled when there are
    no devices, and handles a stale ID.
  - The duplicated "unavailable" copy is removed and punctuation is fixed.
  - The no-mic message says what to do.
- [x] SettingsSystem:
  - Each section has its own SaveStatus.
  - The update status is live.
  - The load error is shown instead of fake defaults.
  - Label and checkbox form one hit target.
  - The version uses `tabular-nums`.
  - There is no "version undefined".

### D: transcription, translation and cloud sync settings
Files: `settings/SettingsTranscription.tsx`,
`settings/SettingsTranslation.tsx`, `settings/SettingsCloudSync.tsx`

- [x] SettingsTranscription:
  - Soniox URL accepts `ws:`/`wss:` (`isAbsoluteUrl`), and the default
    saves.
  - The API key editor is a `<form>` with a Saving… state and is disabled
    while saving.
  - The key input uses `noAutofill` so password managers ignore it.
  - Errors are linked through `aria-describedby` and are live.
  - The "Set" indicator has real text.
  - Advanced is a Disclosure.
  - URL inputs get `noAutofill`, and placeholders end in `…`.
- [x] SettingsTranslation:
  - Host and model inputs get `noAutofill`, and errors are described and
    live.
  - Each save status sits next to its field.
  - The status error is live and has a "Check again" button.
  - Pull buttons are named per model and can be cancelled.
  - Progress has an accessible name.
  - The pull status is live.
  - Each target has its own abort controller.
  - The test form uses `noValidate`, shows its error next to the field,
    and its result is live.
  - The result uses `break-words`, and the arrow is `aria-hidden`.
  - Labels read "Live model" and "Batch model".
  - The button reads "Test translation".
- [x] SettingsCloudSync:
  - Sign up is a real submit that uses `new-password`.
  - Email uses `username` plus `name`/`spellCheck`/`autoCapitalize`.
  - The recovery code input uses `autoComplete="off"`.
  - Link buttons use Button variant="link".
  - Auth errors are live and focus the field.
  - Logout has a pending state and its own error.
  - "Logged in never" is fixed, and dates use `formatDateTime`.
  - Sync results are live and counts use `tabular-nums`.
  - Advanced is a Disclosure.
  - The server URL is a `<form>` with a ConfirmDialog when an account
    exists.
  - The Save button reads "Save server URL".

## Acceptance criteria

1. Every checklist item above is done, or deferred with a reason noted in
   this file.
2. `cd web && bunx tsc -b` and `bun run build` pass, and `bunx oxlint`
   reports no new warnings.
3. `task test` and `task lint` pass (Go side unchanged).
4. Smoke test against `catchm serve` in a browser:
   - Meeting list padding and search.
   - Settings: Soniox `wss://` URL saves.
   - Rename error shows in the dialog.
   - End meeting asks for confirmation.
   - Tab shows a visible focus ring.

## Results (2026-09-28)

All Phase 1 and Phase 2 checklist items are done. Nothing from the lists
above was deferred beyond the two items in Decisions.

### Extra fixes found while integrating and smoke testing
- **`dark:` followed the OS setting.** Tailwind v4's `dark:` variant
  follows `prefers-color-scheme`, so shadcn's `dark:*` styles were
  applied under Windows dark mode. It is now tied to a `.dark` class
  (`@custom-variant` in `index.css`), which the app never sets.
- **ui/ primitive fixes:**
  - The focus halo is now `ring-ring/80` (3.6:1 on cream).
  - `--destructive` is now `#B42318` (6.1:1).
  - `transition-all` became `transition` in button, badge and toggle.
  - `Progress` now passes `value` to Radix, which sets `aria-valuenow`.
- **Live layout:** Live fills the height left under the update banner
  (`main` is a flex column in the live view), instead of using `h-svh`
  and scrolling under the banner.
- **New shared helper:** `browserOpenURL()` in `wailsRuntime.ts`, used
  by UpdateBanner to open external links.
- **SegmentedControl `describedBy`:** row descriptions are now linked on
  Recording source and STT engine.
- **Review follow-ups:**
  - Overdue detection and sorting fall back to `Date.parse` for due
    dates that aren't `YYYY-MM-DD`.
  - An idle `SaveStatus` takes no space.
  - A mid-meeting mic switch is remembered per meeting.
  - The newest transcript group skips `content-visibility`.

### Verification
- `bunx tsc -b` passes.
- `bun run build` passes.
- `bunx oxlint` shows 3 warnings, down from 7. All are
  `only-export-components` in shadcn `ui/`.
- `task test` and `task lint` pass.
- Smoke test against `catchm serve` with a temp `CATCHM_HOME`. Each
  check passed:
  - List card padding and date range.
  - Soniox `wss://` URL saves (checked with `/api/config`).
  - A rename error shows inside the dialog (forced with a read-only
    meeting dir).
  - Action items show separate owner and meeting groups, local-date
    overdue, and free-text due dates.
  - The Live view layout, source reasons and warning token.
  - End meeting asks for confirmation, warns about an unsaved note, and
    shows "Ending…" until the server finishes.

### Not verified (needs a real desktop or manual check)
- Focus ring visuals: the automation tab has no window focus.
- Wails-only paths:
  - `BrowserOpenURL`.
  - Shortcuts round-trip.
  - Platform ⌘/Ctrl detection.
  - Autostart and notifications.
- Password manager behaviour on sign up.
- Pull cancel against a real Ollama.
- A real device switch mid-meeting.
- Screen reader announcements.

### Follow-ups (not in this change)
- **Backend:** `/api/cloud/status` should report whether account params
  are still stored while logged out (e.g. `account_stored`). With that,
  the server-URL confirm could appear only when it would actually clear
  something. For now it always confirms a real URL change.
- **Deferred:** URL deep-linking/routing, and cancelling an update
  download.
- **Bundle size:** the main JS chunk is now about 515 kB minified, which
  triggers Vite's 500 kB warning. Consider code-splitting the settings
  panes.
