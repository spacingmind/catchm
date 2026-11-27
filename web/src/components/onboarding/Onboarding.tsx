import { useEffect, useRef, useState } from "react"
import { Boxes, Check, Cpu, KeyRound, Languages, Loader2, SlidersHorizontal, type LucideIcon } from "lucide-react"
import { useTranslation } from "react-i18next"
import { cn } from "cn"

import { CloudStep, type CloudTranslator } from "@/components/onboarding/CloudStep"
import { LanguagesStep, type LanguageChoice } from "@/components/onboarding/LanguagesStep"
import { SelfHostStep } from "@/components/onboarding/SelfHostStep"
import { StatusMessage } from "@/components/StatusMessage"
import { ChoiceCard } from "@/components/settings/ChoiceCard"
import { SetupBuilder } from "@/components/settings/SetupBuilder"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useStatusCheck } from "@/hooks/useStatusCheck"
import {
  getMachineCheck,
  updateConfig,
  type AppConfig,
  type CloudSttEngine,
  type UpdateConfigRequest,
} from "@/lib/api"
import {
  engineKeySet,
  engineTranslates,
  isCloudEngine,
  modulePatch,
  presetPatch,
  PRESETS,
  type Preset,
} from "@/lib/setup"
import { readStartPrefs, writeStartPrefs } from "@/lib/startPrefs"

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

type Step = 1 | 2 | 3

const CHOICES: readonly Preset[] = ["cloud", "selfhost", "custom"]

const DESC_KEYS = {
  cloud: "setup.chooser.cloudDesc",
  selfhost: "setup.chooser.selfhostDesc",
  custom: "setup.chooser.customDesc",
} as const

/** Step 3's name, heading and icon follow the setup chosen in step 2. */
const STEP3 = {
  cloud: { label: "onboarding.steps.keys", title: "onboarding.cloud.title", desc: "onboarding.cloud.desc", icon: KeyRound },
  selfhost: {
    label: "onboarding.steps.check",
    title: "onboarding.selfhost.title",
    desc: "onboarding.selfhost.desc",
    icon: Cpu,
  },
  custom: {
    label: "onboarding.steps.custom",
    title: "onboarding.custom.title",
    desc: "onboarding.custom.desc",
    icon: SlidersHorizontal,
  },
} as const

const DEFAULT_ENGINE: CloudSttEngine = isCloudEngine(PRESETS.cloud.transcription) ? PRESETS.cloud.transcription : "soniox"

function initialLanguages(config: AppConfig): LanguageChoice {
  const prefs = readStartPrefs()
  const translateTo = prefs.translateTo ?? ""
  const lang = prefs.lang ?? config.lang ?? ""
  return { lang, translate: translateTo !== "", translateTo, twoWay: !!prefs.twoWay && lang !== "" }
}

/**
 * First run (config.setup === ""): a short wizard before the normal app.
 * 1 Languages (app language, the meetings' language, translation),
 * 2 Setup (Cloud BYOK, Self-host or Custom), 3 the setup's own step: a
 * provider and its key (Cloud), a machine check (Self-host) or the
 * builder (Custom). Finish records the setup with presetPatch (plus the
 * chosen engine and translator under Cloud); App then moves on to the
 * meetings list, where Get started lists what's left to do.
 */
export function Onboarding({ config: initial, onDone }: { config: AppConfig; onDone: () => void }) {
  const { t } = useTranslation()
  // Kept current by every save in the wizard (language, keys, builder).
  const [config, setConfig] = useState(initial)
  const [step, setStep] = useState<Step>(1)
  const [langs, setLangs] = useState<LanguageChoice>(() => initialLanguages(initial))
  const [langError, setLangError] = useState<string | null>(null)
  const [choice, setChoice] = useState<Preset>("cloud")
  const [engine, setEngine] = useState<CloudSttEngine>(() =>
    isCloudEngine(initial.engine) ? initial.engine : DEFAULT_ENGINE,
  )
  const [translator, setTranslator] = useState<CloudTranslator>(() =>
    initial.translate.provider === "llm" ? "llm" : "google",
  )
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const machine = useStatusCheck(getMachineCheck, { enabled: step === 3 && choice === "selfhost" })

  // Each new step (or a switch from Self-host to Cloud on step 3)
  // moves focus to its heading, so keyboard and screen reader users
  // start at the top of what changed. Not on first render.
  const headingRef = useRef<HTMLHeadingElement | null>(null)
  const focusKey = step === 3 ? `3-${choice}` : String(step)
  const firstFocus = useRef(true)
  useEffect(() => {
    if (firstFocus.current) {
      firstFocus.current = false
      return
    }
    headingRef.current?.focus()
  }, [focusKey])

  const go = (next: Step) => {
    setError(null)
    setStep(next)
  }

  const wantsTranslation = langs.translate && langs.translateTo !== ""

  /** Step 1 → 2: check the pair, then save the meeting language as the
   *  config default and the start-flow default. */
  const saveLanguages = async () => {
    if (langs.translate && !langs.translateTo) {
      setLangError(t("onboarding.languages.needTarget"))
      document.getElementById("ob-translate-to")?.focus()
      return
    }
    if (langs.translate && langs.translateTo === langs.lang) {
      setLangError(t("onboarding.languages.sameLanguage"))
      document.getElementById("ob-translate-to")?.focus()
      return
    }
    setLangError(null)
    const translateTo = langs.translate ? langs.translateTo : ""
    writeStartPrefs({
      ...readStartPrefs(),
      lang: langs.lang,
      translateTo,
      twoWay: translateTo !== "" && langs.lang !== "" && langs.twoWay,
    })
    // PUT is a partial update: "" can't clear a language, and a fresh
    // install starts without one anyway.
    if (langs.lang && langs.lang !== config.lang) {
      setSaving(true)
      setError(null)
      try {
        setConfig(await updateConfig({ lang: langs.lang }))
      } catch (err) {
        setError(t("onboarding.saveError", { error: errorMessage(err) }))
        setSaving(false)
        return
      }
      setSaving(false)
    }
    go(2)
  }

  const finishPatch = (): UpdateConfigRequest => {
    if (choice !== "cloud") return presetPatch(choice)
    const asCloud: AppConfig = { ...config, setup: "cloud" }
    const patch: UpdateConfigRequest = { ...presetPatch("cloud"), ...modulePatch(asCloud, "transcription", engine) }
    if (wantsTranslation && !engineTranslates(engine)) {
      patch.translate = { ...patch.translate, ...modulePatch(asCloud, "translation", translator).translate }
    }
    return patch
  }

  const finish = async () => {
    if (saving) return
    setSaving(true)
    setError(null)
    try {
      await updateConfig(finishPatch())
      // The saved setup ends first run: App unmounts this.
      onDone()
    } catch (err) {
      setError(t("onboarding.saveError", { error: errorMessage(err) }))
      setSaving(false)
    }
  }

  const next = () => {
    if (step === 1) void saveLanguages()
    else if (step === 2) go(3)
    else void finish()
  }

  const keyMissing = choice === "cloud" && !engineKeySet(config, engine)
  const selfhostWarn = choice === "selfhost" && machine.data != null && machine.data.selfhost !== "good"
  const nextLabel =
    step < 3
      ? t("onboarding.continue")
      : keyMissing
        ? t("onboarding.addKeyLater")
        : selfhostWarn
          ? t("onboarding.selfhost.confirm")
          : t("onboarding.finish")

  const step3 = STEP3[choice]
  const heading: { title: string; desc: string; icon: LucideIcon } =
    step === 1
      ? { title: t("onboarding.languages.title"), desc: t("onboarding.languages.desc"), icon: Languages }
      : step === 2
        ? { title: t("onboarding.setup.title"), desc: t("onboarding.setup.desc"), icon: Boxes }
        : { title: t(step3.title), desc: t(step3.desc), icon: step3.icon }
  const HeadingIcon = heading.icon
  const stepLabels = [t("onboarding.steps.languages"), t("onboarding.steps.setup"), t(step3.label)]

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <h1 className="mb-2 flex items-center gap-2 text-2xl font-semibold text-balance">
        <Boxes aria-hidden="true" className="size-6" />
        {t("setup.chooser.title")}
      </h1>
      <p className="mb-6 text-sm text-muted-foreground">{t("onboarding.desc")}</p>

      <ol aria-label={t("onboarding.stepsLabel")} className="mb-6 flex flex-wrap items-center gap-x-3 gap-y-2 text-sm">
        {stepLabels.map((label, i) => {
          const n = i + 1
          const state = n < step ? "done" : n === step ? "current" : "todo"
          return (
            <li
              key={n}
              aria-current={state === "current" ? "step" : undefined}
              className={cn(
                "flex items-center gap-2",
                state === "current" ? "font-medium text-foreground" : "text-muted-foreground",
              )}
            >
              {i > 0 && <span aria-hidden="true" className="h-px w-6 bg-border" />}
              <span
                aria-hidden="true"
                className={cn(
                  "flex size-5 items-center justify-center rounded-full text-xs font-medium",
                  state === "current"
                    ? "bg-primary text-primary-foreground"
                    : state === "done"
                      ? "bg-primary/15 text-primary"
                      : "border border-border",
                )}
              >
                {state === "done" ? <Check className="size-3" /> : n}
              </span>
              {label}
              {state === "done" && <span className="sr-only">{t("onboarding.stepDone")}</span>}
            </li>
          )
        })}
      </ol>

      <section aria-labelledby="ob-step-title">
        <h2
          id="ob-step-title"
          ref={headingRef}
          tabIndex={-1}
          className="flex items-center gap-2 text-lg font-semibold outline-none"
        >
          <HeadingIcon aria-hidden="true" className="size-4" />
          {heading.title}
        </h2>
        <p id="ob-step-desc" className="mb-2 mt-0.5 text-sm text-muted-foreground">
          {heading.desc}
        </p>

        {step === 1 && (
          <LanguagesStep
            config={config}
            onConfig={setConfig}
            value={langs}
            onChange={(v) => {
              setLangs(v)
              setLangError(null)
            }}
            error={langError}
          />
        )}

        {step === 2 && (
          <div
            role="radiogroup"
            aria-labelledby="ob-step-title"
            aria-describedby="ob-step-desc"
            className="mt-4 grid gap-3"
          >
            {CHOICES.map((p) => (
              <ChoiceCard
                key={p}
                name="setup-choice"
                value={p}
                checked={choice === p}
                onChange={(v) => setChoice(v as Preset)}
                title={t(`setup.presets.${p}`)}
                description={t(DESC_KEYS[p])}
                extra={p === "cloud" && wantsTranslation ? t("onboarding.setup.sonioxTranslates") : undefined}
                badges={
                  p === "cloud" ? (
                    <Badge variant="secondary" className="border-transparent bg-primary/15 text-primary">
                      {t("setup.chooser.recommended")}
                    </Badge>
                  ) : undefined
                }
                className="p-4"
              />
            ))}
          </div>
        )}

        {step === 3 && (
          <div className="mt-4">
            {choice === "cloud" && (
              <CloudStep
                config={config}
                onConfig={setConfig}
                headingId="ob-step-title"
                engine={engine}
                onEngine={setEngine}
                wantsTranslation={wantsTranslation}
                translator={translator}
                onTranslator={setTranslator}
              />
            )}
            {choice === "selfhost" && <SelfHostStep check={machine} onUseCloud={() => setChoice("cloud")} />}
            {choice === "custom" && <SetupBuilder config={config} onSaved={setConfig} inline />}
          </div>
        )}
      </section>

      <StatusMessage tone="error" className="mt-4 break-words">
        {error}
      </StatusMessage>
      <div className={cn("mt-8 flex items-center gap-3", step > 1 ? "justify-between" : "justify-end")}>
        {step > 1 && (
          <Button variant="outline" onClick={() => go((step - 1) as Step)} disabled={saving}>
            {t("common.back")}
          </Button>
        )}
        <Button
          variant={keyMissing && step === 3 ? "outline" : "default"}
          onClick={next}
          disabled={saving}
          aria-busy={saving || undefined}
        >
          {saving && <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />}
          {saving ? t("onboarding.saving") : nextLabel}
        </Button>
      </div>
    </div>
  )
}
