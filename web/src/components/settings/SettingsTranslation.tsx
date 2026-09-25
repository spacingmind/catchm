import { useEffect, useRef, useState, type FormEvent } from "react"

import { SaveStatus, SettingRow, SettingsSection } from "@/components/settings/pieces"
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
import { LANGUAGES } from "@/lib/languages"

function isAbsoluteHttpUrl(v: string): boolean {
  try {
    const u = new URL(v)
    return u.protocol === "http:" || u.protocol === "https:"
  } catch {
    return false
  }
}

interface PullState {
  running: boolean
  percent: number | null
  statusText: string | null
  error: string | null
}

const idlePullState: PullState = { running: false, percent: null, statusText: null, error: null }

const PERCENT_RE = /(\d+)%/

export function SettingsTranslation() {
  const [config, setConfig] = useState<AppConfig | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [host, setHost] = useState("")
  const [hostError, setHostError] = useState<string | null>(null)
  const [liveModel, setLiveModel] = useState("")
  const [batchModel, setBatchModel] = useState("")

  const [status, setStatus] = useState<TranslateStatus | null>(null)
  const [statusError, setStatusError] = useState<string | null>(null)
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

  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    getConfig()
      .then((c) => {
        setConfig(c)
        setHost(c.translate.host)
        setLiveModel(c.translate.live_model)
        setBatchModel(c.translate.batch_model)
      })
      .catch((err: unknown) => setLoadError(err instanceof Error ? err.message : String(err)))
  }, [])

  const refreshStatus = () => {
    getTranslateStatus()
      .then((s) => {
        setStatus(s)
        setStatusError(null)
      })
      .catch((err: unknown) => setStatusError(err instanceof Error ? err.message : String(err)))
  }

  useEffect(() => {
    refreshStatus()
    return () => abortRef.current?.abort()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const { save, put } = useConfigSave(config, setConfig)

  const commitHost = () => {
    const v = host.trim()
    if (!isAbsoluteHttpUrl(v)) {
      setHostError("Enter an absolute http:// or https:// URL.")
      return
    }
    setHostError(null)
    if (v !== config?.translate.host) {
      void put({ translate: { host: v, live_model: liveModel.trim(), batch_model: batchModel.trim() } })
      refreshStatus()
    }
  }

  const commitModel = (which: "live_model" | "batch_model") => {
    const live = liveModel.trim()
    const batch = batchModel.trim()
    const changed =
      which === "live_model" ? live !== config?.translate.live_model : batch !== config?.translate.batch_model
    if (changed) {
      void put({ translate: { host: host.trim(), live_model: live, batch_model: batch } })
      refreshStatus()
    }
  }

  const handlePull = async (target: TranslateTarget) => {
    setPulls((prev) => ({ ...prev, [target]: { running: true, percent: null, statusText: "Starting…", error: null } }))
    const controller = new AbortController()
    abortRef.current = controller
    try {
      await pullTranslateModel(
        target,
        (event) => {
          if (event.error) {
            setPulls((prev) => ({ ...prev, [target]: { ...prev[target], running: false, error: event.error ?? null } }))
            return
          }
          if (!event.status) return
          const match = PERCENT_RE.exec(event.status)
          setPulls((prev) => ({
            ...prev,
            [target]: {
              running: true,
              percent: match ? Number(match[1]) : prev[target].percent,
              statusText: event.status ?? prev[target].statusText,
              error: null,
            },
          }))
        },
        controller.signal,
      )
      setPulls((prev) => ({ ...prev, [target]: { ...prev[target], running: false, statusText: "Done." } }))
      refreshStatus()
    } catch (err) {
      setPulls((prev) => ({
        ...prev,
        [target]: { ...prev[target], running: false, error: err instanceof Error ? err.message : String(err) },
      }))
    }
  }

  const [testFieldError, setTestFieldError] = useState<string | null>(null)

  const handleTest = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (!testText.trim()) {
      setTestFieldError("Enter some text to translate.")
      return
    }
    if (!testLang) {
      setTestFieldError("Choose a target language.")
      return
    }
    setTestFieldError(null)
    setTesting(true)
    setTestError(null)
    setTestResult(null)
    try {
      const res = await testTranslate({ text: testText, target_lang: testLang, target: testTarget })
      setTestResult(res.translated)
    } catch (err) {
      setTestError(err instanceof Error ? err.message : String(err))
    } finally {
      setTesting(false)
    }
  }

  if (loadError) {
    return (
      <SettingsSection title="Translation" description="Ollama settings for live and batch translation.">
        <p className="text-sm text-destructive">{loadError}</p>
      </SettingsSection>
    )
  }

  if (!config) {
    return (
      <div aria-busy="true" className="grid gap-4">
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
        title="Translation"
        description="Ollama translates meetings — live while recording, in batch when refining. Changes save when you leave a field."
      >
        <SettingRow
          label="Ollama host"
          htmlFor="translate-host"
          description="Where your Ollama server listens."
          control={
            <div className="grid w-96 justify-items-end gap-1">
              <Input
                id="translate-host"
                type="url"
                value={host}
                onChange={(e) => setHost(e.target.value)}
                onBlur={commitHost}
                placeholder="http://127.0.0.1:11434"
                aria-invalid={hostError ? true : undefined}
                className="w-full"
              />
              {hostError && <p className="text-sm text-destructive">{hostError}</p>}
            </div>
          }
        />
        <SettingRow
          label="Live model"
          htmlFor="translate-live-model"
          description="Small and fast; used while a meeting is recording."
          control={
            <div className="grid w-72 justify-items-end gap-1">
              <Input
                id="translate-live-model"
                value={liveModel}
                onChange={(e) => setLiveModel(e.target.value)}
                onBlur={() => commitModel("live_model")}
                placeholder="translategemma:4b"
                className="w-full"
              />
            </div>
          }
        />
        <SettingRow
          label="Batch model"
          htmlFor="translate-batch-model"
          description="Higher quality; used when refining a finished meeting."
          control={
            <div className="grid w-72 justify-items-end gap-1">
              <Input
                id="translate-batch-model"
                value={batchModel}
                onChange={(e) => setBatchModel(e.target.value)}
                onBlur={() => commitModel("batch_model")}
                placeholder="translategemma:12b"
                className="w-full"
              />
              <SaveStatus state={save.state} error={save.error} />
            </div>
          }
        />
      </SettingsSection>

      <SettingsSection title="Status" description="Checked against the configured host.">
        {statusError && <p className="mb-4 text-sm text-destructive">{statusError}</p>}
        <SettingRow
          label="Ollama"
          description="Server reachability."
          control={
            status ? (
              <Badge
                variant={status.ollama_reachable ? "secondary" : "destructive"}
                className={
                  status.ollama_reachable ? "border-transparent bg-primary/15 text-primary" : undefined
                }
              >
                {status.ollama_reachable ? "Reachable" : "Unreachable"}
              </Badge>
            ) : (
              <Skeleton className="h-6 w-24 rounded-full" />
            )
          }
        />
        {(["live", "batch"] as const).map((target) => {
          const model = target === "live" ? status?.live_model : status?.batch_model
          const pull = pulls[target]
          return (
            <SettingRow
              key={target}
              label={target === "live" ? "Live model" : "Batch model"}
              description={model?.name ?? "—"}
              control={
                <div className="grid w-72 justify-items-end gap-2">
                  {model ? (
                    <Badge variant={model.installed ? "secondary" : "outline"}>
                      {model.installed ? "Installed" : "Not installed"}
                    </Badge>
                  ) : (
                    <Skeleton className="h-6 w-24 rounded-full" />
                  )}
                  {model && !model.installed && (
                    <div className="grid justify-items-end gap-2">
                      <Button size="sm" onClick={() => void handlePull(target)} disabled={pull.running}>
                        {pull.running ? "Pulling…" : "Pull"}
                      </Button>
                      {pull.running && pull.percent !== null && <Progress value={pull.percent} />}
                      {pull.statusText && <p className="text-xs text-muted-foreground">{pull.statusText}</p>}
                      {pull.error && <p className="text-xs text-destructive">{pull.error}</p>}
                    </div>
                  )}
                </div>
              }
            />
          )
        })}
      </SettingsSection>

      <SettingsSection title="Try it out" description="Translate a snippet to check the setup end to end.">
        <form onSubmit={handleTest} className="grid gap-3">
          <SettingRow
            label="Text"
            htmlFor="test-text"
            control={
              <Input
                id="test-text"
                value={testText}
                onChange={(e) => setTestText(e.target.value)}
                required
                className="w-96"
              />
            }
          />
          <SettingRow
            label="Target language"
            htmlFor="test-lang"
            control={
              <Select value={testLang || undefined} onValueChange={setTestLang}>
                <SelectTrigger id="test-lang" className="w-44">
                  <SelectValue placeholder="Choose…" />
                </SelectTrigger>
                <SelectContent>
                  {LANGUAGES.map((l) => (
                    <SelectItem key={l.code} value={l.code}>
                      {l.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            }
          />
          <SettingRow
            label="Model"
            htmlFor="test-target"
            control={
              <Select value={testTarget} onValueChange={(v) => setTestTarget(v as TranslateTarget)}>
                <SelectTrigger id="test-target" className="w-44">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="live">live</SelectItem>
                  <SelectItem value="batch">batch</SelectItem>
                </SelectContent>
              </Select>
            }
          />
          <div className="flex items-center justify-end">
            <div className="grid justify-items-end gap-1">
              <Button type="submit" disabled={testing}>
                {testing ? "Testing…" : "Test translate"}
              </Button>
              {testFieldError && <p className="text-sm text-destructive">{testFieldError}</p>}
            </div>
          </div>
        </form>
        {testResult && (
          <p className="mt-3 text-sm">
            <span className="text-muted-foreground">→ </span>
            {testResult}
          </p>
        )}
        {testError && <p className="mt-3 text-sm text-destructive">{testError}</p>}
      </SettingsSection>
    </div>
  )
}
