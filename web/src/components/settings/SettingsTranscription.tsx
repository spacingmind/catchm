import { useEffect, useState } from "react"
import { ChevronDown } from "lucide-react"
import { cn } from "cn"

import { SaveStatus, SegmentedControl, SettingRow, SettingsSection } from "@/components/settings/pieces"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { getConfig, type AppConfig, type SttEngine } from "@/lib/api"

/** Absolute http(s) URL, checked on blur only (no red on first render). */
function isAbsoluteHttpUrl(v: string): boolean {
  try {
    const u = new URL(v)
    return u.protocol === "http:" || u.protocol === "https:"
  } catch {
    return false
  }
}

const ENGINES: { value: SttEngine; label: string; hint: string }[] = [
  { value: "whisper", label: "Whisper (local)", hint: "Runs on this device via your Whisper server." },
  { value: "soniox", label: "Soniox (cloud)", hint: "Fast and accurate; needs an API key." },
]

export function SettingsTranscription() {
  const [config, setConfig] = useState<AppConfig | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [keyInput, setKeyInput] = useState("")
  const [keyEditing, setKeyEditing] = useState(false)
  const [keyError, setKeyError] = useState<string | null>(null)
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [whisperUrl, setWhisperUrl] = useState("")
  const [sonioxUrl, setSonioxUrl] = useState("")
  const [urlErrors, setUrlErrors] = useState<{ whisper?: string; soniox?: string }>({})

  useEffect(() => {
    getConfig()
      .then((c) => {
        setConfig(c)
        setWhisperUrl(c.whisper_url)
        setSonioxUrl(c.soniox.url)
      })
      .catch((err: unknown) => setLoadError(err instanceof Error ? err.message : String(err)))
  }, [])

  const { save, put } = useConfigSave(config, setConfig)

  const setEngine = (engine: SttEngine) => {
    void put({ engine })
  }

  const startReplaceKey = () => {
    setKeyInput("")
    setKeyError(null)
    setKeyEditing(true)
  }

  const saveKey = async () => {
    const key = keyInput.trim()
    if (!key) {
      setKeyError("Enter an API key first.")
      return
    }
    const ok = await put({ soniox_api_key: key })
    if (ok) {
      setKeyEditing(false)
      setKeyInput("")
      setKeyError(null)
    } else {
      setKeyError("Couldn't save the key — try again.")
    }
  }

  const commitWhisperUrl = () => {
    const v = whisperUrl.trim()
    if (!isAbsoluteHttpUrl(v)) {
      setUrlErrors((e) => ({ ...e, whisper: "Enter an absolute http:// or https:// URL." }))
      return
    }
    setUrlErrors((e) => ({ ...e, whisper: undefined }))
    if (v !== config?.whisper_url) void put({ whisper_url: v })
  }

  const commitSonioxUrl = () => {
    const v = sonioxUrl.trim()
    if (!isAbsoluteHttpUrl(v)) {
      setUrlErrors((e) => ({ ...e, soniox: "Enter an absolute http:// or https:// URL." }))
      return
    }
    setUrlErrors((e) => ({ ...e, soniox: undefined }))
    if (v !== config?.soniox.url) void put({ soniox: { url: v } })
  }

  if (loadError) {
    return (
      <SettingsSection title="Transcription" description="Speech-to-text engine and its settings.">
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

  const keySet = config.soniox_api_key_set

  return (
    <div>
      <SettingsSection
        title="Transcription"
        description="Defaults for turning speech into text. Saved to your CatchM config."
      >
        <SettingRow
          label="Engine"
          htmlFor="stt-engine"
          description={
            <>
              Used as the default when starting a meeting.{" "}
              {ENGINES.find((e) => e.value === (config.engine === "soniox" ? "soniox" : "whisper"))?.hint}
            </>
          }
          control={
            <SegmentedControl
              label="Transcription engine"
              value={config.engine === "soniox" ? "soniox" : "whisper"}
              onChange={setEngine}
              options={ENGINES.map((e) => ({ value: e.value, label: e.label }))}
            />
          }
        />
        <SettingRow
          label="Soniox API key"
          htmlFor="soniox-key"
          description="Stored locally on this device."
          control={
            keyEditing ? (
              <div className="grid w-72 justify-items-end gap-1.5">
                <Input
                  id="soniox-key"
                  type="password"
                  autoComplete="off"
                  value={keyInput}
                  onChange={(e) => setKeyInput(e.target.value)}
                  placeholder="New API key"
                  autoFocus
                />
                <div className="flex gap-2">
                  <Button size="sm" variant="outline" onClick={() => { setKeyEditing(false); setKeyInput(""); setKeyError(null) }}>
                    Cancel
                  </Button>
                  <Button size="sm" onClick={() => void saveKey()}>
                    Save key
                  </Button>
                </div>
                {keyError && <p className="text-sm text-destructive">{keyError}</p>}
              </div>
              ) : (
                <div className="flex items-center gap-2">
                  {keySet ? (
                    <>
                      <span className="text-sm text-muted-foreground" aria-label="API key set">
                        ● Set
                      </span>
                      <Button size="sm" variant="outline" onClick={startReplaceKey}>
                        Replace
                      </Button>
                    </>
                  ) : (
                    <Button size="sm" variant="outline" onClick={startReplaceKey}>
                      Add key
                    </Button>
                  )}
                </div>
              )
          }
        />
        <div className="flex items-center justify-end">
          <SaveStatus state={save.state} error={save.error} />
        </div>
      </SettingsSection>

      <SettingsSection title="Advanced" description="Server endpoints for the engines.">
        <button
          type="button"
          onClick={() => setAdvancedOpen((o) => !o)}
          aria-expanded={advancedOpen}
          className="flex w-full items-center gap-2 rounded-md py-2 text-sm font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          <ChevronDown className={cn("size-4 transition-transform", advancedOpen && "rotate-180")} />
          {advancedOpen ? "Hide" : "Show"} Whisper & Soniox server URLs
        </button>

        {advancedOpen && (
          <div className="grid gap-2">
            <SettingRow
              label="Whisper server URL"
              htmlFor="stt-whisper-url"
              description="Where the local Whisper server listens."
              control={
                <div className="grid w-96 justify-items-end gap-1">
                  <Input
                    id="stt-whisper-url"
                    type="url"
                    value={whisperUrl}
                    onChange={(e) => setWhisperUrl(e.target.value)}
                    onBlur={commitWhisperUrl}
                    placeholder="http://127.0.0.1:8080"
                    aria-invalid={urlErrors.whisper ? true : undefined}
                    className="w-full"
                  />
                  {urlErrors.whisper && <p className="text-sm text-destructive">{urlErrors.whisper}</p>}
                </div>
              }
            />
            <SettingRow
              label="Soniox WebSocket URL"
              htmlFor="stt-soniox-url"
              description="Endpoint for the Soniox realtime stream."
              control={
                <div className="grid w-96 justify-items-end gap-1">
                  <Input
                    id="stt-soniox-url"
                    type="url"
                    value={sonioxUrl}
                    onChange={(e) => setSonioxUrl(e.target.value)}
                    onBlur={commitSonioxUrl}
                    placeholder="wss://stt-rt.soniox.com/transcribe-websocket"
                    aria-invalid={urlErrors.soniox ? true : undefined}
                    className="w-full"
                  />
                  {urlErrors.soniox && <p className="text-sm text-destructive">{urlErrors.soniox}</p>}
                </div>
              }
            />
          </div>
        )}
      </SettingsSection>
    </div>
  )
}
