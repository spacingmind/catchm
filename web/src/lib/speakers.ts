import type { TFunction } from "i18next"

import type { Utterance } from "@/lib/api"

/** Keys for unlabelled utterances with a known input (internal/meeting). */
export const SPEAKER_ME = "me"
export const SPEAKER_OTHERS = "others"

const LABEL_NUMBER = /^(?:speaker[ _]?|spk[_ ]?|s)?(\d{1,3})$/i

/** The key a speaker is named under; mirrors meeting.SpeakerKey. */
export function speakerKey(u: Pick<Utterance, "speaker" | "source">): string {
  const label = u.speaker?.trim()
  if (label) {
    const m = LABEL_NUMBER.exec(label)
    return m ? String(Number(m[1])) : label
  }
  if (u.source === "mic") return SPEAKER_ME
  if (u.source === "system") return SPEAKER_OTHERS
  return ""
}

export interface SpeakerNaming {
  /** Confirmed names (Meeting.speakers). */
  names?: Record<string, string>
  /** The user's own name (config profile.name). */
  me?: string
  /** The speaker the user said is them (Meeting.me). */
  meKey?: string
  /** The microphone's guess when they haven't (autoMeKey). */
  autoMe?: string
}

/** Whether key is the user; mirrors meeting.SpeakerNamer.IsMe. */
export function isMe(key: string, naming: SpeakerNaming): boolean {
  return key !== "" && (key === SPEAKER_ME || key === naming.meKey || key === naming.autoMe)
}

/** The engine speaker clearly carried by the microphone; mirrors meeting.AutoMeKey. */
export function autoMeKey(us: readonly Pick<Utterance, "speaker" | "source" | "text">[], names?: Record<string, string>): string {
  const stats = new Map<string, { lines: number; mic: number }>()
  for (const u of us) {
    const key = speakerKey(u)
    if (!key || key === SPEAKER_ME || key === SPEAKER_OTHERS || !u.text.trim()) continue
    const st = stats.get(key) ?? { lines: 0, mic: 0 }
    st.lines++
    if (u.source === "mic") st.mic++
    stats.set(key, st)
  }
  const found = [...stats].filter(([, st]) => st.lines >= 3 && st.mic >= 0.8 * st.lines).map(([k]) => k)
  if (found.length !== 1 || names?.[found[0]]?.trim()) return ""
  return found[0]
}

/** What a speaker key reads as; mirrors meeting.SpeakerNamer.Label. */
export function speakerLabel(key: string, naming: SpeakerNaming, t: TFunction): string {
  if (!key) return ""
  const confirmed = naming.names?.[key]?.trim()
  if (confirmed) return confirmed
  if (isMe(key, naming)) return naming.me?.trim() || t("speakers.me")
  if (key === SPEAKER_OTHERS) return t("speakers.others")
  if (/^\d+$/.test(key)) return t("speakers.numbered", { n: key })
  return key
}
