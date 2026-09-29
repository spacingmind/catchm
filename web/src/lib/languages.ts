import { activeLocale } from "@/i18n"

/**
 * Language options shared by the start popover and the translation test
 * form. Codes are what the engines expect (ISO 639-1 mostly); the label
 * is what humans pick. Kept small on purpose — the full CLDR list is
 * overkill for a meeting app.
 */
export const LANGUAGES: { code: string; label: string }[] = [
  { code: "en", label: "English" },
  { code: "vi", label: "Vietnamese" },
  { code: "es", label: "Spanish" },
  { code: "fr", label: "French" },
  { code: "de", label: "German" },
  { code: "it", label: "Italian" },
  { code: "pt", label: "Portuguese" },
  { code: "nl", label: "Dutch" },
  { code: "ru", label: "Russian" },
  { code: "uk", label: "Ukrainian" },
  { code: "pl", label: "Polish" },
  { code: "tr", label: "Turkish" },
  { code: "ar", label: "Arabic" },
  { code: "fa", label: "Persian" },
  { code: "hi", label: "Hindi" },
  { code: "bn", label: "Bengali" },
  { code: "th", label: "Thai" },
  { code: "id", label: "Indonesian" },
  { code: "ms", label: "Malay" },
  { code: "zh", label: "Chinese" },
  { code: "ja", label: "Japanese" },
  { code: "ko", label: "Korean" },
]

let cachedLocale = ""
let displayNames: Intl.DisplayNames | null = null

/** The language's name in the active UI locale ("Tiếng Anh" for "en"
 *  in vi, "English" in en), falling back to the English label, then the
 *  code. First letter capitalized — some locales return lowercase. */
export function languageLabel(code: string): string {
  const locale = activeLocale()
  if (locale !== cachedLocale) {
    cachedLocale = locale
    try {
      displayNames = new Intl.DisplayNames(locale, { type: "language" })
    } catch {
      displayNames = null
    }
  }
  const name = displayNames?.of(code)
  if (name) {
    return name.charAt(0).toLocaleUpperCase(locale) + name.slice(1)
  }
  const hit = LANGUAGES.find((l) => l.code === code)
  return hit ? hit.label : code
}
