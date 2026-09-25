# Plan: M21 — Live view: status row, mid-session source switch, levels, dual pane, compact overlay

- Status: active
- Created: 2026-09-25
- Repo: `catchm`, mostly `web/`, plus a small Go change (engine state).
- Depends on: M19 (merged: `/api/audio/sources`, `POST
  /api/meetings/{id}/source`, `GET /api/meetings/{id}/levels`), M20 +
  M20.1 (merged).

## Why

During a meeting the user can't tell whether CatchM is hearing the
right audio, can't switch source, and the transcript sits in a fixed
384 px box. Research (pplx, Otter/Granola/my-translator; see
`docs/plans/completed/m20-ui-shell.md` + the M20 audit) puts
operational status in the live view and asks for a compact
always-on-top mode for reading translation while another app is in
front.

## Scope

### 0. Go bug first: `/end` hangs (found during M20.1 verification)

In the Linux sandbox, `POST /api/meetings/{id}/end` never returned
for a real `whisper` engine + `mic` source meeting (the verifier had
to kill the server). On real Windows (M19 verification) ending mic
meetings worked, so this may depend on the backend/device or on the
whisper engine's shutdown when its server is unreachable. Reproduce it
(same setup: isolated `CATCHM_HOME`, real mic via malgo, whisper
engine with no whisper server running), capture goroutine dumps
(`SIGQUIT` or a pprof/`runtime.Stack` debug hook) while it hangs, find
the root cause (candidates: `Mixer.Close` waiting on pumps blocked in a
source read, pipeline `Stop` waiting on an engine flush with no
timeout, a device `Uninit` deadlock, a lock held across a blocking
call), fix it, and add a regression test that ends a meeting whose
engine/source blocks, bounded by a deadline. A meeting must always be
endable.

### 1. Status row (top of Live view)

- **Elapsed time** (from `started_at`, ticking each second).
- **Audio source** segmented control `[Mic] [System] [Both]` that
  switches **live** via `POST /api/meetings/{id}/source`. On 422, keep
  the old selection and show the reason inline (the backend keeps the
  old input). Plus a mic device select when the source includes the
  mic. Keyboard: Ctrl/⌘+1/2/3 while the Live view is focused
  (my-translator does this). Disable System/Both with the reason when
  `system.supported` is false.
- **Level meters** per active input from `GET
  /api/meetings/{id}/levels`, polled ~5 Hz **only while the Live view
  is mounted and the document is visible** (pause on `visibilitychange`).
  Show a compact bar meter plus a subtle "no signal" hint when a source
  stays near zero for more than a few seconds.
- **Drops**: if `dropped_ticks`/`dropped_chunks`/`dropped_samples`
  increase, show an amber "Audio is being dropped — connection or
  machine too slow" indicator (not red, not modal).
- **Engine/connection state** (needs the Go change below):
  connecting / streaming / reconnecting / error. Use precise labels
  ("Transcription connected", not just "connected"), green/amber/red/
  gray per the research.

### 2. Go: engine state in the levels response

Extend `GET /api/meetings/{id}/levels` with
`"engine": {"state": "connecting|streaming|reconnecting|error", "error": "..."}`,
tracked by the pipeline from what the engine actually does. For
example: "connecting" until the stream is established, "streaming"
once it is, "error" + message when the engine goroutine fails. Read the
real engine code (`internal/stt/engine_soniox.go`, the whisper engine,
`internal/pipeline`) and report states honestly: don't invent a
"reconnecting" state the engines can't produce. If none can, omit it
and record that. It stays cheap to poll: no disk I/O, and no locking
beyond what `/levels` already does. Hermetic tests with a fake engine
for each state transition. Keep the existing fields unchanged.

### 3. Transcript area

- Fills the remaining viewport height (no fixed `h-96`); keep the M16
  smart auto-scroll + "New lines ↓".
- Readability: 15–17 px body, 1.5–1.65 line height, max ~72ch measure,
  speaker label 12–13 px muted. Consecutive utterances from the same
  speaker are grouped under one label.
- **View modes** when translation is on: `Original + translation`
  (stacked per utterance, today's behavior), **`Side by side`** (two
  columns, one row per utterance so they stay aligned; not two
  independently scrolling panes), `Translation only`. Persist the
  choice (localStorage, non-secret).
- **Font size** A− / A+ (persisted), for reading at a distance.
- Notes textarea stays below, with a proper label (not placeholder-only),
  and "Add note" as the primary action of that block.

### 4. Compact overlay mode (desktop app only)

The same Wails v2 window (no multi-window in v2) switches into a
compact, always-on-top reading mode:
- Toggle: a button in the status row + **Ctrl/⌘+Shift+L** (focused-
  window shortcut; system-wide hotkeys are M22).
- Enter: save the current size/position (`window.runtime.WindowGetSize/
  WindowGetPosition`), `WindowSetMinSize(0,0)`, `WindowSetSize(~480,
  ~260)`, `WindowSetAlwaysOnTop(true)`, and render a compact layout
  (the latest few translated lines, or original + translation per the
  view mode, plus a tiny status strip showing the recording dot,
  source, level, and exit button). Exit restores size, position,
  min-size, and `WindowSetAlwaysOnTop(false)`.
- Re-assert always-on-top after the window is restored/shown (Windows
  can drop topmost after minimize).
- **Feature-detect** `window.runtime?.WindowSetAlwaysOnTop`. In a normal
  browser (`catchm serve`), hide the button and shortcut entirely.
- **Verify the exact runtime API names/signatures against the Wails
  v2.16 module source** (`go list -m -f '{{.Dir}}' github.com/wailsapp/wails/v2`,
  the JS runtime `frontend/runtime` or the injected `window.runtime`)
  before using them. Research came from Perplexity, which has been
  wrong in this project before.
- Frameless / translucent / Mica are **startup-only** in v2 and belong to
  M22 (native shell). M21 compact mode keeps the normal OS title bar.

## Out of scope

Frameless/Mica/custom title bar, system-wide hotkeys, native menus,
notifications, single instance, launch at login, auto-update (all
M22), multi-window, Wails v3 migration, dark mode.

## Acceptance criteria

1. `go build ./... && go vet ./... && gofmt -l . && go test -race ./...`
   and `cd web && bun run build` + lint clean.
2. Go tests for engine-state transitions (fake engine) and the extended
   levels shape.
3. Headless-Chromium screenshots (Playwright + isolated `catchm serve`,
   stubbing `/levels`/transcript via `page.route` where a live pipeline
   isn't available) of: the status row (streaming, error, drops
   warning, no-signal), a mid-session source switch including a 422
   failure, all three view modes, font size, and the compact layout
   rendered at 480×260. Save under `docs/screenshots/m21/`.
4. **Real Windows desktop check of compact mode.** Build the Wails
   desktop exe on the Windows machine (M5/M11 toolchain), toggle
   compact mode, and verify **programmatically** (no full-desktop
   screenshots) that the window gets `WS_EX_TOPMOST` and the expected
   size, and loses both on exit, via PowerShell + user32
   `GetWindowLong`/`GetWindowRect`. Use an isolated test `CATCHM_HOME`
   and never read `C:\Users\ADMIN\.catchm\`. Avoid opening the mic if
   possible (a Soniox-without-key meeting, or no meeting, is enough to
   exercise the window mode). If a meeting must record, delete its data
   afterward.

## Decisions

(fill in)

## Progress

- [ ] Go: `/end` hang root cause + fix + regression test
- [ ] Go: engine state in levels (+ tests)
- [ ] Status row: elapsed, live source switch (+422), device, levels, drops, engine state
- [ ] Transcript: fill viewport, grouping, view modes, font size, notes label
- [ ] Compact overlay mode (+ API verification against module source)
- [ ] Screenshots + real Windows compact-mode check

## Validation

(to be filled)
