import type { SttEngine } from "@/lib/api"

/** Last-used start-flow values (non-secret UI prefs only): what the
 *  Start meeting popover opens with. First-run onboarding seeds the
 *  languages; every start rewrites them. */
const START_PREFS_KEY = "catchm.prefs.startflow"

export interface StartPrefs {
  /** Meeting language; "" = detect automatically. */
  lang?: string
  /** Translate into; "" = no translation. */
  translateTo?: string
  /** lang and translateTo are the two conversation languages. */
  twoWay?: boolean
  engine?: SttEngine
}

export function readStartPrefs(): StartPrefs {
  try {
    const raw = window.localStorage.getItem(START_PREFS_KEY)
    return raw ? (JSON.parse(raw) as StartPrefs) : {}
  } catch {
    return {}
  }
}

export function writeStartPrefs(p: StartPrefs) {
  try {
    window.localStorage.setItem(START_PREFS_KEY, JSON.stringify(p))
  } catch {
    // Storage unavailable — prefs just won't persist.
  }
}
