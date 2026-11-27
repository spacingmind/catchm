import { useTranslation } from "react-i18next"

import { StatusMessage } from "@/components/StatusMessage"
import { descId, labelId, SaveStatus, SegmentedControl, SettingRow } from "@/components/settings/pieces"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { Checkbox } from "@/components/ui/checkbox"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { applyLanguagePref, type UILanguagePref } from "@/i18n"
import type { AppConfig } from "@/lib/api"
import { languageLabel, sortedLanguageCodes } from "@/lib/languages"

/** What step 1 asks; saved as config `lang` and the start-flow prefs. */
export interface LanguageChoice {
  /** Meeting language; "" = detect automatically. */
  lang: string
  translate: boolean
  /** Translate into; only meaningful while `translate`. */
  translateTo: string
  /** Both ways; only offered when `lang` is set. */
  twoWay: boolean
}

// Language names are shown in their own language, as in Settings →
// Appearance.
const UI_LANGUAGE_NAMES = { en: "English", vi: "Tiếng Việt" } as const

/** Radix Select can't have an item with value "" ("detect"). */
const DETECT = "__detect__"

/**
 * Step 1: the app language (applied at once and saved, like Settings →
 * Appearance), the meetings' language and whether to translate them.
 * The meeting choices are saved when the user continues.
 */
export function LanguagesStep({
  config,
  onConfig,
  value,
  onChange,
  error,
}: {
  config: AppConfig
  onConfig: (c: AppConfig) => void
  value: LanguageChoice
  onChange: (v: LanguageChoice) => void
  /** Why the choice can't be saved yet (no target language…). */
  error: string | null
}) {
  const { t, i18n } = useTranslation()
  const { save, put } = useConfigSave(config, onConfig)
  const codes = sortedLanguageCodes()

  const changeUiLanguage = (v: UILanguagePref) => {
    applyLanguagePref(v) // instant switch; the PUT persists it
    void put({ ui_language: v })
  }

  const setLang = (v: string) => {
    const lang = v === DETECT ? "" : v
    // Two-way needs both languages.
    onChange({ ...value, lang, twoWay: lang ? value.twoWay : false })
  }

  const setTranslate = (on: boolean) => {
    // Suggest the language the app is shown in, when it isn't the
    // meeting's own.
    const uiLang = i18n.language
    const suggested = !value.translateTo && uiLang !== value.lang ? uiLang : value.translateTo
    onChange({ ...value, translate: on, translateTo: on ? suggested : value.translateTo })
  }

  return (
    <div>
      <SettingRow
        id="ob-ui-language"
        label={t("onboarding.languages.appLanguage")}
        description={t("onboarding.languages.appLanguageDesc")}
        control={
          <div className="grid justify-items-end gap-1">
            <SegmentedControl
              id="ob-ui-language"
              value={config.ui_language}
              onChange={changeUiLanguage}
              options={[
                { value: "system", label: t("settings.appearance.system") },
                { value: "en", label: UI_LANGUAGE_NAMES.en },
                { value: "vi", label: UI_LANGUAGE_NAMES.vi },
              ]}
              labelledBy={labelId("ob-ui-language")}
              describedBy={descId("ob-ui-language")}
            />
            <SaveStatus state={save.state} error={save.error} />
          </div>
        }
      />

      <SettingRow
        htmlFor="ob-meeting-lang"
        label={t("onboarding.languages.meetingLanguage")}
        description={t("onboarding.languages.meetingLanguageDesc")}
        control={
          <Select value={value.lang || DETECT} onValueChange={setLang}>
            <SelectTrigger id="ob-meeting-lang" className="w-64" aria-describedby={descId("ob-meeting-lang")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={DETECT}>{t("onboarding.languages.detect")}</SelectItem>
              {codes.map((c) => (
                <SelectItem key={c} value={c}>
                  {languageLabel(c)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        }
      />

      <SettingRow
        id="ob-translate"
        label={t("onboarding.languages.translate")}
        description={t("onboarding.languages.translateDesc")}
        control={
          <SegmentedControl
            id="ob-translate"
            value={value.translate ? "yes" : "no"}
            onChange={(v) => setTranslate(v === "yes")}
            options={[
              { value: "no", label: t("onboarding.languages.no") },
              { value: "yes", label: t("onboarding.languages.yes") },
            ]}
            labelledBy={labelId("ob-translate")}
            describedBy={descId("ob-translate")}
          />
        }
      />

      {value.translate && (
        <SettingRow
          htmlFor="ob-translate-to"
          label={t("onboarding.languages.translateInto")}
          description={t("onboarding.languages.translateIntoDesc")}
          control={
            <div className="grid w-64 gap-1.5">
              <Select value={value.translateTo || undefined} onValueChange={(v) => onChange({ ...value, translateTo: v })}>
                <SelectTrigger
                  id="ob-translate-to"
                  className="w-full"
                  aria-invalid={error ? true : undefined}
                  aria-describedby={error ? `${descId("ob-translate-to")} ob-translate-error` : descId("ob-translate-to")}
                >
                  <SelectValue placeholder={t("onboarding.languages.chooseLanguage")} />
                </SelectTrigger>
                <SelectContent>
                  {codes.map((c) => (
                    <SelectItem key={c} value={c}>
                      {languageLabel(c)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <StatusMessage id="ob-translate-error" tone="error">
                {error}
              </StatusMessage>
            </div>
          }
        />
      )}

      {value.translate && value.lang && (
        <SettingRow
          htmlFor="ob-two-way"
          label={t("onboarding.languages.twoWay")}
          description={t("onboarding.languages.twoWayDesc")}
          control={
            <Checkbox
              id="ob-two-way"
              checked={value.twoWay}
              onCheckedChange={(c) => onChange({ ...value, twoWay: c === true })}
              aria-describedby={descId("ob-two-way")}
            />
          }
        />
      )}
    </div>
  )
}
