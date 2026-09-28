import { useCallback, useEffect, useRef, useState } from "react"

import {
  hasWindowControls,
  notifyCompactMode,
  windowEventsOn,
  windowGetPosition,
  windowGetSize,
  windowRestoreAbsolutePosition,
  windowSetAlwaysOnTop,
  windowSetMinSize,
  windowSetSize,
} from "@/lib/wailsRuntime"

// Must match cmd/catchm-desktop/tray.go's trayToggleCompactEvent const
// exactly — the tray's "Compact mode" menu item emits this.
const TRAY_TOGGLE_COMPACT_EVENT = "tray:toggle-compact"

const COMPACT_WIDTH = 480
const COMPACT_HEIGHT = 260

interface SavedWindowState {
  w: number
  h: number
  x: number
  y: number
}

/**
 * Compact always-on-top reading mode (M21 §4), desktop-only. `available`
 * is `hasWindowControls()` — a plain browser (catchm serve) never gets a
 * toggle button or the keyboard shortcut wired up at all, not just a
 * disabled one.
 */
export function useCompactMode() {
  const [compact, setCompact] = useState(false)
  const savedRef = useRef<SavedWindowState | null>(null)
  // Whether the window should be always-on-top right now. A ref, updated
  // synchronously: exit() clears it before its awaited resize/move, whose
  // webview focus events would otherwise re-assert always-on-top (compact
  // stays true until exit() finishes) and leave the full window pinned.
  const pinnedRef = useRef(false)
  const available = hasWindowControls()

  const enter = useCallback(async () => {
    if (!available || compact) return
    const [size, pos] = await Promise.all([windowGetSize(), windowGetPosition()])
    savedRef.current = { w: size.w, h: size.h, x: pos.x, y: pos.y }
    windowSetMinSize(0, 0)
    windowSetSize(COMPACT_WIDTH, COMPACT_HEIGHT)
    pinnedRef.current = true
    windowSetAlwaysOnTop(true)
    notifyCompactMode(true)
    setCompact(true)
  }, [available, compact])

  const exit = useCallback(async () => {
    if (!available || !compact) return
    pinnedRef.current = false
    windowSetAlwaysOnTop(false)
    const saved = savedRef.current
    if (saved) {
      windowSetSize(saved.w, saved.h)
      // windowRestoreAbsolutePosition, not a raw windowSetPosition(x, y)
      // round-trip of what windowGetPosition returned in enter() above
      // — that was M21's exact bug (a 24px drift on this exact call),
      // root-caused and fixed in wailsRuntime.ts. MUST be awaited here:
      // real Windows testing caught that firing it detached (the
      // previous `void windowRestoreAbsolutePosition(...)`) let the
      // calls below run before its internal SetPosition(0,0) ->
      // GetPosition -> corrective-SetPosition chain finished, leaving
      // the window stranded at the calibration midpoint (the monitor's
      // work-area origin) instead of the target position — a new,
      // 100%-reproducible drift, worse than the one this was fixing.
      await windowRestoreAbsolutePosition(saved.x, saved.y)
    }
    // The app (cmd/catchm-desktop) never sets its own min-size, so (0,0)
    // is the correct restored value, not a placeholder.
    windowSetMinSize(0, 0)
    notifyCompactMode(false)
    setCompact(false)
  }, [available, compact])

  const toggle = useCallback(() => {
    if (compact) void exit()
    else void enter()
  }, [compact, enter, exit])

  // Windows can drop always-on-top after the window is minimized and
  // restored. window.runtime has no dedicated event for that (checked
  // against the module source — see lib/wailsRuntime.ts), so re-assert
  // on the closest portable signals available: the webview regaining
  // OS focus, or the page becoming visible again.
  useEffect(() => {
    if (!compact) return
    const reassert = () => {
      if (pinnedRef.current) windowSetAlwaysOnTop(true)
    }
    const onVisibility = () => {
      if (document.visibilityState === "visible") reassert()
    }
    window.addEventListener("focus", reassert)
    document.addEventListener("visibilitychange", onVisibility)
    return () => {
      window.removeEventListener("focus", reassert)
      document.removeEventListener("visibilitychange", onVisibility)
    }
  }, [compact])

  // Ctrl/⌘+Shift+L toggles compact mode while the Live view is mounted
  // (a focused-window shortcut; system-wide hotkeys are M22).
  useEffect(() => {
    if (!available) return
    const onKeyDown = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || !e.shiftKey || e.altKey) return
      if (e.key.toLowerCase() !== "l") return
      e.preventDefault()
      toggle()
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [available, toggle])

  // M22 §3: the tray's "Compact mode" menu item (no direct Go->JS call
  // surface other than Wails events — DesktopBridge, M22 §2, only goes
  // JS->Go).
  useEffect(() => {
    if (!available) return
    return windowEventsOn(TRAY_TOGGLE_COMPACT_EVENT, toggle)
  }, [available, toggle])

  return { compact, available, toggle }
}
