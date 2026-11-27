import { useState } from "react"
import { Languages, Sparkles, Speech, type LucideIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { ChoiceCard } from "@/components/settings/ChoiceCard"
import { descId, labelId, SaveStatus, SegmentedControl, SettingRow } from "@/components/settings/pieces"
import { CloudConsentDialog } from "@/components/settings/SettingsSummaries"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useStatusCheck } from "@/hooks/useStatusCheck"
import {
  getSttStatus,
  getSummaryStatus,
  getTranslateStatus,
  type AppConfig,
  type UpdateConfigRequest,
} from "@/lib/api"
import { navigate } from "@/lib/route"
import {
  MODULES,
  currentChoices,
  engineKeySet,
  engineOptionName,
  engineTranslates,
  isCloudEngine,
  keyAdded,
  lockedReason,
  modulePatch,
  needsTranslatorText,
  optionLockedKey,
  optionsVisible,
  presetPatch,
  translateNeed,
  type ModuleId,
  type Preset,
  type SetupOption,
} from "@/lib/setup"

const MODULE_ICONS: Record<ModuleId, LucideIcon> = {
  transcription: Speech,
  translation: Languages,
  summaries: Sparkles,
}

const MODULE_TITLE_KEYS = {
  transcription: "settings.nav.transcription",
  translation: "settings.nav.translation",
  summaries: "settings.nav.summaries",
} as const

const MODULE_DESC_KEYS = {
  transcription: "setup.modules.transcriptionDesc",
  translation: "setup.modules.translationDesc",
  summaries: "setup.modules.summariesDesc",
} as const

const PRESET_IDS: readonly Preset[] = ["cloud", "selfhost", "custom"]

const READY_BADGE = "border-transparent bg-primary/15 text-primary"

/** What the chosen option still needs: null while unknown. */
interface Need {
  ready: boolean | null
  text?: string
  action?: { label: string; run: () => void }
}

/**
 * The stack builder (docs/plans/active/cloud-mode.md → Builder): a
 * preset control on top, then one section per module with a card per
 * option the setup shows. Choosing a preset fills every module; choosing
 * an option saves it, and turns a Cloud/Self-host setup into Custom when
 * the result no longer matches the preset. Under each module, what the
 * chosen option still needs, with the way to its Settings page.
 *
 * `inline` (first run, Custom): no preset control and no links away;
 * the chooser records the setup when the user continues.
 */
export function SetupBuilder({
  config,
  onSaved,
  inline = false,
}: {
  config: AppConfig
  onSaved: (config: AppConfig) => void
  inline?: boolean
}) {
  const { t } = useTranslation()
  const choices = currentChoices(config)
  const [consentOpen, setConsentOpen] = useState(false)

  const stt = useStatusCheck(getSttStatus)
  const translateChecked = choices.translation !== "none"
  const translate = useStatusCheck(getTranslateStatus, { enabled: translateChecked })
  const summary = useStatusCheck(getSummaryStatus)
  const refreshAll = () => {
    stt.refresh()
    if (translateChecked) translate.refresh()
    summary.refresh()
  }

  // One save state per area, so each status shows next to its control.
  const saves = {
    preset: useConfigSave(config, onSaved),
    transcription: useConfigSave(config, onSaved),
    translation: useConfigSave(config, onSaved),
    summaries: useConfigSave(config, onSaved),
  }
  const save = (area: keyof typeof saves, patch: UpdateConfigRequest) => {
    void saves[area].put(patch).then((ok) => ok && refreshAll())
  }

  const preset: Preset = config.setup || "custom"
  const choosePreset = (p: Preset) => {
    if (p !== config.setup) save("preset", presetPatch(p))
  }

  const choose = (module: ModuleId, id: string) => {
    if (id === choices[module]) return
    save(module, modulePatch(config, module, id as SetupOption["id"]))
  }

  const openSection = (section: string) => navigate({ name: "settings", section })
  const setUp = (section: string) => ({ label: t("getStarted.setUp"), run: () => openSection(section) })

  const need = (module: ModuleId): Need => {
    if (module === "transcription") {
      const engine = choices.transcription
      if (isCloudEngine(engine) && !engineKeySet(config, engine)) {
        return {
          ready: false,
          text: t("setup.status.addEngineKey", { provider: engineOptionName(engine) }),
          action: { label: t("settings.transcription.addKey"), run: () => openSection("transcription") },
        }
      }
      const st = stt.data?.[engine]
      if (!st) return { ready: null }
      return st.ready ? { ready: true } : { ready: false, text: st.reason, action: setUp("transcription") }
    }
    if (module === "translation") {
      if (choices.translation === "none") {
        return engineTranslates(config.engine) ? { ready: true } : { ready: false, text: needsTranslatorText(config.engine) }
      }
      // Status for a provider a save just replaced isn't shown.
      const st = translate.data && translate.data.provider === choices.translation ? translate.data : null
      if (!st) return { ready: null }
      if (st.ready) return { ready: true }
      return { ready: false, text: translateNeed(st), action: setUp("translation") }
    }
    // Status for a provider a save just replaced isn't shown.
    const st = summary.data && summary.data.provider === choices.summaries ? summary.data : null
    if (!st) return { ready: null }
    if (st.ready) return { ready: true }
    const text = st.reason ?? t("setup.status.notReadyFallback")
    if (st.provider === "catchm-cloud" && st.cloud?.logged_in === false) {
      return { ready: false, text, action: { label: t("settings.summaries.openCloudSync"), run: () => openSection("cloud") } }
    }
    if (st.provider === "catchm-cloud" && st.cloud && !st.cloud.consent) {
      return { ready: false, text, action: { label: t("setup.status.allow"), run: () => setConsentOpen(true) } }
    }
    return { ready: false, text, action: setUp("summaries") }
  }

  return (
    <div>
      {!inline && (
        <div className="mb-8">
          <SettingRow
            label={t("setup.presetLabel")}
            id="setup-preset"
            description={t(`setup.presetDesc.${preset}`)}
            control={
              <div className="grid justify-items-end gap-1">
                <SegmentedControl
                  id="setup-preset"
                  labelledBy={labelId("setup-preset")}
                  describedBy={descId("setup-preset")}
                  value={preset}
                  onChange={choosePreset}
                  options={PRESET_IDS.map((p) => ({ value: p, label: t(`setup.presets.${p}`) }))}
                />
                <SaveStatus state={saves.preset.save.state} error={saves.preset.save.error} />
              </div>
            }
          />
        </div>
      )}

      {MODULES.map((module) => {
        const Icon = MODULE_ICONS[module]
        const headingId = `setup-${module}-title`
        const n = need(module)
        const areaSave = saves[module].save
        return (
          <section key={module} aria-labelledby={headingId} className="mb-8">
            <h2 id={headingId} className="flex items-center gap-2 text-lg font-semibold">
              <Icon aria-hidden="true" className="size-4" />
              {t(MODULE_TITLE_KEYS[module])}
            </h2>
            <p className="mb-2 mt-0.5 text-sm text-muted-foreground">{t(MODULE_DESC_KEYS[module])}</p>
            <div role="radiogroup" aria-labelledby={headingId} className="grid gap-3 sm:grid-cols-2">
              {optionsVisible(config.setup, module).map((o) => (
                <ChoiceCard
                  key={o.id}
                  name={`setup-${module}`}
                  value={o.id}
                  checked={choices[module] === o.id}
                  onChange={(id) => choose(module, id)}
                  title={t(o.labelKey)}
                  description={t(o.descKey)}
                  badges={hasBadges(config, o) ? <OptionBadges config={config} option={o} /> : undefined}
                  disabled={optionLockedKey(config, module, o.id) !== null}
                  reason={lockedReason(t, config, module, o.id)}
                />
              ))}
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-3">
              <NeedBadge ready={n.ready} />
              <p className="min-w-0 flex-1 text-sm text-muted-foreground">{n.text}</p>
              {n.action && !inline && (
                <Button size="sm" variant="outline" onClick={n.action.run}>
                  {n.action.label}
                </Button>
              )}
            </div>
            <SaveStatus state={areaSave.state} error={areaSave.error} className="mt-2" />
          </section>
        )
      })}

      <CloudConsentDialog
        open={consentOpen}
        onOpenChange={setConsentOpen}
        onAccepted={(c) => {
          onSaved(c)
          summary.refresh()
        }}
      />
    </div>
  )
}

function hasBadges(config: AppConfig, o: SetupOption): boolean {
  return o.badges.length > 0 || keyAdded(config, o)
}

/** An option card's badges: its facts ("Speakers", "Free tier"…) and
 *  "Key added" once its key is stored. Pass to ChoiceCard's `badges`
 *  only when the option has any (see SetupOption.badges, keyAdded). */
export function OptionBadges({ config, option }: { config: AppConfig; option: SetupOption }) {
  const { t } = useTranslation()
  return (
    <>
      {option.badges.map((b) => (
        <Badge key={b} variant="secondary">
          {t(`setup.badges.${b}`)}
        </Badge>
      ))}
      {keyAdded(config, option) && (
        <Badge variant="secondary" className={READY_BADGE}>
          {t("setup.badges.keyAdded")}
        </Badge>
      )}
    </>
  )
}

/** "Ready" / "Not set up" / pulsing "Checking…", as in Get started. */
function NeedBadge({ ready }: { ready: boolean | null }) {
  const { t } = useTranslation()
  if (ready === null) {
    return (
      <Badge variant="outline" className="motion-safe:animate-pulse">
        {t("getStarted.checking")}
      </Badge>
    )
  }
  return ready ? (
    <Badge variant="secondary" className={READY_BADGE}>
      {t("getStarted.ready")}
    </Badge>
  ) : (
    <Badge variant="outline">{t("getStarted.not-ready")}</Badge>
  )
}
