/**
 * Thin, typed wrapper over the Wails v2.16 injected `window.runtime`
 * (M21 §4). Verified against the actual module source (not just docs),
 * since research on this project has been wrong about Wails before:
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
 *
 * `catchm serve` in a plain browser never injects `window.runtime` at
 * all, so every call here is guarded by hasWindowControls() — the
 * feature-detection the spec asks for, not a build-time flag.
 */

interface WailsWindowRuntime {
  WindowGetSize(): Promise<{ w: number; h: number }>
  WindowGetPosition(): Promise<{ x: number; y: number }>
  WindowSetSize(width: number, height: number): void
  WindowSetMinSize(width: number, height: number): void
  WindowSetPosition(x: number, y: number): void
  WindowSetAlwaysOnTop(b: boolean): void
  WindowShow(): void
}

declare global {
  interface Window {
    runtime?: Partial<WailsWindowRuntime>
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

export function windowSetAlwaysOnTop(b: boolean): void {
  window.runtime!.WindowSetAlwaysOnTop!(b)
}

export function windowShow(): void {
  window.runtime!.WindowShow!()
}
