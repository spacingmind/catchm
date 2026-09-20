# Plan: M11 — desktop shell system tray icon

- Status: active
- Created: 2026-09-20

## Goal

`cmd/catchm-desktop` (Wails v2, M5) currently opens one native window
with no tray presence — closing the window quits the whole app,
including any in-progress meeting capture. Add a system tray icon so
the app can run in the background (window hidden) while a meeting is
being captured, matching the "runs in the background during a meeting"
expectation set by `docs/ui-brainstorm.md`.

## Why this needs a third-party library

Checked the actual Wails v2.16.0 source in the local module cache
(`$(go env GOPATH)/pkg/mod/github.com/wailsapp/wails/v2@v2.16.0`):
`pkg/menu/tray.go` defines a `TrayMenu` type, but it is **not** wired
into `options.App` or any public runtime function in this version —
it's vestigial, not a working feature. Wails v2 does not have built-in
system tray support (that only lands properly in Wails v3, still
alpha). The community-standard workaround, used by many real Wails v2
apps, is running a system tray via
`github.com/energye/systray` (a fork of `getlantern/systray` that fixes
issues on Windows message-loop handling) alongside the Wails app,
coordinating window show/hide via Wails's own runtime package
(`github.com/wailsapp/wails/v2/pkg/runtime`).

## Scope

- Add `github.com/energye/systray` as a dependency of
  `cmd/catchm-desktop` only (don't pull it into the main module's
  other binaries — check whether Go's module graph requires any
  special handling here, e.g. if `cmd/catchm-desktop` needs its own
  `go.mod` or if the root `go.mod` already covers it fine as a normal
  dependency; record which).
- `cmd/catchm-desktop/main.go`:
  - Intercept the window's close event (Wails `options.App.OnBeforeClose`
    callback) to hide the window (`runtime.WindowHide(ctx)`) instead of
    quitting, *unless* the user chose "Quit" from the tray menu (track
    an explicit "really quit" flag/flow so closing via the tray's Quit
    item still exits normally — check `OnBeforeClose`'s exact signature
    and return-value contract in this Wails version before assuming how
    to prevent the default close).
  - Start the systray in its own goroutine (systray libraries typically
    require running on their own event loop via `systray.Run(onReady,
    onExit)`), with a tray icon (reuse the same mascot art already used
    for the window/exe icon — `cmd/catchm-desktop/build/appicon.png`,
    resized appropriately for a tray icon, which is much smaller than a
    window icon; check what format/size `energye/systray.SetIcon`
    expects, likely raw ICO/PNG bytes via `os.ReadFile` plus a resize
    step similar to what M5's icon generation already did with Pillow —
    Go-side resizing is fine too if simpler, e.g.
    `golang.org/x/image/draw` or just ship a pre-sized tray icon file
    generated once, your call, record it) and two menu items: "Open
    CatchM" (calls `runtime.WindowShow(ctx)` and brings it to front) and
    "Quit" (sets the real-quit flag and calls `runtime.Quit(ctx)`).
  - Store the Wails runtime context (`ctx context.Context` from
    `options.App.OnStartup`) stored somewhere the tray's goroutine/callbacks can
    reach it (a package-level variable guarded appropriately, or a small
    struct — this is a single-window desktop app, a simple approach is
    fine, don't over-engineer a DI framework for one context value).
  - **Tooltip reflects active-meeting state.** Added after UX research
    into similar apps (Granola, Fathom, Otter): the single most common
    complaint/failure mode in this category is the app *looking* idle or
    *looking* like it's recording when it actually isn't — users only
    find out after the meeting ends. Cheaply address this: a background
    goroutine polls `GET /api/meetings` on the same handler this binary
    already serves (call the handler in-process — no real HTTP round
    trip needed, or just hit `http://127.0.0.1:<port>` if that's simpler
    given how the server is already wired here, your call) every
    few seconds; if any meeting has `ended_at == null` (still active),
    call `systray.SetTooltip("CatchM — Recording")`, otherwise
    `systray.SetTooltip("CatchM")`. No second icon asset, no animation —
    just the tooltip text, which is the cheapest possible way to make
    the tray honestly reflect real state per the research's core lesson.
    Stop this poll goroutine cleanly on app quit (tie it to the same
    context/quit signal as everything else, don't leak it).
- `Taskfile.yml`/`README.md`: no changes needed unless the new
  dependency requires an extra system package on Linux beyond what's
  already documented (energye/systray needs `libayatana-appindicator3-dev`
  or `libappindicator3-dev` on Linux, in addition to the existing
  gtk/webkit2gtk packages — if you can determine this for certain, add
  it to the README's Linux system-dependency list; if you can't verify
  it in your sandbox, note it as a documentation gap in Decisions rather
  than guessing at the exact package name).

Out of scope: a full tray context menu beyond the two items above,
per-platform tray icon styling nuances (macOS template images, Windows
high-DPI tray icon variants), starting the app minimized-to-tray on
launch (it should still open its window normally on first launch, tray
is only for hide-on-close), auto-start-on-login.

## Constraints given this sandbox

Same situation as M5: this sandbox has no system webview/tray libraries
installed and (per M5's own findings) no passwordless sudo to install
them, so **you will almost certainly not be able to `go build`/`wails
build` this and see it actually run**. Write the code as correctly as
you can from the real API surface (read the actual
`energye/systray` and Wails v2 `runtime` package source in your module
cache the same way M5's implementer read the real Wails source — don't
guess plausible-sounding function names you haven't confirmed exist),
run whatever static checks *do* work without the cgo/webview link step
(`gofmt`, and `go vet`/`go build` on anything that doesn't require the
cgo path — note in Decisions exactly what could and couldn't be
verified, same honesty standard M5 set). A human with a real Windows
machine will do the final live build+verify pass after you're done —
this is expected and fine, don't treat an unverified build as a
blocker to finishing your part.

## Acceptance criteria

1. Whatever subset of `go build ./... `/`go vet ./...`/`gofmt -l .`
   *can* run without the cgo/webview/tray link step stays clean; the
   rest of the module (everything except `cmd/catchm-desktop`) must be
   completely unaffected — confirm `task build && task test && task
   lint` (the non-desktop path) still passes exactly as before.
2. Plan doc's Decisions section states plainly what was and wasn't
   verified, matching M5's precedent — no fabricated "it works" claims.
3. (To be confirmed later by a human with real Windows access, not in
   this run): the tray icon appears, closing the window hides it
   instead of quitting, "Open CatchM" restores the window, "Quit"
   actually exits the process.

## Decisions

- **systray API calls, all read from the actual v1.0.3 source** at
  `$(go env GOPATH)/pkg/mod/github.com/energye/systray@v1.0.3/` before
  writing the call sites: `systray.Run(onReady, onExit func())`
  (blocks until `systray.Quit()`; doc comment "Methods may be called
  from any Goroutine" per README), `SetIcon(iconBytes []byte)` — with
  the Windows implementation's own doc comment specifying "iconBytes
  should be the content of .ico for windows", `SetTooltip(string)`,
  `AddMenuItem(title, tooltip string) *MenuItem`, the fork's
  `(*MenuItem).Click(fn func())` callback style (this fork replaced
  upstream's `ClickedCh` channel), and `Quit()` — confirmed idempotent
  via `quitOnce.Do(quit)` in the source. Wails side likewise read from
  v2.16.0 source: `options.App.OnStartup func(ctx context.Context)`,
  `options.App.OnBeforeClose func(ctx context.Context) (prevent bool)`
  — return `true` prevents the close — and
  `runtime.WindowShow/WindowHide/Quit(ctx context.Context)`. Note
  v2.16.0's runtime package has no separate bring-to-front function;
  `WindowShow` is the restore path.
- **Icon**: `build/trayicon.ico`, generated once with Pillow 12.2.0
  (available in this sandbox) from the existing 512×512
  `build/appicon.png` — a two-size 16×16 + 32×32 ICO (verified via
  `file`: "MS Windows icon resource - 2 icons"). Embedded via
  `//go:embed build/trayicon.ico` rather than `os.ReadFile` so it can't
  be missing at runtime. No separate PNG needed: the primary target is
  Windows (real .ico), and systray's Linux backend decodes PNG-from-
  ICO fine (`_ "image/png"` registered inside its own package, which
  also decodes the PNG-compressed entries Pillow wrote into the ICO).
- **Close-to-tray vs real quit**: a `reallyQuit` bool guarded by the
  same `sync.Mutex` as the stored Wails context. `OnBeforeClose`
  returns `prevent=true` + `runtime.WindowHide` unless `reallyQuit` is
  set; only the tray's Quit item sets it (then `systray.Quit()` +
  `runtime.Quit(ctx)`).
- **Context storage**: single package-level `wailsCtx
  context.Context` + `reallyQuit` under one mutex, read via a small
  `appContext()` helper — deliberately minimal for a single-window app.
- **Tooltip poll (scope addition)**: a goroutine started from
  `onReady`, driven by `context.WithCancel` whose cancel is invoked
  both as systray's `onExit` and via `defer` from `run()` — so the
  poller stops on tray exit, app exit, or the run error path. It
  re-serves `GET /api/meetings` through the same in-process
  `http.Handler` via `httptest.NewRecorder` (no listener, no TCP port,
  exactly the spec's "call the handler in-process" option) every 3s,
  unmarshals `[]meeting.Meeting` (Go type, no duplicated JSON shape),
  and sets `"CatchM — Recording"` if any `EndedAt == nil` else
  `"CatchM"`. Fetch errors are skipped (tooltip stays at last-known
  state; next tick retries) rather than crashing the loop.
- **go.mod**: no separate module needed — root `go.mod` now carries
  `github.com/energye/systray v1.0.3` (plus its lone transitive dep
  `github.com/godbus/dbus/v5`) as a normal dependency, and since Go
  compiles per-package, nothing but `cmd/catchm-desktop` imports it;
  `cmd/catchm`'s build is untouched (verified below).
- **Linux system package**: resolved by reading the actual v1.0.3
  source rather than guessing — its go.mod requires only
  `godbus/dbus/v5`, and `systray_unix.go` (build tag `linux || freebsd
  || openbsd || netbsd`) implements the KDE StatusNotifierItem /
  canonical DbusMenu protocols directly over pure-Go DBus; the only
  `import "C"` in the repo is `systray_darwin.go`. So this fork has
  **no appindicator/GTK build dependency at all** — the spec's
  `libayatana-appindicator3-dev`/`libappindicator3-dev` premise
  (true for upstream `getlantern/systray`) does not apply to
  `energye/systray`. Runtime requirement on Linux is merely a DBus
  session bus plus a StatusNotifierHost (any modern desktop shell
  provides one); not verifiable in this headless sandbox, so the
  README was not changed. Recorded here instead of guessing a package
  name into the README.

## Progress

- [x] energye/systray dependency added (go get, v1.0.3, root go.mod)
- [x] Tray icon asset prepared (build/trayicon.ico, Pillow, 16+32px)
- [x] OnBeforeClose hides window instead of quitting
- [x] Tray menu: Open (WindowShow) + Quit (real exit via reallyQuit)
- [x] Non-desktop build/test/lint confirmed unaffected
- [x] Docs note on Linux tray system package (resolved from source: no
      extra package needed; README untouched)

## Validation

- **`go build ./...` / `go vet ./...` / `gofmt -l .`** (whole module,
  including `cmd/catchm-desktop`): all pass cleanly. Better than M5's
  situation — the only cgo in energye/systray v1.0.3 is in
  `systray_darwin.go` (not built on Linux), and its Linux backend is
  pure-Go DBus, so even the full `go build ./...` compiles the new
  desktop package without any tray system library. What remains
  unverified is only the runtime behavior (needs a real display/tray
  host) and the `wails build` cgo-webview link step (same gtk/webkit
  gap as M5, unchanged).
- **`task build && task test && task lint`**: all pass exactly as
  before — full `go test ./...` green across all internal packages
  (agent, batch, capture, config, meeting, pipeline, server, stt,
  tools, translate), `cmd/catchm` binary builds, web UI builds clean.
  `web/dist/index.html` remained unchanged after the rebuild (same
  content-hash build as pre-M11).
- **Acceptance criterion 3 (live tray behavior) — verified on real
  Windows (2026-09-20)**:
  - `wails build` succeeded end-to-end on a real Windows 11 machine
    (same toolchain from M5: Go 1.27.0 + WinLibs mingw-w64 + Wails CLI
    v2.16.0), pulling `energye/systray` and compiling cleanly.
  - Launched the real `catchm-desktop.exe`; confirmed via `Get-Process`
    the process started and was responding.
  - Confirmed the close-hides-not-quits behavior programmatically
    (rather than via more full-screen screenshots, to avoid capturing
    unrelated content on the live desktop): sent `WM_CLOSE` to the main
    window via `PostMessage`. Result: the process was still running
    afterward (same PID) with `MainWindowTitle` now empty — i.e. the
    window was hidden, not destroyed, and the app did not exit. This is
    exactly `OnBeforeClose`'s intended behavior.
  - **Not independently re-verified in this pass**: the tray icon's
    visual appearance in the notification area, and clicking "Open
    CatchM"/"Quit" from the tray menu directly (Windows hides new tray
    icons in the overflow area by default, and reaching it interactively
    risked more full-desktop screenshots of the user's active screen,
    which was avoided). The close→hide behavior above is strong
    evidence the tray/OnBeforeClose wiring initialized successfully
    (a crash in `systray.Run` or its onReady callback would likely have
    prevented the app from reaching a stable hidden state at all), but
    the tooltip text and menu click paths specifically are still only
    code-reviewed, not click-tested.
