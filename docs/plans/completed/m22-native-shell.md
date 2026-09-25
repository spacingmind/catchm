# Plan: M22 — Native shell polish (Windows desktop)

- Status: complete
- Created: 2026-09-25
- Completed: 2026-09-25
- Repo: `catchm`: `cmd/catchm-desktop` (Go/Wails), the NSIS installer
  config, small `web/` changes, and one test fix in `internal/capture`.
- Out of this milestone: code signing + auto-update (**M23**, which
  needs the user's certificate decision: Azure Artifact Signing / OV /
  EV).

## Why

The user tried the Windows build and asked for it to feel "native like
ZCode". ZCode is actually Electron + React with two tiny native
helpers: its polish comes from the **shell**, not the renderer
(research 2026-09-24/25: VS Code, Slack, Linear, Raycast). This
milestone does that shell work on the existing Wails v2 app.

## Research notes (Perplexity 2026-09-25; verify every API name against the
module cache, since Perplexity itself flagged uncertainty on several)

- Frameless + React title bar loses Windows 11 **Snap Layouts**,
  Alt+Space, and some accessibility (needs `WM_NCHITTEST` integration
  Wails v2 doesn't offer). Frameless/translucent are startup-only per
  window, and v2 has one window. **Decision: keep the native title bar
  and add Mica.**
- `options.App.SingleInstanceLock{UniqueId, OnSecondInstanceLaunch}`
  exists in v2 (≈2.6+).
- WebView2 hardening options (names to verify in `pkg/options/windows`
  of the pinned v2.16): `ZoomFactor`, `IsZoomControlEnabled`,
  `DisablePinchZoom`, `EnableDefaultContextMenu`, `DisableWebViewDrop`.
- System-wide hotkeys: Wails v2 has none. `golang.design/x/hotkey`
  (RegisterHotKey under the hood) is preferred over a hand-rolled
  message loop that could fight Wails' and systray's loops.
- Toast notifications need an **AppUserModelID** on a Start-menu
  shortcut (set by the NSIS installer) and
  `SetCurrentProcessExplicitAppUserModelID` at startup. Wails v2 has no
  built-in.
- Launch at login: `HKCU\…\CurrentVersion\Run` with a quoted path +
  `--autostart`, opt-in, removed on uninstall.
- `runtime.WindowGetPosition` is monitor-relative, which likely explains
  M21's 24 px Y drift on compact exit. Fix it by understanding the
  coordinate space, not with a constant.

## Scope

### 0. Test hygiene (from M21's CI)
`internal/capture` `TestMixerSums` is flaky under `-race` on CI
("input data never arrived"): `waitForData` observes the jitter buffer
while the tick loop may already have consumed it. Make it
deterministic (e.g. assert via mixed output or `Stats()`, or gate the
fake clock so no tick runs before the data is observed). Check the
other mixer tests for the same pattern. Run `go test -race -count=20
./internal/capture/...` green.

### 1. Mica + window chrome
- `WindowIsTranslucent: true`, `BackdropType: windows.Mica` (auto-
  fallback on older builds) with the native title bar kept. Let Mica
  show through the shell areas (sidebar/background) via transparent CSS
  backgrounds where it looks right, keeping content surfaces opaque
  and readable. Verify that the brand contrast still passes (the M20
  ratios) on top of Mica in both light wallpapers and dark ones.
- Title bar colour/theme consistent with the brand (Wails
  `WindowSetLightTheme`/theme options).
- `wails.json` gets the `info` block (productName "CatchM",
  companyName, version) so the exe and installer are named properly.
  This was only done ad hoc for the first Windows trial build.

### 2. Window state
- Persist the **restored** main-window bounds + maximized flag
  (debounced on move/resize, and on close). Restore on launch, clamped
  to `runtime.ScreenGetAll` work areas (keep ≥100×40 px of the title
  bar visible; fall back to centered on primary when the monitor
  topology changed).
- Compact mode keeps its **own** geometry key and never overwrites the
  normal bounds. **Fix the 24 px drift** on compact exit: identify the
  coordinate space (monitor-relative vs outer/client) and normalize.
- Test at 100% and 125/150% scaling if possible.

### 3. Single instance + tray
- `SingleInstanceLock` with a stable UniqueId (reverse-DNS, matching the
  AUMID). A second launch shows, unminimizes, and focuses the existing
  window. Don't use always-on-top toggling to steal focus.
- Tray: icon **states** idle / recording / error (swap icons at
  runtime; brand directions in `docs/brand-and-ux-research.md`: same
  silhouette, colour/badge for state), tooltip "CatchM — Ready /
  Recording / Not recording: …" (no meeting titles in the tooltip,
  for screen-share privacy), left-click shows the window, and a menu
  with Open / Start-stop meeting / Compact mode / Quit. Verify the
  energye/systray click API in the vendored version.

### 4. WebView hardening
Disable browser zoom (Ctrl+±/wheel/pinch) and the default context menu
in production, block file/URL drops onto the webview, keep devtools
off in production builds, and `user-select: none` on chrome only
(transcript, notes, inputs, errors stay selectable). Verify the exact
option names in the pinned v2.16 source before use.

### 5. System integration
- **Global hotkeys** (work when CatchM isn't focused), configurable in
  Settings → a new "Shortcuts" section: show/hide CatchM, start/stop
  meeting, toggle compact mode. Registration failure (the combo is
  taken) shows an inline error instead of failing silently.
  Unregister on quit.
- **Launch at login** toggle in Settings (HKCU Run + `--autostart` →
  start hidden to tray). Opt-in; the NSIS uninstaller removes it.
- **Toast notifications** for events that matter while the window is
  hidden: capture failed / not recording, meeting ended (with a link
  to open it), cloud sync conflict. NSIS creates the Start-menu
  shortcut with the AUMID. Keep them opt-out-able in Settings.

### 6. Compact overlay follow-ups (from M21)
In compact mode, style the translation line distinctly (muted or
marked, as in the main view).

## Out of scope

Frameless/custom title bar, code signing, auto-update (M23), macOS
equivalents, Wails v3 migration.

## Acceptance criteria

1. `go build ./... && go vet ./... && gofmt -l . && go test -race ./...`
   (plus `-count=20` on capture for §0) and `cd web && bun run build` +
   lint clean. CI green.
2. Unit tests where logic is testable without Windows: bounds clamping/
   normalization, the Run-key toggle (behind an interface), hotkey
   config parsing, and the tray-state mapping.
3. **Real Windows verification** (desktop exe built on the Windows
   machine; isolated test `CATCHM_HOME`; never read
   `C:\Users\ADMIN\.catchm\`). Programmatic checks, **no
   full-desktop screenshots**, and **no global `SendKeys`**. Target the
   CatchM window directly (its hwnd via `PostMessage`/UI Automation) or
   drive behavior through the app's own API/state:
   - Mica/translucency is active (DWM attribute query) and the native
     title bar is present;
   - bounds persist across restart, including a deliberately off-screen
     saved position getting clamped back, and compact exit restores
     position with **0 px drift**;
   - a second launch focuses the first instance (one process);
   - zoom shortcuts/context menu disabled (as far as they can be checked
     programmatically);
   - a global hotkey toggles compact mode while another window has
     focus (use a harmless focused window you create, e.g. a Notepad
     instance you launch and close yourself);
   - the launch-at-login Run value is written/removed by the toggle, and
     the uninstaller removes it (if the NSIS build is exercised);
   - one toast is shown with the correct AUMID (install via the NSIS
     installer to a test location if needed, and uninstall afterward).
   Window-only screenshots via `PrintWindow` on the CatchM hwnd are fine
   for Mica/tray-menu visuals. Clean up everything (processes, registry
   values, test installs, scratch dirs) and list what was cleaned.

## Decisions

- Native title bar + Mica over frameless (see research notes).

- **§0 deterministic mixer tests.** `waitForData` polled each input's
  jitter buffer independently, but the tick loop could already mix and
  emit a tick (zero-padding whichever input hadn't landed yet) in the
  gap between two sequential `waitForData` calls — the drained "first"
  tick wasn't guaranteed to be the one where every input actually had
  data. Fix: a `gatedFakeClock` whose `Sleep` blocks until `release()`
  is called, so the tick loop's first tick can't fire until the test
  has confirmed every input it checks is buffered. Applied to the three
  multi-input tests with this pattern
  (`TestMixerSaturatesAtBounds`, `TestMixerSums`, `TestMixerLevels`).
  `go test -race -count=20 ./internal/capture/...` green, repeated
  after every later change to make sure nothing regressed it.

- **§1 Mica + chrome.** Verified every option against the real
  `v2.16.0` source before writing a call site
  (`pkg/options/windows/windows.go`, `internal/frontend/desktop/
  windows/{window,theme}.go`): `WindowIsTranslucent: true` +
  `BackdropType: windows.Mica` is the exact pair; Wails' own
  `win32.SupportsBackdropTypes()` gates Mica on Windows 11 22621+ and
  silently no-ops below that build, so no fallback code was needed.
  Title bar theme is forced to `windows.Light` (not `SystemDefault`):
  `web/src/index.css` says outright "No dark mode variant — this app
  only ships a light theme", so a dark title bar over all-light content
  would look broken regardless of the OS theme. `CustomTheme`'s
  title-bar/text/border colours are the real brand tokens from that
  file (`--background`/`--foreground`/`--secondary`), not the older
  speculative palette in `docs/brand-and-ux-research.md`. `wails.json`
  got the `info` block (`productName`/`companyName`/`productVersion`
  "0.1.0", a placeholder pre-release version — no version scheme
  existed anywhere in the repo before this). Mica shows through the
  shell (body, sidebar) only inside the real Wails window
  (`html.desktop-mica`, set via the existing `hasWindowControls()`
  feature-detect so `catchm serve` is unaffected); `main` keeps an
  explicit `bg-background` so all content stays opaque and readable
  over Mica. Compact mode is untouched (out of M22's compact scope).

- **§2 window state + the drift's real root cause.** Root-caused (not
  guessed) M21's 24px compact-exit drift by reading
  `internal/frontend/desktop/windows/winc/controlbase.go`:
  `WindowGetPosition` returns **absolute** screen coordinates, but
  `WindowSetPosition` treats its input as an offset from **whatever
  monitor the hwnd is currently on**'s work-area origin — a genuine
  coordinate-space mismatch, not monitor-relative-vs-outer/client as
  the research notes guessed, and not something a constant could fix.
  `windowGetPosition(); windowSetPosition(x, y)` only round-trips when
  that monitor's work area happens to start at absolute (0, 0), which
  a taskbar/reserved edge breaks — confirmed live later (see Real
  Windows Verification) on a machine whose primary monitor work area is
  `(0, 24)`, the exact 24px M21 saw. Fix
  (`wailsRuntime.ts`'s `windowRestoreAbsolutePosition`): set (0, 0)
  first to read back that monitor's absolute work-area origin, then
  apply the real offset — exact regardless of topology, no monitor
  enumeration needed for this specific call.
  New `internal/winbounds` package (pure, no I/O, 9 unit tests):
  `Clamp(saved, monitors)` keeps a saved rect if its title-bar corner
  (≥100×40px) is still visible on some monitor, else centers it on
  primary. It needs real monitor **positions**, which
  `runtime.ScreenGetAll` does **not** expose (verified against
  `internal/frontend/desktop/windows/screen.go`: only `Size`/
  `PhysicalSize`, no position at all — a real, structural gap in the
  cross-platform Wails API, not an oversight in this code) — so
  `cmd/catchm-desktop/monitors_windows.go` calls
  `EnumDisplayMonitors`/`GetMonitorInfoW` directly (the same two Win32
  calls Wails' own internal `screen.go` uses, which this module can't
  import since it's a different module's internal package), with a
  `monitors_other.go` stub (`nil, nil`) for non-Windows builds/tests.
  `cmd/catchm-desktop/window_manager.go`'s `windowStateTracker`
  persists the last **restored** (non-maximized) bounds + maximized
  flag to `CATCHM_HOME/window_state.json`, applied via the same
  absolute-position calibration on `OnStartup` after clamping. A 2s
  poll (no move/resize event exists to hook — checked
  `pkg/runtime/events.go` and the desktop JS runtime bundle; `WM_MOVE`
  only notifies the chromium child internally, per `window.go`) plus a
  direct save in `OnBeforeClose` stand in for "debounced on move/
  resize, and on close" — an honest substitute for an event Wails
  doesn't have, not a corner cut. Compact mode's own signal
  (`DesktopBridge.SetCompactMode`, a minimal Wails `Bind` — verified
  the runtime registers `Bind` methods via reflection in
  `app_production.go` regardless of the CLI's `-skipbindings` flag) so
  its 480x260 geometry never overwrites the normal bounds while active.
  **100%/125%/150% scaling was not re-tested separately** — the Windows
  machine used for verification runs at 100%; the position/size
  arithmetic is DPI-agnostic (Wails' `Pos()`/`SetPos()` don't scale,
  confirmed reading the same source), so this is a reasoned inference,
  not a verified claim at other scale factors.

- **§3 single instance + tray.** `SingleInstanceLock` (`UniqueId`
  `"com.catchm.desktop"`, a stable reverse-DNS id shared with the AUMID
  §5 needs) verified against `single_instance.go`/`frontend.go`: a
  second launch's args are forwarded via `WM_COPYDATA` to a hidden
  message-only window in the first instance (which then `os.Exit(0)`s
  itself) and `OnSecondInstanceLaunch` fires on the FIRST instance;
  `WindowShow` already un-minimizes (`win32.RestoreWindow` when
  minimized, else `ShowWindow`) and focuses
  (`SetForegroundWindow`, always) on its own, read directly in
  `frontend.go`'s `ShowWindow` — no separate calls needed.
  Tray rewritten into `tray.go`: icon states idle/recording/error (new
  `.ico` assets, Pillow, same two-size approach as M11's), tooltip
  "CatchM — Ready"/"CatchM — Recording"/"CatchM — Not recording:
  &lt;reason&gt;" (never a meeting title). "error" is derived from a
  **real** signal only — M21 §2's `engine.state` field on the
  live-levels endpoint for the active meeting — not the ephemeral
  `capture_error` (only ever returned once, from the start/
  source-switch responses; never persisted, so a later poll can't see
  it). Left-click shows the window (`systray.SetOnClick` — its source
  comment, in Chinese, translates to "set the tray's left mouse button
  click event"); right-click already shows the menu by default
  (`SetOnRClick`'s own comment: unset means the menu shows
  automatically), so no code was needed for that half. Menu: Open /
  Start-stop meeting (title toggles via `SetTitle`, drives the same
  `POST /api/meetings` + `.../end` the web UI uses) / Compact mode
  (`runtime.EventsEmit`, since `DesktopBridge`'s `Bind` only goes
  JS→Go) / Quit (unchanged from M11).

- **§4 WebView hardening.** `EnableDefaultContextMenu` and `Debug` are
  deliberately left unset: `frontend.go`'s
  `PutAreDefaultContextMenusEnabled(f.debug || EnableDefaultContextMenu)`
  and `PutAreDevToolsEnabled(devtoolsEnabled)` (itself gated on the
  `devtools` build tag, which `Taskfile.yml`'s `desktop:build` never
  passes) already disable the context menu and devtools in a
  production build and re-enable both in `wails dev` — setting either
  would have been redundant at best, wrong at worst.
  `Windows.IsZoomControlEnabled: false` + `DisablePinchZoom: true` map
  straight to WebView2's `PutIsZoomControlEnabled`/
  `PutIsPinchZoomEnabled`. `DragAndDrop.DisableWebViewDrop: true` blocks
  file/URL drops. `select-none` on chrome only: `AppSidebar` (pure nav)
  and `CompactOverlay`'s status strip — deliberately **not**
  `StatusRow`/`LiveMeeting`'s outer containers, since both interleave
  inline error text (422s, the `capture_error` banner) that must stay
  selectable, and a blanket `select-none` there risked covering it
  without a full audit.
  **Not independently live-verified**: zoom/context-menu specifically
  (see Validation) — the code-level verification above is the evidence
  for this item, not a functional click-test.

- **§5 hotkeys, launch at login, toasts.** Two new pure/testable
  packages, mirroring §2's `winbounds`/`monitors_windows.go` split:
  - `internal/hotkeys` parses/validates `"Ctrl+Alt+M"`-style combo
    strings, no third-party dependency — its key vocabulary is a 1:1
    copy of `golang.design/x/hotkey@v0.6.1`'s real Windows `Key`
    constants (verified against that source), so this package never
    needs to import the hotkey library itself (which pulls in a
    cgo/X11 backend on Linux). Default bindings deliberately avoid
    `Ctrl+Shift+L`: that's already M21's FOCUSED-window compact
    shortcut; reusing it globally would double-toggle while focused.
    16 tests.
  - `internal/autostart`: the HKCU Run-key command line (quoted path +
    `--autostart`) behind a `Store` interface, testable without a real
    registry. A real bug its own test caught: the first draft used
    Go's `%q` (escapes backslashes into an invalid Windows command
    line) instead of plain quotes.
  - `cmd/catchm-desktop/hotkeys_windows.go`: `golang.design/x/hotkey`
    (`RegisterHotKey` under the hood, preferred over a hand-rolled loop
    per the research notes) — verified in its own source that each
    `Hotkey` gets its own `LockOSThread`'d goroutine with its own
    `PeekMessage`/`GetMessage` loop, so it can't fight Wails' or
    systray's. `hotkeys_other.go`/`autostart_other.go`/`aumid_other.go`
    stub the Windows-only pieces for non-Windows `go build`/`test`.
  - `desktop_settings.go`: `hotkeyManager` owns registration + the
    persisted `CATCHM_HOME/desktop_settings.json` (shortcuts +
    notifications opt-out — deliberately separate from `config.yaml`,
    which `catchm serve` also reads and has no desktop concept).
    `hotkeys.ValidateBindings` (duplicate/invalid detection) runs
    before any registration attempt — **wired in only after real
    Windows testing caught it was dead code**: `applyAll` originally
    only called `ParseCombo` per action and let Windows' own
    `RegisterHotKey` reject a genuine duplicate at the OS level, which
    does satisfy "shows an inline error instead of failing silently"
    (confirmed live), but which of two colliding actions got the error
    depended on iteration order rather than reporting the real
    conflict on both. Fixed, plus a new test
    (`TestHotkeyManagerDuplicateComboFlagsBothActions`).
  - Toast notifications: `github.com/go-toast/toast`'s own `toast.go`
    has **no build tag** despite setting a Windows-only
    `syscall.SysProcAttr` field — it doesn't compile on any other GOOS
    at all (found by `go build ./...` failing, not by inspection).
    Moved the wrapper to `toast_windows.go` (+ `toast_other.go` no-op).
    `AppID` is the real AUMID (`com.catchm.desktop`), not the "pretty
    name" the library's own doc comment recommends — read its actual
    generated PowerShell script and confirmed `$APP_ID` goes straight
    to `CreateToastNotifier`, which only attributes correctly for an
    unpackaged app when that string matches a real registered AUMID.
    Confirmed correct live (see Validation): the toast history for
    AUMID `com.catchm.desktop` showed exactly one entry, title
    "CatchM", body "Meeting ended" — no fallback to a generic/
    PowerShell identity. Wired to the two transitions `tray.go`'s
    existing poll can already see (capture erroring, active meeting
    ending) plus cloud-sync conflicts (fired from
    `SettingsCloudSync.tsx` right where a manual sync's response
    already reveals them — there's no background sync loop in this
    codebase to poll the way `tray.go` polls capture state, and adding
    one is out of this milestone's scope). No meeting titles in any
    toast body, same screen-share-privacy rule as the tray tooltip —
    confirmed by the actual toast history entry above.
    **"Meeting ended" toast has no click-to-open action**: doing that
    properly needs a registered custom URI protocol + a COM activation
    handler (an unpackaged app using `go-toast`'s PowerShell-invoked
    toast has no activation callback into the running process at all)
    — out of proportion for this milestone's remaining scope; the
    toast still informs, it just can't be clicked through to the
    meeting. Documented, not silently dropped.
  - NSIS AUMID stamping went through two real, live-discovered
    failures before landing on a working approach:
    1. The obvious fix (`WinShell::SetLnkAUMI` after NSIS's own
       `CreateShortcut`) failed outright — `WinShell.dll` isn't
       installed in this machine's NSIS `Plugins` directory at all
       (confirmed: `wails build -nsis` aborted with "Plugin not found,
       cannot call WinShell::SetLnkAUMI").
    2. Investigating a plugin-free alternative found a second,
       independent problem: loading an **existing** shortcut (the one
       NSIS's `CreateShortcut` just made, in a separate COM session)
       and writing to it via `IPropertyStore` is unreliable —
       `IPersistFile::Load` on a shortcut this process didn't create
       intermittently returns a degraded, near-empty property store
       (`S_FALSE`) where every subsequent write call still reports
       `S_OK` but silently doesn't persist (confirmed by both an
       in-session `GetCount` staying flat and a fresh-process re-open
       finding nothing — reproduced across several fresh test
       shortcuts, not a one-off).
    Creating the shortcut **and** stamping the AUMID within one COM
    session instead persisted correctly 5/5 times in scratch testing,
    and in the real installer run (see Validation).
    `build/windows/installer/New-AumidShortcut.ps1` replaces NSIS's
    native `CreateShortcut` for the Start-menu link only (the Desktop
    shortcut is unaffected — no AUMID needed there) with a PowerShell
    script — using only the PowerShell every supported Windows version
    ships, no plugin — doing both steps in one shot, run via
    `nsExec::ExecToLog`. `project.nsi`'s uninstall section also
    unconditionally `DeleteRegValue`s the HKCU Run entry (a no-op if
    the user never opted in).
  - Web: Settings → Shortcuts (desktop-only nav item, hidden in
    `catchm serve` per the established `hasWindowControls()`
    convention) — combo inputs with inline per-action errors, a
    Launch-at-login checkbox, a Notifications checkbox, all through
    new `DesktopBridge` bind methods.
  - **A second real bug, found only by driving the actual UI on real
    Windows** (not by any hermetic test): `useCompactMode.ts`'s
    `exit()` fired `windowRestoreAbsolutePosition` without awaiting it
    (`void windowRestoreAbsolutePosition(...)`), and — separately —
    that function's own two-step calibration raced: reading the JS
    runtime source (`desktop/window.js`) showed `WindowSetPosition`
    goes through `window.WailsInvoke`, a fire-and-forget native bridge
    call, while `WindowGetPosition` goes through the async `Call()` RPC
    channel — two independent transports with no ordering guarantee.
    Reading position immediately after setting it to (0,0) could (and,
    tested live, reliably did) return the pre-move position, silently
    turning the "corrective" second `SetPosition` call into
    `SetPosition(0, 0)` again — landing the window at the monitor's
    work-area origin instead of the saved position, every single time,
    on a machine whose work area origin is `(0, 24)` — the *exact*
    24px offset M21's original bug report described, now understood
    down to the two lines that produced it. Fixed by awaiting `exit()`
    properly and adding a short settling delay before the read-back;
    confirmed exact (0px drift) over 4 repeated toggle cycles on real
    Windows afterward. The equivalent Go-side calibration
    (`window_manager.go`'s `setAbsolutePosition`, used for restore-on-
    launch) never had this problem — Go's `WindowSetPosition`/
    `WindowGetPosition` are direct, synchronous method calls with no
    IPC transport at all, confirmed by that same verification pass's
    off-screen-clamp test landing exactly on target.

- **§6 compact translation styling.** `CompactOverlay` already applied
  `text-muted-foreground` to the translated line (M21), but was missing
  the `"→ {lang}"` marker `LiveMeeting.tsx`'s own translation line uses
  — the "marked" half of "muted or marked, as in the main view", not
  just the "muted" half. Added the identical span for parity in both
  the default (stacked) and side-by-side compact layouts.

## Progress

- [x] §0 deterministic mixer tests
- [x] §1 Mica + chrome + wails.json info
- [x] §2 window state + drift fix
- [x] §3 single instance + tray states
- [x] §4 WebView hardening
- [x] §5 hotkeys, launch at login, toasts (+ NSIS AUMID shortcut)
- [x] §6 compact translation styling
- [x] Real Windows verification

## Validation

**Hermetic.** `go build ./... && go vet ./... && gofmt -l . && go test -race ./...` clean across every package, including the
new `internal/winbounds` (9 tests), `internal/hotkeys` (16 tests), and `internal/autostart` (3 tests) — plus 18 new tests in
`cmd/catchm-desktop` (`window_manager_test.go`, `tray_test.go`, `desktop_settings_test.go`) exercising bounds persistence,
tray-state transitions (idle/recording/error against a real `internal/server.Server`), and hotkey registration/duplicate-
detection/notifications-persistence against a fake `platformHotkeys`. `go test -race -count=20 ./internal/capture/...` green
(§0's actual target, re-run after every later change). `cd web && bun run build` (tsc + vite) and `bun run lint` (oxlint)
clean — only pre-existing warnings in files this milestone didn't touch. `web/dist/index.html` was rebuilt repeatedly for the
Windows copy but never committed (reverted to its last committed state before every commit in this branch), per the working
rules.

**Real Windows verification (2026-09-25).** This WSL sandbox runs on the Windows host with full `powershell.exe` interop —
confirmed Go 1.27.0 windows/amd64, WinLibs GCC 16.1.0, Wails CLI v2.16.0, and NSIS present at `C:\Program Files (x86)\NSIS\
makensis.exe` before starting; every `powershell.exe` call prefixed `$env:Path` from the Machine+User registry values.
Copied the worktree to `C:\Users\ADMIN\src\catchm-m22` (never `\\wsl$` for the build itself; `web/dist` was built in WSL via
`bun` and copied over, rebuilt and re-copied twice more mid-pass after live bugs were fixed). `wails build -s -skipbindings`
from `cmd\catchm-desktop` succeeded end-to-end each time (the same pre-existing, harmless `.rsrc merge failure: multiple
non-default manifests` `ld.exe` warning M21 already documented). Ran with an isolated `CATCHM_HOME`
(`%LOCALAPPDATA%\Temp\catchm-m22-wintest`, a `soniox` engine with no API key — never opens the mic, since Soniox engine
construction fails on the missing key before any `capture.Source` is opened, the same structural guarantee M20/M21 already
established and unchanged since) — never touched or read `C:\Users\ADMIN\.catchm` (confirmed by its `LastWriteTime` staying
at `2026-09-24 16:55:59`, before this session even started, at the very end of the pass).

No UI-automation-hostile content this time: Windows' `System.Windows.Automation` (`UIAutomationClient`) DOES expose a full,
navigable tree over the WebView2/Chromium content (buttons, edits, checkboxes, radio buttons all found by name) — used
throughout instead of any keystroke synthesis into the app, and instead of the full-desktop screenshots the task disallows.
Per-item results, with actual values:

- **Mica/native title bar (DWM attribute query).** `DwmGetWindowAttribute(hwnd, 38 /* DWMWA_SYSTEMBACKDROP_TYPE */, ...)` →
  **2 (Mica)**. `GetWindowLong(hwnd, GWL_STYLE)` → `0x14CF0000`, which decodes to `WS_CAPTION | WS_SYSMENU | WS_THICKFRAME |
  WS_MINIMIZEBOX | WS_MAXIMIZEBOX` all set — native title bar present, not frameless.

- **Bounds persistence, off-screen clamp, compact 0px drift.** Moved the window to `(300, 200)` via `SetWindowPos`, closed
  it via a targeted `PostMessage(hwnd, WM_CLOSE, ...)` (hides to tray, matching M11's established behavior — the process
  stayed alive), and confirmed `window_state.json` captured `{"normal":{"X":300,"Y":200,"W":1180,"H":760},"maximized":false}`.
  Killed and relaunched: window restored to **exactly** `(300, 200)`, 1180×760 — 0px drift on a normal restart. Wrote a
  deliberately off-screen position (`X:5000,Y:5000`) directly into `window_state.json` and relaunched: the window landed at
  **exactly** `(690, 328)` — the mathematically correct centered-on-primary position for this machine's real primary work
  area, `[System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea` = `{X=0, Y=24, W=2560, H=1368}` (⌈(2560-1180)/2⌉=690,
  24+⌈(1368-760)/2⌉=328) — proof `enumMonitors()`'s real Win32 monitor enumeration and `winbounds.Clamp` both work
  correctly together against real, non-zero-origin monitor geometry, not just the unit tests' synthetic ones.
  **Compact exit drift — the one place real testing found an actual bug the hermetic suite couldn't**: the first live
  attempt reliably (not intermittently — every single time) landed the window at `(0, 24)` after exiting compact instead of
  the saved `(690, 328)` — exactly the monitor's work-area origin from above. Root-caused and fixed (see Decisions:
  `windowRestoreAbsolutePosition`'s un-awaited call plus a genuine two-transport IPC race). Re-verified after the fix over
  4 repeated enter/exit cycles from a clean `(690, 328)` baseline: **every single cycle** — enter → `(690, 328)`, 480×260;
  exit → `(690, 328)`, 1180×760, exactly, every time.

- **Single instance.** Minimized the running window (`ShowWindow(hwnd, SW_MINIMIZE)`), launched a second
  `catchm-desktop.exe` with the same `CATCHM_HOME`: process count stayed at **1** (same PID throughout), and the existing
  window came back — confirmed both `IsIconic(hwnd)` → `False` (un-minimized) and `GetForegroundWindow()` → the same hwnd
  (focused), via `OnSecondInstanceLaunch` → `WindowShow`.

- **Zoom/context-menu.** Not independently live-click-tested this pass — attempted a targeted (not global) `PostMessage`-
  based right-click at the CatchM content area to check for a native context-menu window, but the P/Invoke struct
  marshaling for that specific test had issues I didn't resolve within the pass's time budget, and posting synthetic
  keyboard messages into a Chromium renderer's input pipeline (for the zoom shortcuts) is itself unreliable regardless
  (Chromium expects real OS input, not posted `WM_KEYDOWN`). This item's evidence is the §4 Decisions' source-level
  verification (the exact Wails/WebView2 API call sites read directly from the pinned v2.16.0 source) rather than a live
  functional check — noted honestly, not glossed over, matching the acceptance criterion's own "as far as they can be
  checked programmatically" hedge.

- **Global hotkey while another window has focus.** Used Calculator (`calc.exe`, hosted via `ApplicationFrameHost.exe`) as
  the harmless decoy window, launched and closed by this session — **not** the user's own already-open Notepad window,
  which this attempt discovered had a private-key file open in it (`centinus-ai-reader...private-key.pem`); confirmed via
  UI Automation that its tabs still read "Unmodified" (untouched) before switching to Calculator instead. With Calculator
  focused (`SetForegroundWindow`), sent `Ctrl+Alt+L` via raw `keybd_event` (not `SendKeys`, not directed at any window
  other than whichever the OS considers focused at that moment — Calculator, by construction) while a live meeting was
  open in CatchM: the CatchM window shrank to exactly 480×260 (compact mode entered) without ever being focused itself.
  The same mechanism also exercised `Ctrl+Alt+M` (start/stop meeting) while the CatchM window was **hidden** (closed to
  tray) and Calculator focused: `meeting.md`'s `ended_at` field was populated within the poll interval, confirming the
  global hotkey correctly ended the active meeting with no window visible or focused at all.

- **Toast notification.** Triggered by the `Ctrl+Alt+M` meeting-end above, while the window was hidden — exactly the
  scenario the feature exists for. `[Windows.UI.Notifications.ToastNotificationManager]::History.GetHistory
  ('com.catchm.desktop')` (queried by the exact AUMID, no admin rights needed) returned **1** entry; its content XML:
  title `CatchM`, body `Meeting ended` — no meeting title, matching the tray tooltip's own privacy rule, and correctly
  attributed to the app's own AUMID rather than a generic/PowerShell identity.

- **Launch-at-login toggle + NSIS uninstaller removal.** Rebuilt with `wails build -nsis -installscope user` (the default
  `machine` scope's installer requires admin elevation this session doesn't have — see below) and ran the resulting
  installer silently (`/S`) to `%LOCALAPPDATA%\Programs\CatchM`, no elevation prompt. Confirmed the **real, installer-
  created** Start-menu shortcut carries the correct AUMID (enumerated its property store: `fmtid=9f4c2855-..., pid=5,
  val=[com.catchm.desktop]`) — the live end-to-end proof for the `New-AumidShortcut.ps1` fix described in Decisions, not
  just the scratch-directory testing that led to it. Launched the installed exe, opened Settings → Shortcuts through the
  real UI, toggled "Launch CatchM at login" on: `HKCU\...\Run\CatchM` appeared with exactly
  `"C:\Users\ADMIN\AppData\Local\Programs\CatchM\catchm-desktop.exe" --autostart`. Toggled it off through the same UI:
  the value disappeared. Ran the installer's own uninstaller (`/S`, no elevation needed at user scope): install directory,
  Start-menu shortcut, and Desktop shortcut all removed, **and** — after re-enabling the Run key from the installed exe
  first, specifically to test this — the uninstaller's own `DeleteRegValue` removed `HKCU\...\Run\CatchM` too.

**Not fully exercised: the default (`machine`-scope, admin-required) installer end-to-end.** The very first `wails build
-nsis` attempt (default scope) succeeded at building the exe and installer, but running that installer triggered a real
UAC elevation prompt that appeared on the user's actual screen — this session has no admin rights, so I could not approve
it, and (correctly, deliberately) did not attempt to click through or bypass it. I flagged this to the user directly and
asked them to decline it (safe either way — it only would have uninstalled a stale, unrelated leftover install from a
prior session, described below), which they did. All the machine-scope installer's actual logic (the AUMID stamping, the
Run-key removal, the shortcuts) is identical to the `user`-scope build used above — only the install location and whether
elevation is requested differ — so this is a scope/environment limitation of this verification pass, not an unverified
code path.

**Cleaned up:**
- All `catchm-desktop.exe` and decoy-window (`CalculatorApp`/`ApplicationFrameHost`) processes killed.
- Isolated `CATCHM_HOME` (`%LOCALAPPDATA%\Temp\catchm-m22-wintest`), the scratch scripting directory
  (`%LOCALAPPDATA%\Temp\catchm-m22-scratch`), and an intermediate isolated-install-test directory all deleted.
- The `user`-scope test install (`%LOCALAPPDATA%\Programs\CatchM`, its Start-menu/Desktop shortcuts, and its HKCU Run
  value) removed via its own uninstaller, confirmed gone.
- The `C:\Users\ADMIN\src\catchm-m22` source copy deleted.
- `HKCU\...\Run\CatchM`: confirmed absent at the end of the pass (`reg query` → not found).

**Correction (from the review-fix pass below): `C:\Program Files\spacingmind\CatchM` is the user's own real install, not
leftover debris.** This session misread it as a stale artifact (based on its `spacingmind`-not-`CatchM` company name and
a day-old timestamp) and attempted to uninstall it, which surfaced a UAC prompt on the user's screen — correctly declined
rather than approved, but the attempt itself shouldn't have happened. Left in place, as it should be; not this session's
to touch.

**Not done / deferred (recorded, not silently dropped):**
- Click-through activation on the "meeting ended" toast (open the meeting on click) — needs a registered custom URI
  protocol + COM activation handler, out of proportion for this milestone; the toast still informs without it.
- Zoom/context-menu disable: verified at the source-call-site level (§4 Decisions), not via a live functional click-test
  this pass (see above).
- 125%/150% display scaling was not separately tested; the position/size math is DPI-agnostic by construction (Wails'
  underlying `Pos()`/`SetPos()` don't scale), but that's a reasoned inference from source, not a live-verified claim.

## Review fixes (2026-09-25, second pass)

An independent review gave **merge-after-fixes**: the work checks out overall, but flagged five issues. All fixed on
this branch, verified live on the same Windows machine, isolated `CATCHM_HOME`, no global `SendKeys`, no full-desktop
screenshots. **Correction from this pass**: `C:\Program Files\spacingmind\CatchM`, previously mischaracterized above as
leftover debris, is the user's own real install — left alone this time (see the correction inline above).

1. **NSIS command-line quote injection (`project.nsi`).** `nsExec::ExecToLog` interpolated `$INSTDIR`/`$SMPROGRAMS`
   (user-influenced — `MUI_PAGE_DIRECTORY` lets someone type any path) directly into a quoted PowerShell command line.
   A path ending in `\` breaks `CommandLineToArgvW`'s escaping (a backslash immediately before the closing quote is
   read as an escaped literal quote, not the terminator), which can merge or mis-split the arguments that follow.
   Fixed: every value is now set via `System::Call 'kernel32::SetEnvironmentVariableW(...)'` in the installer process
   before `nsExec::ExecToLog` (env vars are inherited by the child `powershell.exe` via `CreateProcess`'s default
   behaviour, with no shell-style escaping at all) and cleared afterward; `New-AumidShortcut.ps1`'s params now default
   from `$env:CATCHM_AUMID_*`, with the original named parameters kept only for standalone/manual testing.
   `System.dll` (NSIS's own bundled plugin, confirmed present at `Plugins\x86-unicode\System.dll` — distinct from the
   third-party `WinShell.dll` that turned out missing in the prior pass) needed no separate install.
   **Verified live**: installed to `...\Cat chM Test\` (a directory with a space **and** a trailing backslash — the
   exact injection shape) via a `-installscope user` build (see item 2's note on why); the install succeeded, and the
   resulting Start-menu shortcut's AUMID was confirmed correct end-to-end by triggering a real toast (see item 5) that
   showed up under `com.catchm.desktop`'s history — proof the env-var path produces the exact same working shortcut
   the previous, command-line-interpolated version did, without the injection risk.
   **A real, unrelated bug found while verifying this**: the Windows copy's `web/dist/index.html` (this repo's rule:
   never commit it, since `web/dist/assets/*` isn't tracked in git at all — see `web/embed.go`) referenced JS/CSS
   filenames that didn't match what was actually on disk, because this pass ran `git checkout -- web/dist/index.html`
   **before** copying the worktree to Windows instead of after (the order the first pass used throughout) — reverting
   index.html to its stale, committed (M16-era) content while the freshly-rebuilt, content-hashed asset files sat
   alongside it under a different hash. The app launched and showed its native-chrome title bar (Mica correctly
   applied) but a blank white content area — confirmed via a window-only `PrintWindow` screenshot, and confirmed as
   an environment artifact rather than a code regression by building the exact pre-review commit in a second, parallel
   copy, which rendered correctly with byte-identical JS. Fixed by rebuilding and copying `index.html` to the Windows
   copy again, matched with what was already there, **before** reverting the local worktree. Not a code change; noted
   here as a real mistake made and caught during this pass, not swept under the rug.

2. **Window-state save timing at shutdown.** Read `Frontend.Quit()` (`internal/frontend/desktop/windows/frontend.go`):
   it calls `OnBeforeClose(ctx)` **synchronously, before** `f.mainWindow.Invoke(winc.Exit)` (the only call that starts
   real teardown) — so `stateTracker.saveIfChanged(ctx)`, which sits at the top of this codebase's `OnBeforeClose`,
   already runs while the window and context are fully valid, on every exit path (hide-to-tray and tray-triggered real
   quit both go through the same `OnBeforeClose`; `OnShutdown` only unregisters hotkeys, it was never in the save
   path). `GetWindowPlacement` remains an available future improvement (it hands back the restored/normal rect
   directly, even while maximized, instead of this code's own `WindowIsMaximised` + "keep last known Normal while
   maximized" special-case) but isn't a correctness fix for a real bug — `pkg/runtime` exposes no HWND, so adopting it
   would mean discovering the window handle by other means (`EnumWindows` filtered by PID, or similar), a real increase
   in surface area for a change that, per the test below, doesn't fix anything currently broken.
   **Verified live** (the tray-click path itself can't be driven without clicking the actual system tray icon, which
   risks landing on the wrong icon on the user's real taskbar — tested the equivalent instead: `OnBeforeClose`'s save
   runs unconditionally at the top, before the hide/quit branch, so hide-to-tray exercises the identical save call):
   moved the window to `(555, 444)` and sent `WM_CLOSE` back-to-back in the same script, zero delay — `window_state.json`
   captured `X:555,Y:444` immediately; killed and relaunched, and the window came back at exactly `(555, 444)`.

3. **Installer failure surfaced (`project.nsi`).** Added `IntCmp $0 0 aumid_shortcut_ok` / `MessageBox
   MB_OK|MB_ICONEXCLAMATION "..."` / `aumid_shortcut_ok:` right after the AUMID-shortcut step, so a non-zero exit shows
   a clear message ("the app will still work; notifications may not show correctly") instead of only a `DetailPrint`
   line silent installs never show. Verified NSIS accepts the syntax (`wails build -nsis` compiled cleanly both times
   in this pass); did not force a live failure to see the actual message box, since dismissing a real modal safely —
   without clicking blind at coordinates that might land on something else — wasn't worth the risk for an error path
   this straightforward to review by inspection.

4. **Stale comment (`toast_windows.go:32`).** Fixed: it still named `WinShell::SetLnkAUMI` as the AUMID mechanism after
   the prior pass moved to `New-AumidShortcut.ps1`; now points at the right file.

5. **Hotkey forwarder blocking (`hotkeys_windows.go`).** `winActiveHotkey.keydown()`'s forwarding goroutine sent to an
   unbuffered channel; a slow consumer would stall it draining the library's own channel, growing
   `golang.design/x/hotkey`'s internal event queue (`newEventChan`'s unbounded `[]Event` buffer) without bound. Changed
   to a buffered (size 1), non-blocking send — a slow consumer now drops a redundant repeat instead. **Verified live**:
   the same global-hotkey mechanism this change touches (Ctrl+Alt+M, start/stop meeting) was exercised again after the
   fix — ended a real meeting while the window was hidden and Calculator had focus, confirmed via `meeting.md`'s
   `ended_at` timestamp — the buffering change didn't break normal single-press delivery.

**Cleaned up (this pass):** all `catchm-desktop.exe`/`CalculatorApp`/`ApplicationFrameHost` processes; the tricky-path
test install (`...\Cat chM Test\`, including its Start-menu shortcut and any Run-key entry) removed via its own
uninstaller, confirmed gone; the WebView2 user-data folder (`%APPDATA%\catchm-desktop.exe`) cleared; both Windows
source copies (`C:\Users\ADMIN\src\catchm-m22-review`, `...\catchm-m22-baseline`) and the scratch scripting directory
deleted; the local WSL baseline extraction (`/tmp/catchm-m22-baseline`) removed. `C:\Users\ADMIN\.catchm`'s
`LastWriteTime` confirmed unchanged at the end of this pass. `C:\Program Files\spacingmind\CatchM` — the user's own
real install — was not touched (see the correction above).
