import { useEffect, useState, type FormEvent } from "react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { getConfig, updateConfig, type SttEngine } from "@/lib/api"

export function SettingsConfig() {
  const [engine, setEngine] = useState<SttEngine>("whisper")
  const [whisperUrl, setWhisperUrl] = useState("")
  const [sonioxUrl, setSonioxUrl] = useState("")
  const [sonioxApiKey, setSonioxApiKey] = useState("")
  const [sonioxKeySet, setSonioxKeySet] = useState(false)
  const [translateHost, setTranslateHost] = useState("")
  const [liveModel, setLiveModel] = useState("")
  const [batchModel, setBatchModel] = useState("")

  const [loaded, setLoaded] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [savedStatus, setSavedStatus] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)

  useEffect(() => {
    getConfig()
      .then((c) => {
        setEngine(c.engine === "soniox" ? "soniox" : "whisper")
        setWhisperUrl(c.whisper_url)
        setSonioxUrl(c.soniox.url)
        setSonioxKeySet(c.soniox_api_key_set)
        setTranslateHost(c.translate.host)
        setLiveModel(c.translate.live_model)
        setBatchModel(c.translate.batch_model)
        setLoaded(true)
        setLoadError(null)
      })
      .catch((err: unknown) => setLoadError(err instanceof Error ? err.message : String(err)))
  }, [])

  const handleSave = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setSaving(true)
    setSaveError(null)
    try {
      const c = await updateConfig({
        engine,
        whisper_url: whisperUrl,
        soniox: { url: sonioxUrl },
        soniox_api_key: sonioxApiKey,
        translate: { host: translateHost, live_model: liveModel, batch_model: batchModel },
      })
      setSonioxKeySet(c.soniox_api_key_set)
      setSonioxApiKey("")
      setSavedStatus("Saved")
      setTimeout(() => setSavedStatus(null), 2000)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <h1 className="mb-6 text-2xl font-semibold">Engines</h1>

      {loadError && <p className="mb-4 text-sm text-destructive">{loadError}</p>}

      {loaded && (
        <form onSubmit={handleSave} className="grid gap-4">
          <Card>
            <CardHeader>
              <CardTitle>Speech-to-text</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-3">
              <div className="grid gap-1.5">
                <Label htmlFor="config-engine">Default engine</Label>
                <Select value={engine} onValueChange={(v) => setEngine(v as SttEngine)}>
                  <SelectTrigger id="config-engine" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="whisper">whisper</SelectItem>
                    <SelectItem value="soniox">soniox</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="config-whisper-url">Whisper server URL</Label>
                <Input
                  id="config-whisper-url"
                  value={whisperUrl}
                  onChange={(e) => setWhisperUrl(e.target.value)}
                  placeholder="http://127.0.0.1:8080"
                />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="config-soniox-url">Soniox WebSocket URL</Label>
                <Input
                  id="config-soniox-url"
                  value={sonioxUrl}
                  onChange={(e) => setSonioxUrl(e.target.value)}
                  placeholder="wss://stt-rt.soniox.com/transcribe-websocket"
                />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="config-soniox-key">Soniox API key</Label>
                <Input
                  id="config-soniox-key"
                  type="password"
                  value={sonioxApiKey}
                  onChange={(e) => setSonioxApiKey(e.target.value)}
                  placeholder={sonioxKeySet ? "Currently set" : "Not set"}
                  autoComplete="off"
                />
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Translation (Ollama)</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-3">
              <div className="grid gap-1.5">
                <Label htmlFor="config-translate-host">Host</Label>
                <Input
                  id="config-translate-host"
                  value={translateHost}
                  onChange={(e) => setTranslateHost(e.target.value)}
                  placeholder="http://127.0.0.1:11434"
                />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="config-live-model">Live model</Label>
                <Input
                  id="config-live-model"
                  value={liveModel}
                  onChange={(e) => setLiveModel(e.target.value)}
                  placeholder="translategemma:4b"
                />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="config-batch-model">Batch model</Label>
                <Input
                  id="config-batch-model"
                  value={batchModel}
                  onChange={(e) => setBatchModel(e.target.value)}
                  placeholder="translategemma:12b"
                />
              </div>
            </CardContent>
          </Card>

          <div className="flex items-center gap-3">
            <Button type="submit" className="w-fit" disabled={saving}>
              {saving ? "Saving…" : "Save"}
            </Button>
            {savedStatus && <span className="text-sm text-muted-foreground">{savedStatus}</span>}
          </div>
          {saveError && <p className="text-sm text-destructive">{saveError}</p>}
        </form>
      )}
    </div>
  )
}
