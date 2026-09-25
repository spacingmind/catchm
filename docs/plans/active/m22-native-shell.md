# Plan: M22 — Native shell polish (Windows desktop)

- Status: active
- Created: 2026-09-25
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
- (fill in the rest)

## Progress

- [ ] §0 deterministic mixer tests
- [ ] §1 Mica + chrome + wails.json info
- [ ] §2 window state + drift fix
- [ ] §3 single instance + tray states
- [ ] §4 WebView hardening
- [ ] §5 hotkeys, launch at login, toasts (+ NSIS AUMID shortcut)
- [ ] §6 compact translation styling
- [ ] Real Windows verification

## Validation

(to be filled)
