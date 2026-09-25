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

- **§0 `/end` hang — root cause.** `Pipeline.Stop()` closed the source
  and waited for the feeder goroutine to finish *before* cancelling the
  pipeline's `context.Context`. The feeder's only way to give up on an
  engine that isn't currently reading `pcmCh` is its own
  `case <-ctx.Done()` — so if the engine goroutine is blocked inside
  synchronous work that doesn't involve reading `pcmCh` (the real
  trigger: `WhisperEngine.transcribe`'s `http.Client.Do` against a
  whisper server that accepts the TCP connection but never responds —
  confirmed in this sandbox, connecting to a closed local port times
  out instead of refusing instantly, so the window is long enough to
  hit every time), `Stop()`'s wait and the feeder's only unblock signal
  were circularly ordered: `Stop()` → wait for feeder → feeder needs
  `ctx.Done()` → `ctx` is cancelled only after `Stop()`'s wait returns.
  Reproduced with a real `whisper` + `mic` meeting against an
  unreachable `whisper_url`; `SIGQUIT`'d the process mid-hang and read
  the goroutine dump: `Pipeline.Stop` parked on `<-p.feederDone`
  (pipeline.go:128), the feeder parked on its send-or-done select
  (pipeline.go:71), the results goroutine parked on `for range resultCh`
  (pipeline.go:81), and `WhisperEngine.Stream`'s goroutine parked inside
  `http.Client.Do` (engine_whisper.go:102) — nothing left free to move
  any of them.
  **First fix attempt was wrong, caught in review before merging:**
  cancelling `ctx` immediately after `src.Close()` does unblock the
  stuck case, but it also makes every drain-path `select { case ch <-
  x: ; case <-ctx.Done(): }` (the feeder's send to `pcmCh`, Soniox's
  send to `resultCh`) race an already-closed `ctx.Done()` against a
  genuinely-successful send — Go's `select` picks pseudo-randomly
  between ready cases, so a perfectly healthy shutdown could
  non-deterministically drop the last chunk or utterance. Worse for
  Soniox specifically: its reader's 3 s force-close timer starts
  counting from `ctx.Done()`, which would now fire before the
  end-of-audio frame is even sent, shrinking the window the server has
  to flush its final tokens instead of only kicking in as a backstop
  after a normal handshake.
  **Actual fix: bounded graceful drain.** `Pipeline.stop(grace)` closes
  the source, then waits (via a combiner goroutine + `select`) for both
  the feeder and results goroutines to finish **without touching
  `ctx`** for up to `grace` (`stopGraceDeadline` = 5 s in production;
  `Stop()` is a one-line wrapper `stop(stopGraceDeadline)`). Every
  drain-path `ctx.Done()` case stays unready during that window, so the
  real send/receive always wins — a normal end (including an engine's
  legitimate post-end-of-audio tail, e.g. Soniox's finals or a
  slightly-delayed last chunk) is never raced. Only if the drain hasn't
  finished within `grace` — the stuck case — does it call `p.cancel()`
  and wait (now unbounded but fast, since every engine wires this same
  `ctx` into its blocking work) for the drain to actually finish;
  `cancel()` is always called exactly once on every path, to release
  `ctx`'s resources. **Does not touch M19's Mixer ownership protocol**
  — `Mixer.Close()`/`SetInputs` and their input-ownership rules are
  untouched; the bug and the fix are entirely in `pipeline.go`'s
  shutdown ordering, one level above the Mixer.
  Regression tests (both use an unexported `stop(grace)` seam so they
  run with a short grace instead of the production 5 s):
  `TestPipelineStopUnblocksStuckEngine` (a fake engine that never reads
  `pcmCh` until `ctx.Done()`) asserts `stop()` both returns within a
  generous deadline *and* doesn't overshoot `grace` by much — confirmed
  it fails (hangs past the deadline) with the original immediate-cancel
  code and passes with the bounded-drain fix.
  `TestPipelineStopDrainsTailBeforeCancelling` (a fake engine that
  drains `pcmCh` normally, then — after it closes, like Soniox waiting
  for its server's finals — waits a short delay before emitting one
  last `Result`) asserts that result lands in the transcript; confirmed
  it fails 10/10 runs against the immediate-cancel version (the tail is
  dropped every time at this timing, not just occasionally) and passes
  against the bounded-drain fix.

- **§2 engine state in `/levels`.** Added `stt.EngineState` (a plain
  string, not a closed enum — see below) and an optional
  `stt.EngineStatusReporter` interface (`EngineStatus() (state
  EngineState, errMsg string)`), the same opt-in pattern
  `capture.DropCounter` already uses. `Pipeline` stores the engine it
  was given and exposes `EngineState()`, a type-assertion + delegate;
  `handleMeetingLevels` adds `"engine": {"state": "...", "error":
  "..."}` (error omitted when empty) only when the pipeline's engine
  implements the interface — `ok == false` omits the field entirely
  rather than inventing a value, kept existing fields unchanged.
  **Only `streaming` and `error` are ever produced; `connecting` and
  `reconnecting` are not, and this is a structural fact, not a
  to-do:** both engines' `Stream` establishes (or fails) their
  connection *synchronously*, before `Start` returns and a `Pipeline` —
  and so a `/levels` entry — exists to observe an in-between state; a
  failed connect surfaces as the meeting's `capture_error` instead.
  Neither engine retries a dropped connection (Soniox's session simply
  ends), so there is no `reconnecting` to report either. Per-engine:
  - **Whisper** has no persistent connection and, by design, no chunk
    failure that stops the stream (a failed request is dropped and the
    loop tries the next chunk — see its own comment). There is nothing
    else true to report, so it always returns `("streaming", "")`: an
    honest, if constant, signal that the loop is alive and willing to
    process audio (and gives the live view something to show for every
    engine, not just Soniox).
  - **Soniox** starts at `streaming` once its dial + config handshake
    succeed (inside `Stream`, before it returns). Its writer and reader
    goroutines both watch for connection failures, but a normal end
    *also* makes the reader's `ReadMessage` fail (the server closes the
    connection after flushing finals in response to our end-of-audio
    frame) — indistinguishable from a real drop by the error alone, and
    `ctx.Err()` cannot be used to tell them apart either, since §0's
    bounded-drain fix deliberately keeps `ctx` alive throughout a normal
    end too. The actual signal: an `atomic.Bool` the writer sets *after*
    `pcmCh` closes, right before it sends the end-of-audio frame. The
    reader only reports `EngineError` if that flag is still unset —
    i.e. the connection went away before we ever asked to end things.
  Hermetic tests, no real network: `internal/stt/engine_state_test.go`
  (`TestWhisperEngineStatusAlwaysStreaming`;
  `TestSonioxEngineStatusStreamsAfterConnect`;
  `TestSonioxEngineStatusNormalEndNotError` — confirmed it fails if the
  `endOfAudioSent` guard is removed;
  `TestSonioxEngineStatusUnexpectedDropIsError`, all against an
  `httptest` + `gorilla/websocket` fake server) plus
  `internal/server/sources_api_test.go`'s
  `TestMeetingLevelsEngineState` (a controllable fake engine, proving
  the HTTP response shape end to end for both states) and an addition
  to `TestMeetingLevelsShape` proving `engine` is omitted for an engine
  that doesn't implement the reporter.

## Progress

- [x] Go: `/end` hang root cause + fix + regression test
- [x] Go: engine state in levels (+ tests)
- [ ] Status row: elapsed, live source switch (+422), device, levels, drops, engine state
- [ ] Transcript: fill viewport, grouping, view modes, font size, notes label
- [ ] Compact overlay mode (+ API verification against module source)
- [ ] Screenshots + real Windows compact-mode check

## Validation

(to be filled)
