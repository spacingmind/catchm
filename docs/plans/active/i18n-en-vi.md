# UI i18n: English + Vietnamese

## Problem

Every user-facing string in the web UI is hard-coded English (~800–1,000
strings across ~36 components under `web/src/components`), and ~88
`httpError(w, …)` sites in `internal/server` return English sentences
that the UI shows verbatim (`web/src/lib/api.ts:333`). A Vietnamese user
gets an English-only app. Dates already go through `Intl` but with an
`undefined` locale (`web/src/lib/format.ts:6`), so they follow the
browser, not the user's choice.

## Decisions (user, 2026-09-29)

- **Scope:** the web UI plus user-facing backend errors. Native strings
  (tray menu `cmd/catchm-desktop/tray.go`, toasts, NSIS installer) are a
  follow-up; the storage choice below keeps that open.
- **Storage:** `config.yaml`, not localStorage, so Go can read it when
  native strings follow.
- **Library:** i18next + react-i18next. Lingui was the alternative;
  rejected because its macros need Babel or SWC, and
  `@vitejs/plugin-react` 6.1.1 has neither (oxc only). Adopting it would
  put `@babel/core` + `@rolldown/plugin-babel` back in the build for
  every file. i18next is runtime-only: no build change.

## Design

### Preference

- New config field `ui_language`: `"system"` (default when absent),
  `"en"`, `"vi"`. Distinct from the existing `lang` (default STT
  language). Explicit `"system"` because PUT `/api/config` treats an
  empty field as "leave as-is" (`setIfNonEmpty`), so empty can't mean
  "reset".
- Round-trips through `sanitizedConfig` / `configRequestBody`; PUT
  rejects any other value with 400.
- `"system"` resolves in the UI from `navigator.languages`: first entry
  starting with `vi` → `vi`, otherwise `en`.
- Switcher in Settings → Appearance, next to Theme. Applies instantly
  (`i18next.changeLanguage`) and saves through the existing
  `useConfigSave`.
- Before config loads, the UI renders in the resolved system language
  (no English flash for a vi-system user on first launch); a stored
  choice then switches it if different.

### Web

- `web/src/i18n/` with `index.ts` (init), `en.ts` (source of truth),
  `vi.ts`. Resources bundled, not lazy-loaded (two small languages).
- Keys namespaced by screen (`settings.translation.*`,
  `meeting.detail.*`, `errors.*`, `common.*`). `en.ts` is `as const`;
  react-i18next `CustomTypeOptions` types `t()` keys from it, so a typo'd
  key fails `tsc`. `vi.ts` is typed to the same deep shape, so a missing
  or extra key also fails `tsc` — enforced by CI's existing web build,
  no new test runner.
- Plurals: i18next `_one` / `_other` for English; Vietnamese uses
  `_other` only.
- Translate visible text **and** `aria-label`, `title`, `placeholder`,
  `alt`, and document title.
- `<html lang>` follows the active language.
- `format.ts`, `MeetingList.tsx`'s month formatter and
  `StartMeetingPopover.tsx`'s default title take the active locale
  instead of `undefined`; formatters are rebuilt on language change (they
  are module-level for performance today).
- Language names in `web/src/lib/languages.ts` (22 ISO 639-1 codes
  with hand-written English labels) come from `Intl.DisplayNames` in the
  active locale; the code list stays as the curated set.

### Backend errors

- New `httpErrorCode(w, status, code, msg)` writes
  `{"error": msg, "code": code}`. `msg` stays English — it's what logs,
  MCP tools and agents read.
- Audit all ~88 sites. Codes only for errors a user can actually hit
  through the UI (missing API key, engine unreachable, bad file format,
  meeting not found, …). Programmer or validation errors ("bad json")
  keep `httpError` with no code.
- Existing summary `kind` values (`summary_not_ready`,
  `consent_required`, `quota_exceeded`, `summary_failed`) are reused
  as-is, not duplicated.
- UI: `ApiError` carries `code`, and the display is
  `t("errors.<code>", { defaultValue })`. An error with no code, or a
  code the UI doesn't know, shows a localized generic line with the raw
  English message as detail. Nothing gets swallowed.
- Existing UI-side rewrites (`MeetingDetail.tsx:103-116`,
  `useFindFlows.ts`, `StatusRow.tsx`) move into the dictionary.

## Out of scope

- Native strings: tray, toasts, NSIS installer, Wails window title.
- Summary/analysis output language: follows the meeting's language
  (`AnalysisLang`), independent of UI language.
- MCP tool descriptions and agent-facing text.
- Languages beyond en/vi (the structure allows adding one as a new file).

## Rollout (one PR, commits per step)

1. Config field + API + Go tests.
2. i18n infra, switcher, `<html lang>`, locale-aware formatters; the
   Settings → Appearance screen converted as the pilot.
3. Convert screens: shell/sidebar → meeting list → start popover +
   live view + compact overlay → meeting detail + insights → import
   dialog → remaining settings → projects, onboarding, update banner.
4. Backend error codes + UI mapping.
5. Vietnamese pass: the user reviews `vi.ts` wording (terminology
   choices like "cuộc họp", "bản ghi", "tóm tắt" fixed once in a glossary
   at the top of `vi.ts`).

## Acceptance criteria

- `ui_language` round-trips through GET/PUT `/api/config`; an invalid
  value → 400; absent → `"system"`; a partial PUT without it leaves it
  unchanged (Go tests).
- Switching language in Settings changes the whole UI immediately
  without reload, and survives restart.
- `vi.ts` missing or adding a key relative to `en.ts`, or a `t()` call
  with an unknown key, fails `bun run build`.
- No hard-coded user-facing English left in `web/src/components`
  outside `components/ui/*`: checked by a grep script listed in
  Validation, with any remaining hits justified (brand names, units).
- Dates/times/month headers render in the active locale.
- Every coded backend error shows in Vietnamese in vi; an uncoded one
  shows the localized generic line plus English detail.
- The layout holds with Vietnamese strings (typically ~20–30% longer)
  in the sidebar, settings rows, compact overlay and dialogs: checked
  visually on macOS + Windows at the default window size and in compact
  mode.
- `go test ./...`, `go vet`, gofmt, `bun run build`, `bun run lint`
  green.

## Progress

Steps 1–4 done on `feat/i18n-en-vi` (coded by GLM via paseo, reviewed
and committed step by step). Step 5 (user review of `vi.ts` wording)
and the visual check are open.

## Decisions made during implementation

- `ui_language` is also applied at startup from `main.tsx`; an older
  backend without the field is treated as `"system"`.
- Date formatters use the browser's regional tag when it matches the
  UI language (`en-GB` keeps day-first dates), else the bare language.
- Memoized translated text depends on the language: memo components
  call `useTranslation()` themselves; `useMemo` deps include `t` or
  `i18n.language` (meeting-list group headers, flow/mind-map layouts).
- Quoted UI names inside messages use i18next nesting
  (`“$t(labels.engine.whisper)”`) so they always match the real label.
- `<Trans>` tags must not be HTML void element names: `<link>` parsed
  as self-closing and rendered an empty anchor; renamed `<ext>`.
- Error codes: `ApiError` translates known codes in its constructor, so
  every existing `err.message` call site is localized without changes;
  `serverMessage` keeps the English. Code-based checks replaced two
  regexes on the message text in MeetingDetail.

## Validation

- `go vet ./...`, gofmt, case-collision check, `go test ./...` green;
  `TestConfigEndpointUILanguage` and the error-code tests cover step 1/4.
- `bun run build` + `bun run lint` green (only the 3 pre-existing
  `components/ui` warnings).
- Type guard: a missing key in vi, an extra key in vi, and an unknown
  `t()` key each fail `tsc` (checked by hand).
- 742 keys; a script comparing `{{vars}}`, `$t()` nesting and tags per
  key between en and vi: 0 mismatches.
- An audit of every English string removed from components on this
  branch against en.ts found one lost paragraph (Cloud sync encryption
  explanation, restored) and one lost ellipsis (fixed); the rest were
  CSS classes, code, or text now produced by nesting/interpolation.
- `<Trans>` messages with links/kbd rendered with react-dom/server in
  both languages.
- Still to do: visual check (macOS + Windows, default size + compact
  mode) with Vietnamese strings, and the user's pass over `vi.ts`.
