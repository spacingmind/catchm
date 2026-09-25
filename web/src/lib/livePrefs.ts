/** Local-only Live view prefs (non-secret UI state), per M21 §3. */

export type TranscriptViewMode = "stacked" | "side-by-side" | "translation-only"

/** Body text sizes (px) the A-/A+ control steps through, from the
 *  spec's 15-17px baseline up to a size meant for reading at a
 *  distance. */
export const FONT_SIZES = [15, 16, 17, 18, 20, 22, 24, 28] as const
const DEFAULT_FONT_SIZE = 16

const VIEW_MODE_KEY = "catchm.prefs.live.viewMode"
const FONT_SIZE_KEY = "catchm.prefs.live.fontSize"

function readStored(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

function writeStored(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // Storage unavailable — the pref just won't persist.
  }
}

export function readViewMode(): TranscriptViewMode {
  const v = readStored(VIEW_MODE_KEY)
  return v === "side-by-side" || v === "translation-only" ? v : "stacked"
}

export function writeViewMode(mode: TranscriptViewMode) {
  writeStored(VIEW_MODE_KEY, mode)
}

export function readFontSize(): number {
  const v = Number(readStored(FONT_SIZE_KEY))
  return (FONT_SIZES as readonly number[]).includes(v) ? v : DEFAULT_FONT_SIZE
}

export function writeFontSize(px: number) {
  writeStored(FONT_SIZE_KEY, String(px))
}
