import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react"
import { Check, ShieldAlert, TriangleAlert } from "lucide-react"
import i18next from "i18next"
import { Trans, useTranslation } from "react-i18next"
import { cn } from "cn"

import { ConfirmDialog } from "@/components/ConfirmDialog"
import { Disclosure } from "@/components/Disclosure"
import { StatusMessage } from "@/components/StatusMessage"
import {
  descId,
  ExternalLink,
  SaveStatus,
  SettingRow,
  SettingsSection,
} from "@/components/settings/pieces"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { useAnnounceChange, useStatusCheck } from "@/hooks/useStatusCheck"
import {
  getConfig,
  getSummaryStatus,
  getTranslateStatus,
  pullTranslateModel,
  updateConfig,
  type AppConfig,
  type SummaryProvider,
  type SummaryStatus,
  type TranslateModelStatus,
} from "@/lib/api"
import { formatDateTime } from "@/lib/format"
import { noAutofill } from "@/lib/inputProps"
import { formatGb, modelDownloadGb } from "@/lib/modelSizes"
import { navigate } from "@/lib/route"

const OLLAMA_DOWNLOAD = "https://ollama.com/download"

const PERCENT_RE = /(\d+)%/

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

interface ProviderOption {
  value: SummaryProvider
  labelKey: (typeof PROVIDER_LABEL_KEYS)[SummaryProvider]
  descriptionKey: (typeof PROVIDER_DESC_KEYS)[SummaryProvider]
}

const PROVIDER_LABEL_KEYS = {
  ollama: "settings.summaries.providerOllama",
  "catchm-cloud": "settings.summaries.providerCloud",
  openai: "settings.summaries.providerCloudflare",
} as const
const PROVIDER_DESC_KEYS = {
  ollama: "settings.summaries.providerOllamaDesc",
  "catchm-cloud": "settings.summaries.providerCloudDesc",
  openai: "settings.summaries.providerCloudflareDesc",
} as const

const PROVIDERS: ProviderOption[] = [
  {
    value: "ollama",
    labelKey: "settings.summaries.providerOllama",
    descriptionKey: "settings.summaries.providerOllamaDesc",
  },
  {
    value: "catchm-cloud",
    labelKey: "settings.summaries.providerCloud",
    descriptionKey: "settings.summaries.providerCloudDesc",
  },
  {
    value: "openai",
    labelKey: "settings.summaries.providerCloudflare",
    descriptionKey: "settings.summaries.providerCloudflareDesc",
  },
]

interface ModelPreset {
  model: string
  /** Shown when this computer's memory is unknown. */
  labelKey: (typeof PRESET_LABEL_KEYS)[keyof typeof PRESET_LABEL_KEYS]
  plainLabelKey: (typeof PRESET_LABEL_KEYS)[keyof typeof PRESET_LABEL_KEYS]
  ramGb: number
  needsKey: (typeof PRESET_NEEDS_KEYS)[keyof typeof PRESET_NEEDS_KEYS]
}

const PRESET_LABEL_KEYS = {
  light: "settings.summaries.presetLight",
  lightPlain: "settings.summaries.presetLightPlain",
  recommended: "settings.summaries.presetRecommended",
  standard: "settings.summaries.presetStandard",
  alsoRecommended: "settings.summaries.presetAlsoRecommended",
  standardAlt: "settings.summaries.presetStandardAlt",
  best: "settings.summaries.presetBest",
  bestPlain: "settings.summaries.presetBestPlain",
} as const
const PRESET_NEEDS_KEYS = {
  needs8: "settings.summaries.presetNeeds8",
  needs16: "settings.summaries.presetNeeds16",
  needs32: "settings.summaries.presetNeeds32",
} as const

const MODEL_PRESETS: ModelPreset[] = [
  {
    model: "qwen3.5:4b",
    labelKey: "settings.summaries.presetLight",
    plainLabelKey: "settings.summaries.presetLightPlain",
    ramGb: 8,
    needsKey: "settings.summaries.presetNeeds8",
  },
  {
    model: "qwen3.5:9b",
    labelKey: "settings.summaries.presetRecommended",
    plainLabelKey: "settings.summaries.presetStandard",
    ramGb: 16,
    needsKey: "settings.summaries.presetNeeds16",
  },
  {
    model: "gemma4:12b",
    labelKey: "settings.summaries.presetAlsoRecommended",
    plainLabelKey: "settings.summaries.presetStandardAlt",
    ramGb: 16,
    needsKey: "settings.summaries.presetNeeds16",
  },
  {
    model: "qwen3.5:27b",
    labelKey: "settings.summaries.presetBest",
    plainLabelKey: "settings.summaries.presetBestPlain",
    ramGb: 32,
    needsKey: "settings.summaries.presetNeeds32",
  },
]

/** A "16 GB" computer reports a little less (about 15.6 GiB, often less
 *  after reserved memory), so a model fits when the computer has at
 *  least 85% of what it needs. */
const fitsMemory = (p: ModelPreset, totalRamGb: number) => totalRamGb >= p.ramGb * 0.85

/** Model sizes are decimal GB; free disk space (system.free_disk_gb) is
 *  GiB. A download fits when it leaves at least this much (GiB) free. */
const DISK_MARGIN_GIB = 0.5
const gbToGib = (gb: number) => (gb * 1e9) / 2 ** 30

/** Plain words for where "On this computer" setup stands, for the live
 *  region; null while unknown or when another provider is chosen. */
function setupProgressText(status: SummaryStatus | null): string | null {
  const ollama = status?.ollama
  if (!status || status.provider !== "ollama" || !ollama) return null
  if (status.ready) return i18next.t("settings.summaries.setupReady")
  if (!ollama.reachable) return i18next.t("settings.summaries.setupOllamaNotRunning")
  if (!ollama.installed) return i18next.t("settings.summaries.setupOllamaFound")
  return status.reason ?? i18next.t("settings.summaries.setupNotReady")
}

/** The biggest preset that fits this computer's memory (the first of a
 *  tie, so the 9b over the 12b), or null when none fits. */
function recommendedPreset(totalRamGb: number): ModelPreset | null {
  let best: ModelPreset | null = null
  for (const p of MODEL_PRESETS) {
    if (fitsMemory(p, totalRamGb) && (!best || p.ramGb > best.ramGb)) best = p
  }
  return best
}

/** Translation models that are downloaded, each name once. */
function installedModels(models: TranslateModelStatus[]): string[] {
  return [...new Set(models.filter((m) => m.installed).map((m) => m.name))]
}

/** How often "On this computer" re-checks while a step is still to do. */
const SETUP_POLL_MS = 5000

const CUSTOM_MODEL = "custom"

/** The "Your Cloudflare account" option is the OpenAI-compatible
 *  provider pointed at Workers AI: only the Account ID and API token are
 *  asked for; the base URL and model are derived (no new config field). */
const CLOUDFLARE_ACCOUNTS_PREFIX = "https://api.cloudflare.com/client/v4/accounts/"
const CLOUDFLARE_BASE_SUFFIX = "/ai/v1"
const CLOUDFLARE_MODEL = "@cf/qwen/qwen3-30b-a3b-fp8"
const CLOUDFLARE_TOKENS_URL = "https://dash.cloudflare.com/profile/api-tokens"

function isCloudflareUrl(url: string): boolean {
  return url.startsWith(CLOUDFLARE_ACCOUNTS_PREFIX) && url.endsWith(CLOUDFLARE_BASE_SUFFIX)
}

/** The Cloudflare Account ID inside a saved base_url, if any. */
function cloudflareAccountId(url: string): string | null {
  if (!isCloudflareUrl(url)) return null
  return url.slice(CLOUDFLARE_ACCOUNTS_PREFIX.length, url.length - CLOUDFLARE_BASE_SUFFIX.length)
}

function cloudflareBaseUrl(accountId: string): string {
  return `${CLOUDFLARE_ACCOUNTS_PREFIX}${accountId}${CLOUDFLARE_BASE_SUFFIX}`
}

const OPENAI_KEY_ERROR_ID = "summary-openai-key-error"

/**
 * Asks once before any transcript goes to CatchM Cloud, and saves the
 * answer (summary.cloud_consent). With `select`, accepting also makes
 * CatchM Cloud the summary provider. Used by Settings → Summaries and by
 * the meeting page when a summary is refused for missing consent.
 * Focus moves into the dialog and returns on close (ConfirmDialog).
 */
export function CloudConsentDialog({
  open,
  onOpenChange,
  onAccepted,
  select = false,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onAccepted: (config: AppConfig) => void
  select?: boolean
}) {
  const { t } = useTranslation()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const accept = async () => {
    if (pending) return
    setPending(true)
    setError(null)
    try {
      const c = await updateConfig({
        summary: select ? { provider: "catchm-cloud", cloud_consent: true } : { cloud_consent: true },
      })
      setPending(false)
      onAccepted(c)
      onOpenChange(false)
      setError(null)
    } catch (err) {
      setPending(false)
      setError(t("settings.summaries.consentSaveError", { error: errorMessage(err) }))
    }
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(next) => {
        // A failure from last time shouldn't greet the next opening.
        if (!next) setError(null)
        onOpenChange(next)
      }}
      title={t("settings.summaries.consentTitle")}
      description={
        <>
          <span className="block font-medium text-foreground">{t("settings.summaries.cloudPrivacy")}</span>
          <span className="mt-2 block">{t("settings.summaries.consentBody")}</span>
        </>
      }
      confirmLabel={pending ? t("settings.pieces.saving") : t("settings.summaries.consentAccept")}
      cancelLabel={t("settings.summaries.consentCancel")}
      pending={pending}
      error={error}
      onConfirm={() => void accept()}
    />
  )
}

interface PullState {
  running: boolean
  percent: number | null
  statusText: string | null
  error: string | null
}

const idlePullState: PullState = { running: false, percent: null, statusText: null, error: null }

/** Green "Ready" / red "Not ready" / pulsing "Checking…" badge. */
function ReadyBadge({ ready, children }: { ready: boolean | null; children?: ReactNode }) {
  const { t } = useTranslation()
  if (ready === null) {
    return (
      <Badge variant="outline" className="motion-safe:animate-pulse">
        {t("settings.system.checking")}
      </Badge>
    )
  }
  return (
    <Badge
      variant={ready ? "secondary" : "destructive"}
      className={ready ? "border-transparent bg-primary/15 text-primary" : undefined}
    >
      {children ?? (ready ? t("getStarted.ready") : t("settings.summaries.notReady"))}
    </Badge>
  )
}

export function SettingsSummaries() {
  const { t } = useTranslation()
  const [config, setConfig] = useState<AppConfig | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  // CatchM Cloud chosen but not yet agreed to: shown as selected while
  // the consent dialog is open, and dropped again if the user cancels.
  const [pendingProvider, setPendingProvider] = useState<SummaryProvider | null>(null)

  const [modelChoice, setModelChoice] = useState("")
  const [customModel, setCustomModel] = useState("")
  const [customModelError, setCustomModelError] = useState<string | null>(null)
  const [pull, setPull] = useState<PullState>(idlePullState)
  const abortRef = useRef<AbortController | null>(null)

  const [baseUrl, setBaseUrl] = useState("")
  const [openaiModel, setOpenaiModel] = useState("")

  const [cloudflareAccount, setCloudflareAccount] = useState("")
  const [cloudflareError, setCloudflareError] = useState<string | null>(null)
  const [keyInput, setKeyInput] = useState("")
  const [keyEditing, setKeyEditing] = useState(false)
  const [keySaving, setKeySaving] = useState(false)
  const [keyError, setKeyError] = useState<string | null>(null)
  const keyInputRef = useRef<HTMLInputElement | null>(null)
  const keyButtonRef = useRef<HTMLButtonElement | null>(null)
  const restoreKeyFocus = useRef(false)

  const load = useCallback(() => {
    getConfig()
      .then((c) => {
        setConfig(c)
        const preset = MODEL_PRESETS.some((p) => p.model === c.summary.ollama_model)
        setModelChoice(preset ? c.summary.ollama_model : CUSTOM_MODEL)
        setCustomModel(preset ? "" : c.summary.ollama_model)
        setBaseUrl(c.summary.openai.base_url)
        setOpenaiModel(c.summary.openai.model)
        setCloudflareAccount(cloudflareAccountId(c.summary.openai.base_url) ?? "")
      })
      .catch((err: unknown) => setLoadError(errorMessage(err)))
  }, [])

  useEffect(load, [load])

  const retryLoad = () => {
    setLoadError(null)
    load()
  }

  // While "On this computer" still has a step to do, re-check every few
  // seconds (and on focus), so installing or opening Ollama in another
  // window shows up here without "Check again". Saves and a finished
  // download re-check at once.
  const {
    data: status,
    error: statusError,
    checking,
    refresh: refreshStatus,
  } = useStatusCheck(getSummaryStatus, {
    pollMs: SETUP_POLL_MS,
    pollWhile: (s) => config?.summary.provider === "ollama" && !(s?.provider === "ollama" && s.ready),
  })
  // Which translation models are already downloaded, for the total size.
  const { data: translate } = useStatusCheck(getTranslateStatus)

  useEffect(() => () => abortRef.current?.abort(), [])

  useEffect(() => {
    if (!keyEditing && restoreKeyFocus.current) {
      restoreKeyFocus.current = false
      keyButtonRef.current?.focus()
    }
  }, [keyEditing])

  // One save state per area, so each status shows next to its own control.
  const providerSave = useConfigSave(config, setConfig)
  const modelSave = useConfigSave(config, setConfig)
  const openaiSave = useConfigSave(config, setConfig)

  const chooseProvider = (provider: SummaryProvider) => {
    if (!config || provider === config.summary.provider) {
      setPendingProvider(null)
      return
    }
    if (provider === "catchm-cloud" && !config.summary.cloud_consent) {
      setPendingProvider(provider)
      return
    }
    setPendingProvider(null)
    void providerSave.put({ summary: { provider } }).then((ok) => ok && refreshStatus())
  }

  const onConsentAccepted = (c: AppConfig) => {
    setConfig(c)
    setPendingProvider(null)
    refreshStatus()
  }

  const saveOllamaModel = (model: string) => {
    if (!model || model === config?.summary.ollama_model) return
    setPull(idlePullState)
    void modelSave.put({ summary: { ollama_model: model } }).then((ok) => ok && refreshStatus())
  }

  const chooseModel = (value: string) => {
    setModelChoice(value)
    setCustomModelError(null)
    if (value !== CUSTOM_MODEL) saveOllamaModel(value)
  }

  const commitCustomModel = () => {
    const v = customModel.trim()
    if (!v) {
      setCustomModelError(t("settings.summaries.customModelRequired"))
      return
    }
    setCustomModelError(null)
    saveOllamaModel(v)
  }

  const handlePull = async () => {
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    setPull({ running: true, percent: null, statusText: t("settings.translation.pullingStarting"), error: null })
    let failed = false
    try {
      await pullTranslateModel(
        "summary",
        (event) => {
          if (event.error) {
            failed = true
            setPull((prev) => ({ ...prev, running: false, error: event.error ?? null }))
            return
          }
          if (!event.status) return
          // Plain progress instead of Ollama's own status lines.
          const match = PERCENT_RE.exec(event.status)
          setPull((prev) => {
            const percent = match ? Number(match[1]) : prev.percent
            return {
              running: true,
              percent,
              statusText: match
                ? t("settings.translation.pullingPercent", { percent: percent ?? 0 })
                : percent !== null
                  ? t("settings.translation.pullingFinishing")
                  : t("settings.translation.pullingPreparing"),
              error: null,
            }
          })
        },
        controller.signal,
      )
      if (!failed) setPull((prev) => ({ ...prev, running: false, statusText: t("settings.translation.pullingDone") }))
      refreshStatus()
    } catch (err) {
      if (controller.signal.aborted) {
        setPull({ running: false, percent: null, statusText: t("updateBanner.downloadCancelled"), error: null })
      } else {
        setPull((prev) => ({ ...prev, running: false, error: errorMessage(err) }))
      }
    } finally {
      if (abortRef.current === controller) abortRef.current = null
    }
  }

  /** Saving the Cloudflare Account ID rewrites the base URL (and the
   *  model, the first time / when still blank). */
  const commitCloudflareAccount = () => {
    const id = cloudflareAccount.trim()
    if (!id) {
      setCloudflareError(t("settings.summaries.cloudflareAccountRequired"))
      return
    }
    setCloudflareError(null)
    const url = cloudflareBaseUrl(id)
    setBaseUrl(url)
    // Keep a model already chosen for Workers AI; anything else (a model
    // from an earlier custom service) gives way to the default.
    const onCloudflare = isCloudflareUrl(config?.summary.openai.base_url ?? "")
    const model = onCloudflare && openaiModel.trim() ? openaiModel.trim() : CLOUDFLARE_MODEL
    if (model !== CLOUDFLARE_MODEL) setOpenaiModel(model)
    void openaiSave
      .put({ summary: { openai: { base_url: url, model } } })
      .then((ok) => ok && refreshStatus())
  }


  const commitOpenaiModel = () => {
    const v = openaiModel.trim()
    if (v && v !== config?.summary.openai.model) {
      void openaiSave.put({ summary: { openai: { model: v } } }).then((ok) => ok && refreshStatus())
    }
  }

  const startReplaceKey = () => {
    setKeyInput("")
    setKeyError(null)
    setKeyEditing(true)
  }

  const closeKeyEditor = () => {
    restoreKeyFocus.current = true
    setKeyEditing(false)
    setKeyInput("")
    setKeyError(null)
  }

  const saveKey = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (keySaving) return
    const key = keyInput.trim()
    if (!key) {
      setKeyError(t("settings.transcription.enterKey"))
      keyInputRef.current?.focus()
      return
    }
    setKeySaving(true)
    setKeyError(null)
    const ok = await openaiSave.put({ summary: { openai: { api_key: key } } })
    setKeySaving(false)
    if (ok) {
      closeKeyEditor()
      refreshStatus()
    } else {
      setKeyError(t("settings.transcription.keySaveError"))
      keyInputRef.current?.focus()
    }
  }

  // Spoken only when the checklist moves on, not on every re-check.
  const setupAnnouncement = useAnnounceChange(
    setupProgressText(status && status.provider === config?.summary.provider ? status : null),
  )

  if (loadError) {
    return (
      <SettingsSection title={t("settings.nav.summaries")} description={t("settings.summaries.desc")}>
        <div className="grid justify-items-start gap-3">
          <StatusMessage tone="error">
            {t("settings.summaries.loadError", { error: loadError })}
          </StatusMessage>
          <Button size="sm" variant="outline" onClick={retryLoad}>
            {t("common.tryAgain")}
          </Button>
        </div>
      </SettingsSection>
    )
  }

  if (!config) {
    return (
      <div aria-busy="true" className="grid gap-4">
        <span className="sr-only">{t("settings.summaries.loading")}</span>
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-20 w-full rounded-xl" />
        <Skeleton className="h-20 w-full rounded-xl" />
        <Skeleton className="h-20 w-full rounded-xl" />
      </div>
    )
  }

  const provider: SummaryProvider = config.summary.provider
  const shown = pendingProvider ?? provider
  // Status for an older provider (a save just switched it) isn't shown.
  const current = status && status.provider === provider ? status : null
  const ready = current ? current.ready : null

  const readiness = (
    <SettingRow
      label={t("settings.summaries.statusLabel")}
      id="summary-status"
      description={
        current
          ? current.ready
            ? t("settings.summaries.statusReady", { create: t("meetingDetail.createSummary") })
            : (current.reason ?? t("settings.summaries.notReadyYet"))
          : t("settings.system.checking")
      }
      control={
        <div className="grid justify-items-end gap-2">
          <ReadyBadge ready={ready} />
          <Button size="sm" variant="outline" onClick={refreshStatus} disabled={checking}>
            {checking ? t("settings.system.checking") : t("settings.translation.checkAgain")}
          </Button>
        </div>
      }
    />
  )

  const ollama = current?.ollama
  // The system block doesn't depend on the provider, so any status will do.
  const system = status?.system
  const totalRamGb = system?.total_ram_gb ?? null
  const freeDiskGb = system?.free_disk_gb ?? null
  const recommended = totalRamGb == null ? null : recommendedPreset(totalRamGb)
  const selectedModel = ollama?.model ?? config.summary.ollama_model
  const selectedGb = modelDownloadGb(selectedModel)

  const ollamaFound = ollama ? ollama.reachable : null
  const modelDownloaded = ollama ? ollama.installed : null
  const allReady = current ? current.ready : null
  const diskShortfall =
    ollama != null &&
    !ollama.installed &&
    selectedGb != null &&
    freeDiskGb != null &&
    gbToGib(selectedGb) + DISK_MARGIN_GIB > freeDiskGb
      ? { need: selectedGb, free: freeDiskGb }
      : null

  const computerParts = [
    totalRamGb != null && t("settings.summaries.computerRam", { gb: Math.round(totalRamGb) }),
    freeDiskGb != null && t("settings.summaries.computerDisk", { size: formatGb(freeDiskGb) }),
  ].filter(Boolean)

  const translationModels = translate ? installedModels([translate.live_model, translate.batch_model]) : []
  const translationGb = translationModels
    .filter((name) => name !== selectedModel)
    .reduce((sum, name) => sum + (modelDownloadGb(name) ?? 0), 0)
  const totalGb = selectedGb != null && translationGb > 0 ? selectedGb + translationGb : null

  const ollamaDetails = (
    <SettingsSection
      title={t("settings.summaries.ollamaTitle")}
      description={t("settings.summaries.ollamaDesc")}
    >
      <StatusMessage className="sr-only">{setupAnnouncement}</StatusMessage>
      <ol className="grid">
        <SetupStep n={1} title={t("settings.summaries.step1Title")} done={ollamaFound}>
          {ollamaFound ? (
            <p className="text-sm text-muted-foreground">{t("settings.summaries.step1Done")}</p>
          ) : (
            <div className="grid gap-1 text-sm text-muted-foreground">
              <p>
                <Trans
                  i18nKey="settings.summaries.step1Download"
                  components={{ ext: <ExternalLink href={OLLAMA_DOWNLOAD}>{null}</ExternalLink> }}
                />
              </p>
              {ollamaFound === false && <p>{t("settings.summaries.step1OpenOnce")}</p>}
            </div>
          )}
        </SetupStep>

        <SetupStep n={2} title={t("settings.summaries.step2Title")} done={modelDownloaded}>
          <fieldset aria-describedby="summary-model-help">
            <legend className="text-sm font-medium">{t("settings.summaries.modelLegend")}</legend>
            <p id="summary-model-help" className="mt-0.5 text-sm text-muted-foreground">
              {t("settings.summaries.modelHelp")}
              {computerParts.length > 0 &&
                ` ${t("settings.summaries.thisComputer", { parts: computerParts.join(", ") })}`}
            </p>
            {totalRamGb != null && !recommended && (
              <p className="mt-2 flex items-start gap-1.5 text-sm font-medium text-warning">
                <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
                {t("settings.summaries.lowMemoryWarning")}
              </p>
            )}
            <div className="mt-3 grid gap-2">
              {MODEL_PRESETS.map((p) => {
                const gb = modelDownloadGb(p.model)
                const tooBig = totalRamGb != null && !fitsMemory(p, totalRamGb)
                return (
                  <ChoiceCard
                    key={p.model}
                    name="summary-model"
                    value={p.model}
                    checked={modelChoice === p.model}
                    onChange={chooseModel}
                    title={
                      <>
                        {t(totalRamGb == null ? p.labelKey : p.plainLabelKey)}{" "}
                        <span translate="no" className="font-normal text-muted-foreground">
                          · {p.model}
                        </span>
                        {recommended === p && (
                          <Badge variant="secondary" className="ml-2 border-transparent bg-primary/15 align-middle text-primary">
                            {t("settings.summaries.recommendedHere")}
                          </Badge>
                        )}
                      </>
                    }
                    description={`${gb != null ? t("settings.summaries.downloadSize", { size: formatGb(gb) }) : ""}${t(p.needsKey)}`}
                    extra={
                      tooBig ? (
                        <span className="mt-1 flex items-start gap-1.5 font-medium text-warning">
                          <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
                          {t("settings.summaries.presetTooBig", { gb: Math.round(totalRamGb) })}
                        </span>
                      ) : undefined
                    }
                  />
                )
              })}
              <ChoiceCard
                name="summary-model"
                value={CUSTOM_MODEL}
                checked={modelChoice === CUSTOM_MODEL}
                onChange={chooseModel}
                title={t("settings.summaries.customModelTitle")}
                description={t("settings.summaries.customModelDesc")}
              />
            </div>
            {modelChoice === CUSTOM_MODEL && (
              <div className="mt-3 grid gap-1">
                <label htmlFor="summary-custom-model" className="text-sm font-medium">
                  {t("settings.summaries.modelNameLabel")}
                </label>
                <Input
                  id="summary-custom-model"
                  name="summary_ollama_model"
                  translate="no"
                  {...noAutofill}
                  value={customModel}
                  onChange={(e) => setCustomModel(e.target.value)}
                  onBlur={commitCustomModel}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") commitCustomModel()
                  }}
                  placeholder="qwen3.5:9b…"
                  aria-invalid={customModelError ? true : undefined}
                  aria-describedby={customModelError ? "summary-custom-model-error" : undefined}
                  className="w-72"
                />
                <StatusMessage id="summary-custom-model-error" tone="error" className="break-words">
                  {customModelError}
                </StatusMessage>
              </div>
            )}
            <SaveStatus state={modelSave.save.state} error={modelSave.save.error} className="mt-2" />
          </fieldset>

          <p className="mt-3 text-sm text-muted-foreground">
            {t("settings.summaries.separateFromTranslation")}
            {translationModels.length > 0 && (
              <>
                {" "}
                {t("settings.summaries.alreadyForTranslation")}{" "}
                {translationModels.map((name, i) => {
                  const gb = modelDownloadGb(name)
                  return (
                    <span key={name}>
                      {i > 0 && ", "}
                      <span translate="no">{name}</span>
                      {gb != null && ` (${formatGb(gb)})`}
                    </span>
                  )
                })}
                .
              </>
            )}
            {totalGb != null && ` ${t("settings.summaries.totalDiskUse", { size: formatGb(totalGb) })}`}
          </p>

          <div className="mt-3 flex flex-wrap items-start justify-between gap-3">
            <p id="summary-model-download" className="min-w-0 text-sm">
              <span className="font-medium">{t("settings.summaries.selectedLabel")} </span>
              <span translate="no" className="break-all">
                {selectedModel}
              </span>
              {selectedGb != null && <span className="text-muted-foreground">{` · ${formatGb(selectedGb)}`}</span>}
              {modelDownloaded === true && <span className="text-muted-foreground"> · {t("settings.translation.downloaded")}</span>}
              {ollamaFound === false && (
                <span className="block text-muted-foreground">{t("settings.summaries.finishStep1First")}</span>
              )}
            </p>
            {ollama && !ollama.installed && (
              <div className="grid w-72 justify-items-end gap-2">
                {pull.running ? (
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => abortRef.current?.abort()}
                    aria-describedby="summary-model-download"
                  >
                    {t("updateBanner.cancelDownload")}
                  </Button>
                ) : (
                  <Button
                    size="sm"
                    onClick={() => void handlePull()}
                    disabled={!ollama.reachable}
                    aria-describedby={
                      diskShortfall ? "summary-model-download summary-disk-warning" : "summary-model-download"
                    }
                  >
                    {t("settings.translation.downloadModel")}
                  </Button>
                )}
                {pull.running && pull.percent !== null && (
                  <Progress value={pull.percent} aria-label={t("settings.summaries.downloadProgressAria")} />
                )}
              </div>
            )}
          </div>
          {diskShortfall && (
            <p
              id="summary-disk-warning"
              className="mt-2 flex items-start gap-1.5 text-sm font-medium text-warning"
            >
              <TriangleAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
              {t("settings.summaries.diskShortfall", { need: formatGb(diskShortfall.need), free: formatGb(diskShortfall.free) })}
            </p>
          )}
          <div className="mt-1 grid justify-items-end">
            <StatusMessage className="text-xs break-words">{pull.statusText}</StatusMessage>
            <StatusMessage tone="error" className="text-xs break-words">
              {pull.error && t("settings.translation.pullErrorHint", { error: pull.error })}
            </StatusMessage>
          </div>
        </SetupStep>

        <SetupStep n={3} title={t("getStarted.ready")} done={allReady}>
          <p className="text-sm text-muted-foreground">
            {allReady
              ? t("settings.summaries.step3Done", { create: t("meetingDetail.createSummary") })
              : ollama?.reachable && ollama.installed && current?.reason
                ? current.reason
                : t("settings.summaries.step3Waiting", { create: t("meetingDetail.createSummary") })}
          </p>
        </SetupStep>
      </ol>
    </SettingsSection>
  )

  const cloud = current?.cloud
  const quotaText = (() => {
    if (!cloud) return t("settings.system.checking")
    if (!cloud.logged_in) return t("settings.summaries.quotaLogin")
    if (cloud.remaining_today == null || cloud.limit == null) {
      return t("settings.summaries.quotaCheckError")
    }
    const resets = cloud.reset_at ? ` ${t("settings.summaries.quotaResets", { date: formatDateTime(cloud.reset_at) })}` : ""
    return t("settings.summaries.quotaLeft", { remaining: cloud.remaining_today, limit: cloud.limit }) + resets
  })()

  const cloudDetails = (
    <SettingsSection title={t("settings.summaries.cloudTitle")} description={t("settings.summaries.cloudDesc")}>
      <div
        id="summary-cloud-privacy"
        className="mb-2 flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm text-foreground"
      >
        <ShieldAlert aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-warning" />
        <p>
          <span className="font-medium">{t("settings.summaries.cloudPrivacy")}</span>{" "}
          {t("settings.summaries.staysEncrypted")}
        </p>
      </div>
      <SettingRow
        label={t("settings.summaries.cloudAccountLabel")}
        id="summary-cloud-account"
        description={
          cloud
            ? cloud.logged_in
              ? t("settings.summaries.youreLoggedIn")
              : t("settings.summaries.cloudAccountLogin")
            : t("settings.system.checking")
        }
        control={
          cloud && !cloud.logged_in ? (
            <Button
              size="sm"
              variant="outline"
              onClick={() => navigate({ name: "settings", section: "cloud" })}
              aria-describedby={descId("summary-cloud-account")}
            >
              {t("settings.summaries.openCloudSync")}
            </Button>
          ) : (
            <ReadyBadge ready={cloud ? cloud.logged_in : null}>{cloud && t("settings.cloudSync.loggedIn")}</ReadyBadge>
          )
        }
      />
      <SettingRow label={t("settings.summaries.quotaLabel")} id="summary-cloud-quota" description={quotaText} control={null} />
    </SettingsSection>
  )

  const keySet = config.summary_openai_api_key_set
  const openaiDetails = (
    <SettingsSection
      title={t("settings.summaries.providerCloudflare")}
      description={t("settings.summaries.cloudflareDesc")}
    >
      {baseUrl && !isCloudflareUrl(baseUrl) && (
        <p className="py-3 text-sm text-muted-foreground">
          {t("settings.summaries.wasOtherService")} (<span translate="no" className="break-all">{baseUrl}</span>).{" "}
          {t("settings.summaries.switchToCloudflare")}
        </p>
      )}
      <SettingRow
        label={t("settings.summaries.accountIdLabel")}
        htmlFor="summary-cloudflare-account"
        description={
          <Trans
            i18nKey="settings.summaries.accountIdDesc"
            components={{ ext: <ExternalLink href={CLOUDFLARE_TOKENS_URL}>{null}</ExternalLink> }}
          />
        }
        control={
          <div className="grid w-72 justify-items-end gap-1">
            <Input
              id="summary-cloudflare-account"
              name="summary_cloudflare_account"
              {...noAutofill}
              value={cloudflareAccount}
              onChange={(e) => setCloudflareAccount(e.target.value)}
              onBlur={commitCloudflareAccount}
              onKeyDown={(e) => {
                if (e.key === "Enter") commitCloudflareAccount()
              }}
              placeholder="e.g. 1234abcd…"
              aria-invalid={cloudflareError ? true : undefined}
              aria-describedby={
                cloudflareError
                  ? `${descId("summary-cloudflare-account")} summary-cloudflare-error`
                  : descId("summary-cloudflare-account")
              }
              className="w-full"
            />
            <StatusMessage id="summary-cloudflare-error" tone="error">
              {cloudflareError}
            </StatusMessage>
          </div>
        }
      />
      <SettingRow
        label={t("settings.summaries.apiTokenLabel")}
        htmlFor={keyEditing ? "summary-openai-key" : undefined}
        id="summary-openai-key"
        description={t("settings.summaries.apiTokenDesc")}
        control={
          keyEditing ? (
            <form noValidate onSubmit={(e) => void saveKey(e)} className="grid w-72 justify-items-end gap-1.5">
              <Input
                ref={keyInputRef}
                id="summary-openai-key"
                name="summary_openai_api_key"
                type="password"
                {...noAutofill}
                value={keyInput}
                onChange={(e) => setKeyInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Escape" && !keySaving) closeKeyEditor()
                }}
                placeholder={t("settings.summaries.newTokenPlaceholder")}
                readOnly={keySaving}
                aria-invalid={keyError ? true : undefined}
                aria-describedby={
                  keyError ? `${descId("summary-openai-key")} ${OPENAI_KEY_ERROR_ID}` : descId("summary-openai-key")
                }
                autoFocus
              />
              <div className="flex gap-2">
                <Button type="button" size="sm" variant="outline" disabled={keySaving} onClick={closeKeyEditor}>
                  {t("common.cancel")}
                </Button>
                <Button type="submit" size="sm" disabled={keySaving} aria-busy={keySaving || undefined}>
                  {keySaving ? t("settings.pieces.saving") : t("settings.transcription.saveKey")}
                </Button>
              </div>
              <StatusMessage id={OPENAI_KEY_ERROR_ID} tone="error">
                {keyError}
              </StatusMessage>
            </form>
          ) : (
            <div className="flex items-center gap-2">
              {keySet && (
                <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
                  <span aria-hidden="true" className="size-2 rounded-full bg-primary" />
                  {t("settings.summaries.tokenSet")}
                </span>
              )}
              <Button
                ref={keyButtonRef}
                size="sm"
                variant="outline"
                onClick={startReplaceKey}
                aria-describedby={descId("summary-openai-key")}
              >
                {keySet ? t("settings.summaries.replaceToken") : t("settings.summaries.addToken")}
              </Button>
            </div>
          )
        }
      />
      <Disclosure label={t("settings.summaries.modelOptional")}>

        <SettingRow
          label={t("settings.summaries.modelLegend")}
          htmlFor="summary-openai-model"
          description={t("settings.summaries.workersModelDesc", { model: CLOUDFLARE_MODEL })}
          control={
            <Input
              id="summary-openai-model"
              name="summary_openai_model"
              translate="no"
              {...noAutofill}
              value={openaiModel}
              onChange={(e) => setOpenaiModel(e.target.value)}
              onBlur={commitOpenaiModel}
              placeholder={CLOUDFLARE_MODEL}
              aria-describedby={descId("summary-openai-model")}
              className="w-72"
            />
          }
        />
      </Disclosure>
      <p className="py-3 text-sm text-muted-foreground">
        {t("settings.summaries.workersFreeAllowance")}
      </p>
      <div className="flex items-center justify-end">
        <SaveStatus state={openaiSave.save.state} error={openaiSave.save.error} />
      </div>
    </SettingsSection>
  )


  return (
    <div>
      <SettingsSection
        title={t("settings.nav.summaries")}
        description={t("settings.summaries.topDesc")}
      >
        <fieldset className="border-b border-border pb-4" aria-describedby="summary-provider-help">
          <legend className="text-sm font-medium">{t("settings.summaries.writeSummariesWith")}</legend>
          <p id="summary-provider-help" className="sr-only">
            {t("settings.summaries.providerHelp")}
          </p>
          <div className="mt-2 grid gap-2">
            {PROVIDERS.map((p) => (
              <ChoiceCard
                key={p.value}
                name="summary-provider"
                value={p.value}
                checked={shown === p.value}
                onChange={(v) => chooseProvider(v as SummaryProvider)}
                title={t(PROVIDER_LABEL_KEYS[p.value])}
                description={t(PROVIDER_DESC_KEYS[p.value])}
                extra={
                  p.value === "catchm-cloud" ? (
                    <span className="mt-1 block font-medium text-foreground">{t("settings.summaries.cloudPrivacy")}</span>
                  ) : undefined
                }
              />
            ))}
          </div>
          <SaveStatus state={providerSave.save.state} error={providerSave.save.error} className="mt-2" />
        </fieldset>
        <StatusMessage tone="error" className="mt-3 break-words">
          {statusError && t("settings.summaries.statusCheckError", { error: statusError, check: t("settings.translation.checkAgain") })}
        </StatusMessage>
        {readiness}
      </SettingsSection>

      {provider === "ollama" && ollamaDetails}
      {provider === "catchm-cloud" && cloudDetails}
      {provider === "openai" && openaiDetails}

      <CloudConsentDialog
        open={pendingProvider === "catchm-cloud"}
        onOpenChange={(open) => {
          if (!open) setPendingProvider(null)
        }}
        onAccepted={onConsentAccepted}
        select
      />
    </div>
  )
}

/**
 * One numbered step of the "On this computer" checklist. The number
 * turns into a ✓ when done, and the state is also written out ("Done",
 * "To do", "Checking…") inside the heading, so it isn't colour- or
 * icon-only.
 */
function SetupStep({
  n,
  title,
  done,
  children,
}: {
  n: number
  title: string
  done: boolean | null
  children: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <li className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 border-b border-border py-4 last:border-b-0">
      <span
        aria-hidden="true"
        className={cn(
          "flex size-7 items-center justify-center rounded-full border text-sm font-semibold",
          done ? "border-transparent bg-primary text-primary-foreground" : "border-border text-muted-foreground",
        )}
      >
        {done ? <Check className="size-4" /> : n}
      </span>
      <div className="min-w-0">
        <h3 className="mb-1 flex flex-wrap items-center gap-2 text-sm font-medium">
          <span>
            <span className="sr-only">{t("settings.summaries.stepAria", { n })}</span>
            {title}
          </span>
          <span className="sr-only">, </span>
          {done === null ? (
            <Badge variant="outline" className="motion-safe:animate-pulse">
              {t("settings.system.checking")}
            </Badge>
          ) : done ? (
            <Badge variant="secondary" className="border-transparent bg-primary/15 text-primary">
              {t("settings.summaries.doneBadge")}
            </Badge>
          ) : (
            <Badge variant="outline">{t("settings.summaries.todoBadge")}</Badge>
          )}
        </h3>
        {children}
      </div>
    </li>
  )
}

/**
 * A radio option drawn as a card: the whole card is the label, so
 * clicking anywhere selects it, and arrow keys move between options
 * like any native radio group. The description is the radio's
 * accessible description.
 */
function ChoiceCard({
  name,
  value,
  checked,
  onChange,
  title,
  description,
  extra,
}: {
  name: string
  value: string
  checked: boolean
  onChange: (value: string) => void
  title: ReactNode
  description: string
  extra?: ReactNode
}) {
  const id = `${name}-${value.replace(/[^a-z0-9-]/gi, "-")}`
  return (
    <label
      htmlFor={id}
      className={cn(
        "flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring",
        checked ? "border-primary bg-primary/5" : "border-border hover:bg-muted/50",
      )}
    >
      <input
        id={id}
        type="radio"
        name={name}
        value={value}
        checked={checked}
        onChange={() => onChange(value)}
        aria-describedby={`${id}-desc`}
        className="mt-1 size-4 shrink-0 accent-primary"
      />
      <span className="min-w-0">
        <span className="block text-sm font-medium">{title}</span>
        <span id={`${id}-desc`} className="block text-sm text-muted-foreground">
          {description}
          {extra}
        </span>
      </span>
    </label>
  )
}
