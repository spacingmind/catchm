import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { SaveStatus, SegmentedControl, SettingRow, SettingsSection, descId, labelId } from "@/components/settings/pieces"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { applyLanguagePref, type UILanguagePref } from "@/i18n"
import { getConfig, type AppConfig } from "@/lib/api"
import { useTheme, type ThemePref } from "@/lib/theme"

const THEME_OPTIONS = [
  { value: "light", labelKey: "settings.appearance.light" },
  { value: "dark", labelKey: "settings.appearance.dark" },
  { value: "system", labelKey: "settings.appearance.system" },
] as const satisfies readonly { value: ThemePref; labelKey: string }[]

// Language names are always shown in their own language, so they live
// here rather than in the dictionaries.
const LANGUAGE_NAMES = { en: "English", vi: "Tiếng Việt" } as const

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

/** Appearance: theme (device-local via localStorage, applies instantly)
 *  and UI language (config-backed so Go can read it later; applies
 *  instantly, then saves with a save status). */
export function SettingsAppearance() {
  const { t } = useTranslation()
  const { pref, setPref } = useTheme()

  const [config, setConfig] = useState<AppConfig | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  const load = useCallback(() => {
    getConfig()
      .then(setConfig)
      .catch((err: unknown) => setLoadError(errorMessage(err)))
  }, [])
  useEffect(load, [load])

  const retryLoad = () => {
    setLoadError(null)
    load()
  }

  const { save, put } = useConfigSave(config, setConfig)

  const themeId = "appearance-theme"
  const langId = "appearance-language"

  const changeLanguage = (value: UILanguagePref) => {
    applyLanguagePref(value) // instant switch; the PUT persists it
    void put({ ui_language: value })
  }

  let languageControl
  if (loadError) {
    languageControl = (
      <div className="grid justify-items-end gap-2">
        <StatusMessage tone="error" className="break-words">
          {t("settings.appearance.languageLoadError", { error: loadError })}
        </StatusMessage>
        <Button size="sm" variant="outline" onClick={retryLoad}>
          {t("common.tryAgain")}
        </Button>
      </div>
    )
  } else if (!config) {
    languageControl = <Skeleton className="h-8 w-56" />
  } else {
    languageControl = (
      <div className="grid justify-items-end gap-1">
        <SegmentedControl
          id={langId}
          value={config.ui_language}
          onChange={changeLanguage}
          options={[
            { value: "system", label: t("settings.appearance.system") },
            { value: "en", label: LANGUAGE_NAMES.en },
            { value: "vi", label: LANGUAGE_NAMES.vi },
          ]}
          labelledBy={labelId(langId)}
          describedBy={descId(langId)}
        />
        <SaveStatus state={save.state} error={save.error} />
      </div>
    )
  }

  return (
    <SettingsSection title={t("settings.appearance.title")}>
      <SettingRow
        id={themeId}
        label={t("settings.appearance.themeLabel")}
        description={t("settings.appearance.themeDesc")}
        control={
          <SegmentedControl
            id={themeId}
            value={pref}
            onChange={setPref}
            options={THEME_OPTIONS.map((o) => ({ value: o.value, label: t(o.labelKey) }))}
            labelledBy={labelId(themeId)}
            describedBy={descId(themeId)}
          />
        }
      />
      <SettingRow
        id={langId}
        label={t("settings.appearance.languageLabel")}
        description={t("settings.appearance.languageDesc")}
        control={languageControl}
      />
    </SettingsSection>
  )
}
