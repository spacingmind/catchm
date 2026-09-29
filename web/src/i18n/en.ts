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
    cancel: "Cancel",
  },
  labels: {
    source: {
      mic: "Microphone",
      system: "Computer audio",
      both: "Both",
    },
    sourceShort: {
      mic: "Mic",
      system: "Computer",
      both: "Mic + computer",
    },
    sourceHint:
      "Online meeting (Zoom, Meet, Teams)? Choose Both to hear you and the others. In a room, Microphone is enough.",
    engine: {
      whisper: "On this computer",
      soniox: "Cloud (Soniox)",
    },
    engineHint: {
      whisper: "Free and private. Needs the free Whisper server running on this computer.",
      soniox: "More accurate and tells speakers apart. Needs a paid Soniox API key.",
    },
  },
  settings: {
    nav: {
      recording: "Recording",
      transcription: "Transcription",
      translation: "Translation",
      summaries: "Summaries",
      cloudSync: "Cloud sync",
      appearance: "Appearance",
      shortcuts: "Shortcuts",
      system: "System",
      title: "Settings",
      sections: "Settings sections",
    },
    pieces: {
      saving: "Saving…",
      saved: "Saved",
      saveError: "Couldn’t save. Try again, or restart CatchM if it keeps failing.",
      opensInBrowser: " (opens in your browser)",
    },
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
