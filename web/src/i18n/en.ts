/** English messages — the source of truth. vi.ts must mirror this
 *  shape exactly (missing AND extra keys are type errors). */
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
