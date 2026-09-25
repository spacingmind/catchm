# Plan: M22 follow-ups — capture error kinds, toast activation, DPI check

- Status: complete
- Created: 2026-09-25
- Completed: 2026-09-25
- Repo: `catchm`: three independent follow-ups from the M22 review,
  committed separately on `polish-followups`.

## 1. Structured capture error codes

The frontend's "Open settings" button on the capture-error banner picked
the settings page by pattern-matching the error message text
(`message.includes("microphone")`), which breaks whenever wording
changes and couples the UI to OS/driver error strings.

- `internal/server`: new `CaptureErrorKind` enum
  (`missing_api_key`, `device_unavailable`, `unsupported`, `engine`,
  `internal`) set at every failure site in `startPipeline` /
  `switchAudioSource`. `POST /api/meetings` adds `capture_error_kind`
  next to the existing `capture_error` (absent on success, same as the
  message field); the 422 from `POST /api/meetings/{id}/source` carries
  it too. Additive only — no existing field renamed, so other consumers
  of the JSON contract are unaffected.
- `web/src/lib/api.ts` mirrors the enum;
  `App.tsx`'s `captureErrorSection` switches on the kind, never the
  message. The message stays the display text.
- Tests assert the kind for a mic-open failure (`device_unavailable`)
  and Soniox-without-key (`missing_api_key`), and that neither key
  appears on success.

## 2. Toast click-to-open meeting

M22 shipped toasts but clicking one did nothing (explicitly deferred in
its Decisions). Now:

- `wails.json` `info.protocols` registers the `catchm` scheme; the NSIS
  installer's existing `wails.associateCustomProtocols` /
  `unassociateCustomProtocols` macros (already inserted in
  `project.nsi`) write/remove `HKCU\Software\Classes\catchm` pointing at
  the exe with `%1` arg passing — no NSIS script change needed.
- `toast_windows.go`: `notify` gains a `meetingID` param; the
  "Meeting ended" toast passes the ended meeting's id, which becomes
  `ActivationArguments` (`catchm://meeting/<id>`) with
  `activationType="protocol"` (read from go-toast's template — the
  `launch` attribute is what Windows activates on click).
- `cmd/catchm-desktop/activate.go`: strict `parseMeetingURI`
  (`/meeting/<hex-id>` only — deliberately prefix-based, since
  `net/url` puts `meeting` in Host for a non-special scheme) and
  `handleLaunchURI`. Cold start: `os.Args` captured before `wails.Run`,
  emitted after `OnStartup`; EventsEmit before the webview booted would
  be lost, so the frontend announces readiness via a new
  `DesktopBridge.ReadyForEvents` bind and `handleLaunchURI` polls for it
  (15s timeout — losing a toast click is recoverable, hanging forever is
  not). Already-running: `OnSecondInstanceLaunch` parses
  `SecondInstanceData.Args` (the lock forwards the second exe's args via
  WM_COPYDATA), emits `app:open-meeting`, and shows the window.
- `App.tsx` subscribes to `app:open-meeting`, fetches the meeting fresh
  (a toast can outlive the meeting; `ended_at` picks live vs detail),
  then navigates. Subscription precedes the readiness call so a held
  cold-start URI always has a listener.

Unit-tested: URI parsing (valid/invalid ids, other schemes, traversal).
Not live-click-tested on Windows this pass (the install path needs an
NSIS build + a real toast click; the pieces — protocol registry value
shape, go-toast XML, Wails arg forwarding — are each source-verified).

## 3. DPI scaling check at 125% and 150%

Real Windows verification (same host/pattern as M19–M22), display
switched between scales via the undocumented
`DisplayConfigSetDeviceInfo` DPI-scale device-info type, **always
restored in a `finally`** after an earlier mishap left the desktop
scaled mid-run (restored immediately once caught; later runs wrapped
set→capture→restore so a failure can't leave the scale changed).

Build: `wails build -s -skipbindings` on the Windows copy of this
branch; isolated `CATCHM_HOME`; app never opened the mic.

- **125%** (dpi 120): window opens at 1475x950 physical = 1180x760
  logical × 1.25 exactly, fully on-screen, `GetDpiForWindow`=120,
  DWMWA_SYSTEMBACKDROP_TYPE=2 (Mica active). Window screenshot (BitBlt;
  PrintWindow renders this GPU-composited webview black): text crisp,
  no blur; sidebar complete (Meetings/Live/Action items/Start
  meeting/Settings); empty-state box fully inside frame; title bar
  present and proportionate; nothing clipped or overlapping.
- **150%** (dpi 144): 1770x1140 physical = 1180x760 × 1.5 exactly,
  fully on-screen, dpi 144, same clean rendering verdict.
- **Compact overlay (480x260)**: source-verified rather than
  live-clicked (needs a live meeting): Wails'
  `ControlBase.SetSize` runs its args through `scaleWithWindowDPI`
  (internal/frontend/desktop/windows/winc/controlbase.go), so
  `useCompactMode`'s fixed 480x260 lands as exactly 480x260 *logical*
  at any scale — same arithmetic as the verified main-window numbers.
- **Settings/live-view pixel checks**: lighter-weight this pass. The
  main list view (the same sidebar + `main` shell the other views
  render inside) was screenshot-verified at both scales; an earlier
  UIA-driven walk of Settings didn't find elements from the child
  console and was cut per scope, so Settings pages are covered by the
  shared-shell check above rather than per-page screenshots. No code
  change was needed: nothing blurry, clipped, or mis-sized was found in
  anything that was checked.

Bugs found/fixed: none in the app. Two tooling lessons recorded for the
next pass: `PrintWindow` returns black for this webview at non-native
DPI (use BitBlt), and a DPI-unaware PowerShell console reports
virtualized window rects (call `SetProcessDpiAwarenessContext` first).

## Validation

- `go test ./...` green except pre-existing `internal/agent` failures
  (need a logged-in claude-agent-sdk session; verified failing on a
  clean tree before any change).
- `cd web && bun run build` (tsc + vite) and `bunx oxlint` clean — the
  same 7 pre-existing warnings, 0 errors, with and without the changes.
- Real Windows: items in §3 above; scale restored to native
  (`cur=0`, primary bounds 2560x1440) and all test artifacts removed
  (`catchm-dpi` source copy, `dpi-shots`, DpiTool/scripts, throwaway
  `CATCHM_HOME`; no catchm processes left).
