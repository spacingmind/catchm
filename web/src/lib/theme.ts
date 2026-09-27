/**
 * Light / Dark / System theme, stored per device in localStorage
 * (docs/plans/active/web-ui-followups.md, Decisions). The resolved
 * theme is the `.dark` class on <html>, which drives index.css's dark
 * tokens and Tailwind's dark: variant. index.html's inline script
 * applies the class before first paint; this module keeps it, the
 * theme-color meta and the Wails window in step afterwards.
 */
import { useSyncExternalStore } from "react"

export type ThemePref = "system" | "light" | "dark"
export type ResolvedTheme = "light" | "dark"

/** Also read by index.html's pre-paint script — keep them in sync. */
export const THEME_STORAGE_KEY = "catchm.theme"

const DARK_QUERY = "(prefers-color-scheme: dark)"

/** Each theme's --background (index.css), for theme-color and the
 *  Wails webview background. */
const THEME_BACKGROUND: Record<ResolvedTheme, string> = {
  light: "#FBF7EA",
  dark: "#1E1411",
}

function isThemePref(v: unknown): v is ThemePref {
  return v === "system" || v === "light" || v === "dark"
}

/** The stored preference; "system" when unset, invalid or storage is
 *  unavailable (private mode, blocked cookies). */
export function getThemePref(): ThemePref {
  try {
    const v = localStorage.getItem(THEME_STORAGE_KEY)
    return isThemePref(v) ? v : "system"
  } catch {
    return "system"
  }
}

function systemPrefersDark(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia(DARK_QUERY).matches
}

function resolve(pref: ThemePref): ResolvedTheme {
  if (pref === "system") return systemPrefersDark() ? "dark" : "light"
  return pref
}

/** Title bar + webview background follow the app theme in the desktop
 *  shell; a no-op in a plain browser (no window.runtime). */
function applyToWailsWindow(pref: ThemePref, resolved: ResolvedTheme): void {
  const rt = window.runtime
  if (!rt) return
  // "system" hands the title bar back to Wails, which then tracks OS
  // changes itself (WM_SETTINGCHANGE); explicit prefs pin it.
  if (pref === "system") rt.WindowSetSystemDefaultTheme?.()
  else if (pref === "dark") rt.WindowSetDarkTheme?.()
  else rt.WindowSetLightTheme?.()
  // What html.desktop-mica's transparent body actually shows (index.css).
  const hex = THEME_BACKGROUND[resolved]
  rt.WindowSetBackgroundColour?.(
    parseInt(hex.slice(1, 3), 16),
    parseInt(hex.slice(3, 5), 16),
    parseInt(hex.slice(5, 7), 16),
    255,
  )
}

function applyTheme(pref: ThemePref, resolved: ResolvedTheme): void {
  document.documentElement.classList.toggle("dark", resolved === "dark")
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", THEME_BACKGROUND[resolved])
  applyToWailsWindow(pref, resolved)
}

export interface ThemeState {
  pref: ThemePref
  resolved: ResolvedTheme
}

// One immutable snapshot, replaced only on change, so
// useSyncExternalStore sees a stable reference between updates.
let state: ThemeState | null = null
const listeners = new Set<() => void>()

function snapshot(): ThemeState {
  if (!state) {
    const pref = getThemePref()
    state = { pref, resolved: resolve(pref) }
  }
  return state
}

function update(pref: ThemePref): void {
  const resolved = resolve(pref)
  const prev = snapshot()
  applyTheme(pref, resolved)
  if (prev.pref === pref && prev.resolved === resolved) return
  state = { pref, resolved }
  for (const l of listeners) l()
}

/** Save the preference on this device and apply it now. */
export function setThemePref(pref: ThemePref): void {
  try {
    localStorage.setItem(THEME_STORAGE_KEY, pref)
  } catch {
    // Storage blocked: still apply for this session.
  }
  update(pref)
}

let initialized = false

/**
 * Apply the stored theme and start following OS changes (while the
 * pref is "system") and other tabs' changes (storage event). Safe to
 * call more than once; main.tsx calls it before the first render.
 */
export function initTheme(): void {
  if (initialized) return
  initialized = true
  update(getThemePref())
  if (typeof window.matchMedia === "function") {
    window.matchMedia(DARK_QUERY).addEventListener("change", () => {
      if (snapshot().pref === "system") update("system")
    })
  }
  window.addEventListener("storage", (e) => {
    // key is null when another tab cleared all storage.
    if (e.key === THEME_STORAGE_KEY || e.key === null) update(getThemePref())
  })
}

function subscribe(listener: () => void): () => void {
  initTheme()
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** Current preference, the theme it resolves to, and a setter. */
export function useTheme(): ThemeState & { setPref: (pref: ThemePref) => void } {
  const s = useSyncExternalStore(subscribe, snapshot)
  return { ...s, setPref: setThemePref }
}
