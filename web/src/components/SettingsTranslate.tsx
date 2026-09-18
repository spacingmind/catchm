import { useEffect, useRef, useState, type FormEvent } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  getTranslateStatus,
  pullTranslateModel,
  testTranslate,
  type TranslateStatus,
  type TranslateTarget,
} from "@/lib/api"

interface PullState {
  running: boolean
  percent: number | null
  statusText: string | null
  error: string | null
}

const idlePullState: PullState = { running: false, percent: null, statusText: null, error: null }

const PERCENT_RE = /(\d+)%/

export function SettingsTranslate() {
  const [status, setStatus] = useState<TranslateStatus | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
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

  const refresh = () => {
    getTranslateStatus()
      .then((s) => {
        setStatus(s)
        setLoadError(null)
      })
      .catch((err: unknown) => setLoadError(err instanceof Error ? err.message : String(err)))
  }

  useEffect(() => {
    refresh()
    return () => abortRef.current?.abort()
  }, [])

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
      refresh()
    } catch (err) {
      setPulls((prev) => ({
        ...prev,
        [target]: { ...prev[target], running: false, error: err instanceof Error ? err.message : String(err) },
      }))
    }
  }

  const handleTest = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
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

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <h1 className="mb-6 text-2xl font-semibold">Translation</h1>

      {loadError && <p className="mb-4 text-sm text-destructive">{loadError}</p>}

      {status && (
        <div className="grid gap-4">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <CardTitle>Ollama</CardTitle>
              <Badge
                className={
                  status.ollama_reachable
                    ? "border-transparent bg-green-100 text-green-800"
                    : "border-transparent bg-amber-100 text-amber-800"
                }
              >
                {status.ollama_reachable ? "Reachable" : "Unreachable"}
              </Badge>
            </CardHeader>
          </Card>

          {(["live", "batch"] as const).map((target) => {
            const model = target === "live" ? status.live_model : status.batch_model
            const pull = pulls[target]
            return (
              <Card key={target}>
                <CardHeader className="flex flex-row items-center justify-between">
                  <CardTitle className="capitalize">{target} model</CardTitle>
                  <Badge variant={model.installed ? "secondary" : "outline"}>
                    {model.installed ? "Installed" : "Not installed"}
                  </Badge>
                </CardHeader>
                <CardContent className="grid gap-2">
                  <p className="text-sm text-muted-foreground">{model.name}</p>
                  {!model.installed && (
                    <div className="grid gap-2">
                      <Button size="sm" className="w-fit" onClick={() => handlePull(target)} disabled={pull.running}>
                        {pull.running ? "Pulling…" : "Pull"}
                      </Button>
                      {pull.running && pull.percent !== null && <Progress value={pull.percent} />}
                      {pull.statusText && <p className="text-xs text-muted-foreground">{pull.statusText}</p>}
                      {pull.error && <p className="text-xs text-destructive">{pull.error}</p>}
                    </div>
                  )}
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}

      <Card className="mt-6">
        <CardHeader>
          <CardTitle>Test translate</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleTest} className="grid gap-3">
            <div className="grid gap-1.5">
              <Label htmlFor="test-text">Text</Label>
              <Input id="test-text" value={testText} onChange={(e) => setTestText(e.target.value)} required />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="grid gap-1.5">
                <Label htmlFor="test-lang">Target language</Label>
                <Input
                  id="test-lang"
                  value={testLang}
                  onChange={(e) => setTestLang(e.target.value)}
                  placeholder="es"
                  required
                />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="test-target">Model</Label>
                <Select value={testTarget} onValueChange={(v) => setTestTarget(v as TranslateTarget)}>
                  <SelectTrigger id="test-target" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="live">live</SelectItem>
                    <SelectItem value="batch">batch</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>
            <Button type="submit" disabled={testing || !testText.trim() || !testLang.trim()} className="w-fit">
              {testing ? "Testing…" : "Test translate"}
            </Button>
          </form>
          {testResult && <p className="mt-3 text-sm">{testResult}</p>}
          {testError && <p className="mt-3 text-sm text-destructive">{testError}</p>}
        </CardContent>
      </Card>
    </div>
  )
}
