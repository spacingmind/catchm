import i18next from "i18next"
import { initReactI18next } from "react-i18next"

import { en } from "./en"
import { vi } from "./vi"

export type UILanguagePref = "system" | "en" | "vi"

export { en, vi }

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "translation"
    resources: { translation: typeof en }
  }
}

let pref: UILanguagePref = "system"

/** "system" resolves against the browser/OS language; unknown values
 *  are treated as "system". */
export function resolveLanguage(p: string): "en" | "vi" {
  if (p === "en" || p === "vi") return p
  for (const tag of navigator.languages ?? []) {
    if (tag.toLowerCase().startsWith("vi")) return "vi"
  }
  return "en"
}

export function applyLanguagePref(p: UILanguagePref) {
  // An older backend sends no ui_language at all: treat it as "system".
  pref = p === "en" || p === "vi" ? p : "system"
  void i18next.changeLanguage(resolveLanguage(p))
}

const setDocLang = (lng: string) => {
  document.documentElement.lang = lng
}

export function initI18n() {
  void i18next.use(initReactI18next).init({
    resources: { en: { translation: en }, vi: { translation: vi } },
    lng: resolveLanguage(pref),
    fallbackLng: "en",
    interpolation: { escapeValue: false },
  })
  setDocLang(i18next.language)
  i18next.on("languageChanged", setDocLang)
  // While the pref is "system", follow OS language changes live; an
  // explicit pref wins until the user changes it again.
  window.addEventListener("languagechange", () => {
    if (pref === "system") void i18next.changeLanguage(resolveLanguage(pref))
  })
}
