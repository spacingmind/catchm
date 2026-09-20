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
    `options.App.OnStartup`) somewhere the tray's goroutine/callbacks can
    reach it (a package-level variable guarded appropriately, or a small
    struct — this is a single-window desktop app, a simple approach is
    fine, don't over-engineer a DI framework for one context value).
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
   *can* run without the cgo webview/tray link step stays clean; the
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

(fill in: exact systray API calls used and how you confirmed their real
signatures, icon format/size/generation approach, how the
close-to-tray vs. real-quit flag is implemented, Linux system package
gap noted or resolved, whether `cmd/catchm-desktop` needed its own
go.mod.)

## Progress

- [ ] energye/systray dependency added
- [ ] Tray icon asset prepared
- [ ] OnBeforeClose hides window instead of quitting
- [ ] Tray menu: Open (WindowShow) + Quit (real exit)
- [ ] Non-desktop build/test/lint confirmed unaffected
- [ ] Docs note on Linux tray system package (or gap recorded)

## Validation

(to be filled honestly — including "not verified because X" where
that's the true state, matching M5's approach)
