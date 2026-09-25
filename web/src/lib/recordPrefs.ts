import type { AudioSources } from "@/lib/api"

/** Local-only recording prefs (non-secret UI defaults), per M20 spec. */
export type AudioSource = "mic" | "system" | "both"

const SOURCE_KEY = "catchm.prefs.recording.source"
const DEVICE_KEY = "catchm.prefs.recording.device"

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

export function readSourcePref(): AudioSource {
  const v = readStored(SOURCE_KEY)
  return v === "system" || v === "both" ? v : "mic"
}

export function writeSourcePref(source: AudioSource) {
  writeStored(SOURCE_KEY, source)
}

export function readDevicePref(): string {
  return readStored(DEVICE_KEY) ?? ""
}

export function writeDevicePref(device: string) {
  writeStored(DEVICE_KEY, device)
}

/**
 * What the start flow actually uses: a saved system/both choice falls
 * back to mic when system audio isn't available (M19 backend missing or
 * unsupported). The stored choice stays untouched so it revives when the
 * backend catches up.
 */
export function effectiveSource(saved: AudioSource, sources: AudioSources | null): AudioSource {
  if (saved === "mic") return "mic"
  return sources?.system?.supported === true ? saved : "mic"
}
