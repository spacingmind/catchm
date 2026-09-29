/**
 * English messages — the source of truth. vi.ts must mirror this shape
 * exactly (missing AND extra keys are type errors).
 *
 * Conventions:
 * - Keys are grouped by screen (`meetingList.*`, `settings.translation.*`);
 *   `common.*` only for strings reused verbatim on 3+ screens.
 * - Components: `const { t } = useTranslation()`. Non-React modules call
 *   `i18next.t` inside a function evaluated at render time, never at
 *   module load (the language can change while the app runs).
 * - Variables: `{{name}}`. Plurals: `key_one` / `key_other` + `{ count }`
 *   (vi only needs `_other`, but must mirror both keys).
 * - Inline markup (links, <strong>, <kbd>): `<Trans i18nKey components>`
 *   with `<0>…</0>` placeholders, never string concatenation.
 * - Translate aria-label, title, placeholder and alt too.
 * - Never translate: brand/product names (CatchM, Soniox, Whisper,
 *   Ollama, OpenAI, Zoom…), key names, file paths, user data, wire values.
 */
export const en = {
  common: {
    tryAgain: "Try again",
  },
  settings: {
    appearance: {
      title: "Appearance",
      themeLabel: "Theme",
      themeDesc: "Stored on this device only. System follows your OS light or dark setting.",
      light: "Light",
      dark: "Dark",
      system: "System",
      languageLabel: "Language",
      languageDesc: "Saved in your CatchM settings. System follows your computer's language.",
      languageLoadError: "Couldn’t load the language setting: {{error}}",
    },
  },
} as const

type Widen<T> = { [K in keyof T]: T[K] extends string ? string : Widen<T[K]> }
export type Messages = Widen<typeof en>
