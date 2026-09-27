/**
 * Thin, typed wrapper over the Wails v2.16 injected `window.runtime`
 * (M21 §4) and `window.go` (M22 §2/§3's DesktopBridge bind). Verified
 * against the actual module source (not just docs), since research on
 * this project has been wrong about Wails before:
 * `github.com/wailsapp/wails/v2@v2.16.0`'s
 * `internal/frontend/runtime/desktop/window.js` — every export there is
 * spread onto `window.runtime` by `desktop/main.js`. Signatures below
 * match that file exactly:
 *   WindowGetSize()            -> Promise<{w: number, h: number}>
 *   WindowGetPosition()        -> Promise<{x: number, y: number}>
 *   WindowSetSize(w, h)        -> void
 *   WindowSetMinSize(w, h)     -> void
 *   WindowSetPosition(x, y)    -> void
 *   WindowSetAlwaysOnTop(bool) -> void
 *   WindowShow()               -> void
 * `window.go.main.DesktopBridge.SetCompactMode` is populated at
 * runtime by `internal/frontend/runtime/desktop/bindings.js`'s
 * `SetBindings`, keyed `[package].[struct].[method]` — verified against
 * that file and `internal/app/app_production.go`'s
 * `binding.NewBindings(..., appoptions.Bind, ...)`, which registers it
 * via reflection regardless of the CLI's `-skipbindings` flag (that
 * only skips writing the TS/JS wrapper files this project doesn't use).
 *
 * `catchm serve` in a plain browser never injects `window.runtime` or
 * `window.go` at all, so every call here is guarded by
 * hasWindowControls() — the feature-detection the spec asks for, not a
 * build-time flag.
 */

interface WailsWindowRuntime {
  WindowGetSize(): Promise<{ w: number; h: number }>
  WindowGetPosition(): Promise<{ x: number; y: number }>
  WindowSetSize(width: number, height: number): void
  WindowSetMinSize(width: number, height: number): void
  WindowSetPosition(x: number, y: number): void
  WindowSetAlwaysOnTop(b: boolean): void
  WindowShow(): void
  BrowserOpenURL(url: string): void
  WindowSetSystemDefaultTheme(): void
  WindowSetLightTheme(): void
  WindowSetDarkTheme(): void
  WindowSetBackgroundColour(r: number, g: number, b: number, a: number): void
  EventsOn(eventName: string, callback: (...data: unknown[]) => void): () => void
}

/** Matches cmd/catchm-desktop/desktop_settings.go's ShortcutsInfo. */
export interface ShortcutsInfo {
  bindings: Record<string, string>
  errors: Record<string, string>
}

declare global {
  interface Window {
    runtime?: Partial<WailsWindowRuntime>
    go?: {
      main?: {
        DesktopBridge?: {
          SetCompactMode?(active: boolean): void
          GetShortcuts?(): Promise<ShortcutsInfo>
          SetShortcuts?(bindings: Record<string, string>): Promise<ShortcutsInfo>
          GetAutostart?(): Promise<boolean>
          SetAutostart?(enabled: boolean): Promise<void>
          GetNotificationsEnabled?(): Promise<boolean>
          SetNotificationsEnabled?(enabled: boolean): Promise<void>
          NotifyCloudSyncConflict?(count: number): void
          ReadyForEvents?(): void
          GetAppVersion?(): Promise<string>
          CheckForUpdate?(): Promise<UpdateStatus>
          GetLastChecked?(): Promise<string>
          DownloadUpdate?(): Promise<DownloadResult>
          CancelDownload?(): Promise<void>
          ApplyUpdate?(): Promise<void>
          DeferUpdate?(): void
        }
      }
    }
  }
}

/** True only inside the Wails desktop webview; false in every browser
 *  (catchm serve). Check this, not a build target, before touching any
 *  window control below. */
export function hasWindowControls(): boolean {
  return typeof window.runtime?.WindowSetAlwaysOnTop === "function"
}

export function windowGetSize(): Promise<{ w: number; h: number }> {
  return window.runtime!.WindowGetSize!()
}

export function windowGetPosition(): Promise<{ x: number; y: number }> {
  return window.runtime!.WindowGetPosition!()
}

export function windowSetSize(width: number, height: number): void {
  window.runtime!.WindowSetSize!(width, height)
}

export function windowSetMinSize(width: number, height: number): void {
  window.runtime!.WindowSetMinSize!(width, height)
}

export function windowSetPosition(x: number, y: number): void {
  window.runtime!.WindowSetPosition!(x, y)
}

/**
 * Places the window at the exact absolute (x, y) — the coordinates
 * WindowGetPosition reports — regardless of which monitor
 * WindowSetPosition treats as "current".
 *
 * M22 §2 root-cause fix for M21's 24px compact-exit drift: verified
 * against `internal/frontend/desktop/windows/winc/controlbase.go` in
 * the v2.16.0 module source, `WindowSetPosition`'s (x, y) is NOT
 * absolute — `SetPos` resolves it as an offset from the CURRENT
 * monitor's work-area origin (`getMonitorInfo(hwnd).RcWork`), while
 * `WindowGetPosition`'s `Pos()` returns raw absolute screen
 * coordinates. `windowSetPosition(x, y)` after a prior
 * `windowGetPosition()` therefore only round-trips correctly when that
 * monitor's work area happens to start at absolute (0, 0) — false
 * whenever a taskbar/reserved edge offsets it, which is exactly what
 * produced the drift.
 *
 * Fix: set (0, 0) first, landing the window exactly at that monitor's
 * work-area origin in the SAME absolute space WindowGetPosition
 * reports; reading it back gives the exact offset to cancel out. The
 * second SetPosition call resolves against the same "current" monitor
 * as the first (the window hasn't moved in between), so the arithmetic
 * is exact regardless of monitor topology — no monitor enumeration
 * needed (unlike the Go-side restore-on-launch clamping, which does
 * need real monitor positions Wails' ScreenGetAll doesn't expose).
 *
 * The short delay before reading it back is load-bearing, found on
 * real Windows: WindowSetPosition goes through `window.WailsInvoke`, a
 * fire-and-forget native bridge call, while WindowGetPosition goes
 * through the async `Call()` RPC channel (both verified in the actual
 * v2.16.0 desktop/window.js) — two independent transports with no
 * ordering guarantee between them. Without the delay, GetPosition can
 * — and, tested live, reliably did — return the PRE-move position,
 * making the "exact offset" computed below zero and this function a
 * no-op that leaves the window stranded at (0, 0)'s absolute location
 * instead of the target.
 */
export async function windowRestoreAbsolutePosition(x: number, y: number): Promise<void> {
  windowSetPosition(0, 0)
  await new Promise((resolve) => setTimeout(resolve, 50))
  const origin = await windowGetPosition()
  windowSetPosition(x - origin.x, y - origin.y)
}

export function windowSetAlwaysOnTop(b: boolean): void {
  window.runtime!.WindowSetAlwaysOnTop!(b)
}

export function windowShow(): void {
  window.runtime!.WindowShow!()
}

/**
 * Opens url in the system browser and returns true inside the Wails
 * window; returns false elsewhere so the caller lets the <a> navigate
 * normally. Wails v2.16 never handles WebView2's NewWindowRequested, so
 * target=_blank / window.open there opens a bare in-app popup instead.
 */
export function browserOpenURL(url: string): boolean {
  const open = window.runtime?.BrowserOpenURL
  if (typeof open !== "function") return false
  open(url)
  return true
}

/**
 * Subscribes to a Wails backend event (e.g. the tray's "Compact mode"
 * item emitting "tray:toggle-compact" — cmd/catchm-desktop/tray.go's
 * trayToggleCompactEvent const, which MUST match this string exactly).
 * Returns the cancel function EventsOn itself returns, verified against
 * internal/frontend/runtime/desktop/events.js. A no-op (returns a no-op
 * canceller) outside the Wails window.
 */
export function windowEventsOn(eventName: string, callback: (...data: unknown[]) => void): () => void {
  if (!hasWindowControls()) return () => {}
  return window.runtime!.EventsOn!(eventName, callback)
}

/**
 * Tells the Go side compact mode is on/off (DesktopBridge.SetCompactMode,
 * M22 §2/§3) — the only signal it has, since compact's resize/reposition
 * happens entirely in the frontend via the calls above. The Go-side
 * window-bounds poller uses this to never persist compact's 480x260
 * geometry as if it were the normal window's bounds. A no-op outside the
 * Wails window (hasWindowControls() false) or on an older build with no
 * DesktopBridge bound.
 */
export function notifyCompactMode(active: boolean): void {
  window.go?.main?.DesktopBridge?.SetCompactMode?.(active)
}

/** M22 §5: current global-shortcut bindings + any registration error
 *  per action (an already-taken combo, invalid, or unsupported on this
 *  OS) — surfaced inline, never silently dropped. */
export function getShortcuts(): Promise<ShortcutsInfo> {
  return window.go!.main!.DesktopBridge!.GetShortcuts!()
}

/** Replaces the bindings and re-registers every hotkey; same shape as
 *  getShortcuts() for the resulting (possibly partial) success. */
export function setShortcuts(bindings: Record<string, string>): Promise<ShortcutsInfo> {
  return window.go!.main!.DesktopBridge!.SetShortcuts!(bindings)
}

/** M22 §5 launch-at-login (HKCU Run key, opt-in). */
export function getAutostart(): Promise<boolean> {
  return window.go!.main!.DesktopBridge!.GetAutostart!()
}

export function setAutostart(enabled: boolean): Promise<void> {
  return window.go!.main!.DesktopBridge!.SetAutostart!(enabled)
}

/** M22 §5 toast notifications opt-out. */
export function getNotificationsEnabled(): Promise<boolean> {
  return window.go!.main!.DesktopBridge!.GetNotificationsEnabled!()
}

export function setNotificationsEnabled(enabled: boolean): Promise<void> {
  return window.go!.main!.DesktopBridge!.SetNotificationsEnabled!(enabled)
}

/** Fires a toast for the cloud-sync-conflict case — called right after
 *  a manual sync's response shows conflicted meetings (there's no
 *  background sync loop in this app to poll for this the way tray.go
 *  polls capture state). A no-op outside the Wails window. */
export function notifyCloudSyncConflict(count: number): void {
  window.go?.main?.DesktopBridge?.NotifyCloudSyncConflict?.(count)
}

/**
 * The frontend's half of the cold-start protocol-activation handshake
 * (cmd/catchm-desktop/activate.go's handleLaunchURI): call once the
 * app:open-meeting subscription is in place, so a catchm://meeting/<id>
 * toast click that launched the whole app isn't emitted into the void
 * before this page ran any JS. A no-op outside the Wails window.
 */
export function notifyReadyForEvents(): void {
  window.go?.main?.DesktopBridge?.ReadyForEvents?.()
}

/** Matches cmd/catchm-desktop/update.go's UpdateStatus. */
export interface UpdateStatus {
  currentVersion: string
  publishedAt?: string
  updateAvailable: boolean
  latestVersion?: string
  notes?: string
  notesURL?: string
  checkedAt?: string
  error?: string
}

/** Matches cmd/catchm-desktop/update.go's DownloadResult. */
export interface DownloadResult {
  sha256: string
  /** CancelDownload stopped it: nothing was kept, and it isn't an error. */
  cancelled?: boolean
}

/**
 * M23: the running app version ("dev" for local builds). Every Wails
 * bound-method call crosses the JS→Go bridge and therefore resolves a
 * Promise, regardless of the Go signature — an earlier version of this
 * wrapper returned that Promise while claiming `string`, which
 * string-interpolated as "[object Promise]" wherever it was rendered.
 * Await it (or `.then(setVersion)`), never render it directly.
 */
export function getAppVersion(): Promise<string> {
  return window.go?.main?.DesktopBridge?.GetAppVersion?.() ?? Promise.resolve("dev")
}

/** M23: manual update check — same path the background check uses. */
export function checkForUpdate(): Promise<UpdateStatus> {
  return window.go!.main!.DesktopBridge!.CheckForUpdate!()
}

/** M23: when the last check (any kind) ran, "" if never. */
export function getLastChecked(): Promise<string> {
  return window.go!.main!.DesktopBridge!.GetLastChecked!()
}

/** M23: download + SHA-256-verify the installer for the latest release. */
export function downloadUpdate(): Promise<DownloadResult> {
  return window.go!.main!.DesktopBridge!.DownloadUpdate!()
}

/** Stop an in-flight downloadUpdate, which then resolves with
 *  `cancelled`. Resolves once the partial file is gone, so a retry can
 *  follow at once; a no-op when nothing is downloading. */
export function cancelDownload(): Promise<void> {
  return window.go?.main?.DesktopBridge?.CancelDownload?.() ?? Promise.resolve()
}

/** M23: run the verified installer elevated and quit. */
export function applyUpdate(): Promise<void> {
  return window.go!.main!.DesktopBridge!.ApplyUpdate!()
}

/** M23: stop prompting for this session. */
export function deferUpdate(): void {
  window.go?.main?.DesktopBridge?.DeferUpdate?.()
}
