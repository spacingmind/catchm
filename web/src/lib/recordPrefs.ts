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

const STARTED_DEVICE_KEY = "catchm.prefs.recording.startedDevice"

/**
 * The mic device the start flow asked for, remembered per meeting so the
 * Live view's mic select starts from what's actually recording — the
 * meeting doc doesn't store the device. Only the latest start is kept
 * (one meeting is live at a time); "" means the default device.
 */
export function writeStartedDevice(meetingId: string, device: string) {
  writeStored(STARTED_DEVICE_KEY, JSON.stringify({ meetingId, device }))
}

/** The device recorded by writeStartedDevice for this meeting, else ""
 *  (default device — also what a tray/shortcut start uses). */
export function readStartedDevice(meetingId: string): string {
  try {
    const v = JSON.parse(readStored(STARTED_DEVICE_KEY) ?? "null") as { meetingId?: unknown; device?: unknown } | null
    return v?.meetingId === meetingId && typeof v.device === "string" ? v.device : ""
  } catch {
    return ""
  }
}

/** Radix Select can't have an item with value "", so the "Default
 *  device" item uses this sentinel; map it to "" (no device) on change. */
export const DEFAULT_DEVICE_VALUE = "__default__"

/** The Select value for a stored device: the sentinel when it's the
 *  default or no longer in the list (unplugged), so the trigger never
 *  shows blank. */
export function deviceSelectValue(device: string, devices: { ID: string }[]): string {
  return device && devices.some((d) => d.ID === device) ? device : DEFAULT_DEVICE_VALUE
}

const TRANSPARENT_KEY = "catchm.compact.transparent"

/** See-through compact overlay pref, per device like the theme
 *  (docs/plans/active/compact-transparent.md). */
export function readCompactTransparent(): boolean {
  return readStored(TRANSPARENT_KEY) === "true"
}

export function writeCompactTransparent(on: boolean) {
  writeStored(TRANSPARENT_KEY, on ? "true" : "false")
}
