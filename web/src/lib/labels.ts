/**
 * User-facing names for audio sources and transcription engines, shared
 * by every screen so a non-technical user sees one name per concept
 * (the start popover, live status row, compact overlay and Settings all
 * read from here). Wire values stay "mic" / "system" / "both" and
 * "whisper" / "soniox"; the names live in the i18n dictionaries and are
 * resolved at call time.
 */
import i18next from "i18next"

import type { AudioSource, SttEngine } from "@/lib/api"

export function sourceLabel(s: AudioSource): string {
  return i18next.t(`labels.source.${s}`)
}

/** Short form for tight spaces (compact overlay, level meters). */
export function sourceShortLabel(s: AudioSource): string {
  return i18next.t(`labels.sourceShort.${s}`)
}

/** One-line explanation shown under the source picker. */
export function sourceHint(): string {
  return i18next.t("labels.sourceHint")
}

export function engineLabel(e: SttEngine): string {
  return i18next.t(`labels.engine.${e}`)
}

export function engineHint(e: SttEngine): string {
  return i18next.t(`labels.engineHint.${e}`)
}
