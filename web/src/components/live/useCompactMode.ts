import { useCallback, useEffect, useRef, useState } from "react"

import {
  hasWindowControls,
  windowGetPosition,
  windowGetSize,
  windowSetAlwaysOnTop,
  windowSetMinSize,
  windowSetPosition,
  windowSetSize,
} from "@/lib/wailsRuntime"

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
  const available = hasWindowControls()

  const enter = useCallback(async () => {
    if (!available || compact) return
    const [size, pos] = await Promise.all([windowGetSize(), windowGetPosition()])
    savedRef.current = { w: size.w, h: size.h, x: pos.x, y: pos.y }
    windowSetMinSize(0, 0)
    windowSetSize(COMPACT_WIDTH, COMPACT_HEIGHT)
    windowSetAlwaysOnTop(true)
    setCompact(true)
  }, [available, compact])

  const exit = useCallback(() => {
    if (!available || !compact) return
    windowSetAlwaysOnTop(false)
    const saved = savedRef.current
    if (saved) {
      windowSetSize(saved.w, saved.h)
      windowSetPosition(saved.x, saved.y)
    }
    // The app (cmd/catchm-desktop) never sets its own min-size, so (0,0)
    // is the correct restored value, not a placeholder.
    windowSetMinSize(0, 0)
    setCompact(false)
  }, [available, compact])

  const toggle = useCallback(() => {
    if (compact) exit()
    else void enter()
  }, [compact, enter, exit])

  // Windows can drop always-on-top after the window is minimized and
  // restored. window.runtime has no dedicated event for that (checked
  // against the module source — see lib/wailsRuntime.ts), so re-assert
  // on the closest portable signals available: the webview regaining
  // OS focus, or the page becoming visible again.
  useEffect(() => {
    if (!compact) return
    const reassert = () => windowSetAlwaysOnTop(true)
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

  return { compact, available, toggle }
}
