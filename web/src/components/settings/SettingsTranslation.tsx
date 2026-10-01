import { useCallback, useEffect, useRef, useState, type FormEvent } from "react"
import { Trans, useTranslation } from "react-i18next"

import { StatusMessage } from "@/components/StatusMessage"
import { descId, ExternalLink, SaveStatus, SettingRow, SettingsSection } from "@/components/settings/pieces"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Progress } from "@/components/ui/progress"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import {
  getConfig,
  getTranslateStatus,
  pullTranslateModel,
  testTranslate,
  type AppConfig,
  type TranslateStatus,
  type TranslateTarget,
} from "@/lib/api"
import { noAutofill } from "@/lib/inputProps"
import { LANGUAGES, languageLabel } from "@/lib/languages"
import { isAbsoluteUrl } from "@/lib/url"

interface PullState {
  running: boolean
  percent: number | null
  statusText: string | null
  error: string | null
}

const idlePullState: PullState = { running: false, percent: null, statusText: null, error: null }

const PERCENT_RE = /(\d+)%/

const TARGET_KEY = {
  live: "settings.translation.liveModel",
  batch: "settings.translation.batchModel",
} as const

const OLLAMA_DOWNLOAD = "https://ollama.com/download"

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

export function SettingsTranslation() {
  const { t } = useTranslation()
  const [config, setConfig] = useState<AppConfig | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [host, setHost] = useState("")
  const [hostError, setHostError] = useState<string | null>(null)
  const [liveModel, setLiveModel] = useState("")
  const [batchModel, setBatchModel] = useState("")

  const [status, setStatus] = useState<TranslateStatus | null>(null)
  const [statusError, setStatusError] = useState<string | null>(null)
  const [checking, setChecking] = useState(true)
  const [pulls, setPulls] = useState<Record<TranslateTarget, PullState>>({
    live: idlePullState,
    batch: idlePullState,
  })

  const [testText, setTestText] = useState("")
  const [testLang, setTestLang] = useState("")
  const [testTarget, setTestTarget] = useState<TranslateTarget>("live")
  const [testResult, setTestResult] = useState<string | null>(null)
  const [testError, setTestError] = useState<string | null>(null)
  const [testing, setTesting] = useState(false)
  const [testFieldErrors, setTestFieldErrors] = useState<{ text?: string; lang?: string }>({})
  const testTextRef = useRef<HTMLInputElement | null>(null)
  const testLangRef = useRef<HTMLButtonElement | null>(null)

  // One controller per target so pulling both models at once works and
  // each can be cancelled on its own; all are aborted on unmount.
  const abortRefs = useRef<Partial<Record<TranslateTarget, AbortController>>>({})
  const statusSeq = useRef(0)

  const load = useCallback(() => {
    getConfig()
      .then((c) => {
        setConfig(c)
        setHost(c.translate.host)
        setLiveModel(c.translate.live_model)
        setBatchModel(c.translate.batch_model)
      })
      .catch((err: unknown) => setLoadError(errorMessage(err)))
  }, [])

  useEffect(load, [load])

  const retryLoad = () => {
    setLoadError(null)
    load()
  }

  // Sequence-guarded: a save and a finished pull can both trigger a
  // check, and only the newest response should land.
  const fetchStatus = useCallback(() => {
    const id = ++statusSeq.current
    getTranslateStatus()
      .then((s) => {
        if (id !== statusSeq.current) return
        setStatus(s)
        setStatusError(null)
      })
      .catch((err: unknown) => {
        if (id === statusSeq.current) setStatusError(errorMessage(err))
      })
      .finally(() => {
        if (id === statusSeq.current) setChecking(false)
      })
  }, [])

  const refreshStatus = () => {
    setChecking(true)
    fetchStatus()
  }

  useEffect(() => {
    fetchStatus()
    const controllers = abortRefs.current
    return () => {
      for (const c of Object.values(controllers)) c?.abort()
    }
  }, [fetchStatus])

  // One save state per field, so each status shows next to its own input.
  const hostSave = useConfigSave(config, setConfig)
  const liveSave = useConfigSave(config, setConfig)
  const batchSave = useConfigSave(config, setConfig)

  const commitHost = () => {
    const v = host.trim()
    if (!isAbsoluteUrl(v, ["http", "https"])) {
      setHostError(t("settings.translation.hostError"))
      return
    }
    setHostError(null)
    if (v !== config?.translate.host) {
      void hostSave
        .put({ translate: { host: v, live_model: liveModel.trim(), batch_model: batchModel.trim() } })
        .then((ok) => ok && refreshStatus())
    }
  }

  const commitModel = (which: "live_model" | "batch_model") => {
    const live = liveModel.trim()
    const batch = batchModel.trim()
    const changed =
      which === "live_model" ? live !== config?.translate.live_model : batch !== config?.translate.batch_model
    if (changed) {
      const { put } = which === "live_model" ? liveSave : batchSave
      void put({ translate: { host: host.trim(), live_model: live, batch_model: batch } }).then(
        (ok) => ok && refreshStatus(),
      )
    }
  }

  const handlePull = async (target: TranslateTarget) => {
    setPulls((prev) => ({ ...prev, [target]: { running: true, percent: null, statusText: t("settings.translation.pullingStarting"), error: null } }))
    abortRefs.current[target]?.abort()
    const controller = new AbortController()
    abortRefs.current[target] = controller
    let failed = false
    try {
      await pullTranslateModel(
        target,
        (event) => {
          if (event.error) {
            failed = true
            setPulls((prev) => ({ ...prev, [target]: { ...prev[target], running: false, error: event.error ?? null } }))
            return
          }
          if (!event.status) return
          // Ollama's own status lines ("pulling manifest", "verifying
          // sha256 digest") mean nothing to most people: show plain
          // progress instead.
          const match = PERCENT_RE.exec(event.status)
          setPulls((prev) => {
            const percent = match ? Number(match[1]) : prev[target].percent
            return {
              ...prev,
              [target]: {
                running: true,
                percent,
                statusText: match
                  ? t("settings.translation.pullingPercent", { percent: percent ?? 0 })
                  : percent !== null
                    ? t("settings.translation.pullingFinishing")
                    : t("settings.translation.pullingPreparing"),
                error: null,
              },
            }
          })
        },
        controller.signal,
      )
      if (!failed) {
        setPulls((prev) => ({ ...prev, [target]: { ...prev[target], running: false, statusText: t("settings.translation.pullingDone") } }))
      }
      refreshStatus()
    } catch (err) {
      if (controller.signal.aborted) {
        setPulls((prev) => ({
          ...prev,
          [target]: { running: false, percent: null, statusText: t("updateBanner.downloadCancelled"), error: null },
        }))
      } else {
        setPulls((prev) => ({ ...prev, [target]: { ...prev[target], running: false, error: errorMessage(err) } }))
      }
    } finally {
      if (abortRefs.current[target] === controller) delete abortRefs.current[target]
    }
  }

  const cancelPull = (target: TranslateTarget) => {
    abortRefs.current[target]?.abort()
  }

  const handleTest = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const errs: { text?: string; lang?: string } = {}
    if (!testText.trim()) errs.text = t("settings.translation.testTextRequired")
    if (!testLang) errs.lang = t("settings.translation.testLangRequired")
    setTestFieldErrors(errs)
    if (errs.text) testTextRef.current?.focus()
    else if (errs.lang) testLangRef.current?.focus()
    if (errs.text || errs.lang) return
    setTesting(true)
    setTestError(null)
    setTestResult(null)
    try {
      const res = await testTranslate({ text: testText, target_lang: testLang, target: testTarget })
      setTestResult(res.translated)
    } catch (err) {
      setTestError(errorMessage(err))
    } finally {
      setTesting(false)
    }
  }

  if (loadError) {
    return (
      <SettingsSection title={t("settings.nav.translation")} description={t("settings.translation.desc")}>
        <div className="grid justify-items-start gap-3">
          <StatusMessage tone="error">
            {t("settings.translation.loadError", { error: loadError })}
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
        <span className="sr-only">{t("settings.translation.loading")}</span>
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
      </div>
    )
  }

  return (
    <div>
      <SettingsSection
        title={t("settings.nav.translation")}
        description={
          <Trans
            i18nKey="settings.translation.descOllama"
            components={{ ext: <ExternalLink href={OLLAMA_DOWNLOAD}>{null}</ExternalLink> }}
          />
        }
      >
        <SettingRow
          label={t("settings.translation.hostLabel")}
          htmlFor="translate-host"
          description={t("settings.translation.hostDesc")}
          control={
            <div className="grid w-96 justify-items-end gap-1">
              <Input
                id="translate-host"
                name="ollama_host"
                type="url"
                {...noAutofill}
                value={host}
                onChange={(e) => setHost(e.target.value)}
                onBlur={commitHost}
                placeholder="http://127.0.0.1:11434…"
                aria-invalid={hostError ? true : undefined}
                aria-describedby={
                  hostError ? `${descId("translate-host")} translate-host-error` : descId("translate-host")
                }
                className="w-full"
              />
              <StatusMessage id="translate-host-error" tone="error" className="break-words">
                {hostError}
              </StatusMessage>
              <SaveStatus state={hostSave.save.state} error={hostSave.save.error} />
            </div>
          }
        />
        <SettingRow
          label={t("settings.translation.liveModel")}
          htmlFor="translate-live-model"
          description={t("settings.translation.liveModelDesc")}
          control={
            <div className="grid w-72 justify-items-end gap-1">
              <Input
                id="translate-live-model"
                name="live_model"
                translate="no"
                {...noAutofill}
                value={liveModel}
                onChange={(e) => setLiveModel(e.target.value)}
                onBlur={() => commitModel("live_model")}
                placeholder="translategemma:4b…"
                aria-describedby={descId("translate-live-model")}
                className="w-full"
              />
              <SaveStatus state={liveSave.save.state} error={liveSave.save.error} />
            </div>
          }
        />
        <SettingRow
          label={t("settings.translation.batchModel")}
          htmlFor="translate-batch-model"
          description={t("settings.translation.batchModelDesc")}
          control={
            <div className="grid w-72 justify-items-end gap-1">
              <Input
                id="translate-batch-model"
                name="batch_model"
                translate="no"
                {...noAutofill}
                value={batchModel}
                onChange={(e) => setBatchModel(e.target.value)}
                onBlur={() => commitModel("batch_model")}
                placeholder="translategemma:12b…"
                aria-describedby={descId("translate-batch-model")}
                className="w-full"
              />
              <SaveStatus state={batchSave.save.state} error={batchSave.save.error} />
            </div>
          }
        />
      </SettingsSection>

      <SettingsSection title={t("settings.translation.statusTitle")} description={t("settings.translation.statusDesc")}>
        <div className={statusError ? "mb-4 flex flex-wrap items-center justify-between gap-2" : undefined}>
          <StatusMessage tone="error" className="min-w-0 break-words">
            {statusError && t("settings.translation.statusCheckError", { error: statusError })}
          </StatusMessage>
          {statusError && (
            <Button size="sm" variant="outline" onClick={refreshStatus} disabled={checking}>
              {checking ? t("settings.system.checking") : t("settings.translation.checkAgain")}
            </Button>
          )}
        </div>
        <SettingRow
          label={t("settings.translation.ollamaLabel")}
          id="translate-status-ollama"
          description={
            status && !status.ollama_reachable ? (
              <Trans
                i18nKey="settings.translation.ollamaDown"
                components={{ ext: <ExternalLink href={OLLAMA_DOWNLOAD}>{null}</ExternalLink> }}
              />
            ) : (
              t("settings.translation.ollamaMustRun")
            )
          }
          control={
            status ? (
              <Badge
                variant={status.ollama_reachable ? "secondary" : "destructive"}
                className={
                  status.ollama_reachable ? "border-transparent bg-primary/15 text-primary" : undefined
                }
              >
                {status.ollama_reachable ? t("settings.translation.running") : t("settings.translation.notRunning")}
              </Badge>
            ) : (
              <Badge variant="outline" className="motion-safe:animate-pulse">
                {t("settings.translation.checkingOllama")}
              </Badge>
            )
          }
        />
        {(["live", "batch"] as const).map((target) => {
          const model = target === "live" ? status?.live_model : status?.batch_model
          const pull = pulls[target]
          const label = t(TARGET_KEY[target])
          const rowId = `translate-status-${target}`
          return (
            <SettingRow
              key={target}
              label={label}
              id={rowId}
              description={
                model?.name ? (
                  <span translate="no" className="break-all">
                    {model.name}
                  </span>
                ) : (
                  "—"
                )
              }
              control={
                <div className="grid w-72 justify-items-end gap-2">
                  {model ? (
                    <Badge variant={model.installed ? "secondary" : "outline"}>
                      {model.installed ? t("settings.translation.downloaded") : t("settings.translation.notDownloaded")}
                    </Badge>
                  ) : (
                    <Badge variant="outline" className="motion-safe:animate-pulse">
                      {t("settings.system.checking")}
                    </Badge>
                  )}
                  {model && !model.installed && (
                    <div className="grid w-full justify-items-end gap-2">
                      {pull.running ? (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => cancelPull(target)}
                          aria-describedby={descId(rowId)}
                        >
                          {t("updateBanner.cancelDownload")}
                        </Button>
                      ) : (
                        <Button
                          size="sm"
                          onClick={() => void handlePull(target)}
                          aria-describedby={descId(rowId)}
                        >
                          {t("settings.translation.downloadModel")}
                        </Button>
                      )}
                      {pull.running && pull.percent !== null && (
                        <Progress value={pull.percent} aria-label={t("settings.translation.downloadProgressAria", { model: t(TARGET_KEY[target]) })} />
                      )}
                      <StatusMessage className="text-xs break-words">{pull.statusText}</StatusMessage>
                      <StatusMessage tone="error" className="text-xs break-words">
                        {pull.error && t("settings.translation.pullErrorHint", { error: pull.error })}
                      </StatusMessage>
                    </div>
                  )}
                </div>
              }
            />
          )
        })}
      </SettingsSection>

      <SettingsSection title={t("settings.translation.tryTitle")} description={t("settings.translation.tryDesc")}>
        <form noValidate onSubmit={(e) => void handleTest(e)} className="grid gap-3">
          <SettingRow
            label={t("settings.translation.testTextLabel")}
            htmlFor="test-text"
            control={
              <div className="grid w-96 justify-items-end gap-1">
                <Input
                  ref={testTextRef}
                  id="test-text"
                  name="test_text"
                  autoComplete="off"
                  value={testText}
                  onChange={(e) => setTestText(e.target.value)}
                  placeholder={t("settings.translation.testTextPlaceholder")}
                  aria-invalid={testFieldErrors.text ? true : undefined}
                  aria-describedby={testFieldErrors.text ? "test-text-error" : undefined}
                  className="w-full"
                />
                <StatusMessage id="test-text-error" tone="error">
                  {testFieldErrors.text}
                </StatusMessage>
              </div>
            }
          />
          <SettingRow
            label={t("settings.translation.testLangLabel")}
            htmlFor="test-lang"
            control={
              <div className="grid justify-items-end gap-1">
                <Select
                  value={testLang || undefined}
                  onValueChange={(v) => {
                    setTestLang(v)
                    setTestFieldErrors((e) => ({ ...e, lang: undefined }))
                  }}
                >
                  <SelectTrigger
                    ref={testLangRef}
                    id="test-lang"
                    className="w-44"
                    aria-invalid={testFieldErrors.lang ? true : undefined}
                    aria-describedby={testFieldErrors.lang ? "test-lang-error" : undefined}
                  >
                    <SelectValue placeholder={t("settings.translation.choosePlaceholder")} />
                  </SelectTrigger>
                  <SelectContent>
                    {LANGUAGES.map((l) => (
                      <SelectItem key={l.code} value={l.code}>
                        {languageLabel(l.code)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <StatusMessage id="test-lang-error" tone="error">
                  {testFieldErrors.lang}
                </StatusMessage>
              </div>
            }
          />
          <SettingRow
            label={t("settings.translation.modelLabel")}
            htmlFor="test-target"
            control={
              <Select value={testTarget} onValueChange={(v) => setTestTarget(v as TranslateTarget)}>
                <SelectTrigger id="test-target" className="w-52">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="live">{t(TARGET_KEY.live)}</SelectItem>
                  <SelectItem value="batch">{t(TARGET_KEY.batch)}</SelectItem>
                </SelectContent>
              </Select>
            }
          />
          <div className="flex items-center justify-end">
            <Button type="submit" disabled={testing} aria-busy={testing || undefined}>
              {testing ? t("settings.translation.translating") : t("settings.translation.testButton")}
            </Button>
          </div>
        </form>
        <StatusMessage className="mt-3 break-words text-foreground">
          {testResult && (
            <>
              <span aria-hidden="true" className="text-muted-foreground">
                →{" "}
              </span>
              {testResult}
            </>
          )}
        </StatusMessage>
        <StatusMessage tone="error" className="mt-3 break-words">
          {testError && t("settings.translation.testErrorHint", { error: testError })}
        </StatusMessage>
      </SettingsSection>
    </div>
  )
}
