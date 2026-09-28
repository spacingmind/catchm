/**
 * User-facing names for audio sources and transcription engines, shared
 * by every screen so a non-technical user sees one name per concept
 * (the start popover, live status row, compact overlay and Settings all
 * read from here). Wire values stay "mic" / "system" / "both" and
 * "whisper" / "soniox".
 */
import type { AudioSource, SttEngine } from "@/lib/api"

export const SOURCE_LABEL: Record<AudioSource, string> = {
  mic: "Microphone",
  system: "Computer audio",
  both: "Both",
}

/** Short form for tight spaces (compact overlay, level meters). */
export const SOURCE_SHORT_LABEL: Record<AudioSource, string> = {
  mic: "Mic",
  system: "Computer",
  both: "Mic + computer",
}

/** One-line explanation shown under the source picker. */
export const SOURCE_HINT =
  "Online meeting (Zoom, Meet, Teams)? Choose Both to hear you and the others. In a room, Microphone is enough."

export const ENGINE_LABEL: Record<SttEngine, string> = {
  whisper: "On this computer",
  soniox: "Cloud (Soniox)",
}

export const ENGINE_HINT: Record<SttEngine, string> = {
  whisper: "Free and private. Needs the Whisper server set up in Settings → Transcription.",
  soniox: "More accurate and tells speakers apart. Needs a paid Soniox API key.",
}
