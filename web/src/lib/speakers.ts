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
}

/** What a speaker key reads as; mirrors meeting.SpeakerNamer.Label. */
export function speakerLabel(key: string, naming: SpeakerNaming, t: TFunction): string {
  if (!key) return ""
  const confirmed = naming.names?.[key]?.trim()
  if (confirmed) return confirmed
  if (key === SPEAKER_ME) return naming.me?.trim() || t("speakers.me")
  if (key === SPEAKER_OTHERS) return t("speakers.others")
  if (/^\d+$/.test(key)) return t("speakers.numbered", { n: key })
  return key
}
