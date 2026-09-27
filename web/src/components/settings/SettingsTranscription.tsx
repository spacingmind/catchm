import { useCallback, useEffect, useRef, useState, type FormEvent } from "react"

import { Disclosure } from "@/components/Disclosure"
import { StatusMessage } from "@/components/StatusMessage"
import {
  descId,
  labelId,
  SaveStatus,
  SegmentedControl,
  SettingRow,
  SettingsSection,
} from "@/components/settings/pieces"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { getConfig, type AppConfig, type SttEngine } from "@/lib/api"
import { noAutofill } from "@/lib/inputProps"
import { isAbsoluteUrl } from "@/lib/url"

const ENGINES: { value: SttEngine; label: string; hint: string }[] = [
  { value: "whisper", label: "Whisper (local)", hint: "Runs on this device via your Whisper server." },
  { value: "soniox", label: "Soniox (cloud)", hint: "Fast and accurate; needs an API key." },
]

const KEY_ERROR_ID = "soniox-key-error"
const WHISPER_URL_ERROR_ID = "stt-whisper-url-error"
const SONIOX_URL_ERROR_ID = "stt-soniox-url-error"

export function SettingsTranscription() {
  const [config, setConfig] = useState<AppConfig | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [keyInput, setKeyInput] = useState("")
  const [keyEditing, setKeyEditing] = useState(false)
  const [keySaving, setKeySaving] = useState(false)
  const [keyError, setKeyError] = useState<string | null>(null)
  const [whisperUrl, setWhisperUrl] = useState("")
  const [sonioxUrl, setSonioxUrl] = useState("")
  const [urlErrors, setUrlErrors] = useState<{ whisper?: string; soniox?: string }>({})

  const keyInputRef = useRef<HTMLInputElement | null>(null)
  // Add/Replace button; focus returns here when the key editor closes so
  // it isn't dropped on <body> with the unmounted form.
  const keyButtonRef = useRef<HTMLButtonElement | null>(null)
  const restoreKeyFocus = useRef(false)

  const load = useCallback(() => {
    getConfig()
      .then((c) => {
        setConfig(c)
        setWhisperUrl(c.whisper_url)
        setSonioxUrl(c.soniox.url)
      })
      .catch((err: unknown) => setLoadError(err instanceof Error ? err.message : String(err)))
  }, [])

  useEffect(load, [load])

  const retryLoad = () => {
    setLoadError(null)
    load()
  }

  useEffect(() => {
    if (!keyEditing && restoreKeyFocus.current) {
      restoreKeyFocus.current = false
      keyButtonRef.current?.focus()
    }
  }, [keyEditing])

  const { save, put } = useConfigSave(config, setConfig)

  const setEngine = (engine: SttEngine) => {
    void put({ engine })
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
      setKeyError("Enter an API key first.")
      keyInputRef.current?.focus()
      return
    }
    setKeySaving(true)
    setKeyError(null)
    const ok = await put({ soniox_api_key: key })
    setKeySaving(false)
    if (ok) {
      closeKeyEditor()
    } else {
      setKeyError("Couldn’t save the key. Check that CatchM is still running, then try again.")
      keyInputRef.current?.focus()
    }
  }

  const commitWhisperUrl = () => {
    const v = whisperUrl.trim()
    if (!isAbsoluteUrl(v, ["http", "https"])) {
      setUrlErrors((e) => ({ ...e, whisper: "Enter an absolute http:// or https:// URL, like http://127.0.0.1:8080." }))
      return
    }
    setUrlErrors((e) => ({ ...e, whisper: undefined }))
    if (v !== config?.whisper_url) void put({ whisper_url: v })
  }

  const commitSonioxUrl = () => {
    const v = sonioxUrl.trim()
    // The Soniox engine dials this with a WebSocket client, which only
    // speaks ws:// and wss:// (the default is wss://).
    if (!isAbsoluteUrl(v, ["ws", "wss"])) {
      setUrlErrors((e) => ({
        ...e,
        soniox: "Enter an absolute ws:// or wss:// URL, like wss://stt-rt.soniox.com/transcribe-websocket.",
      }))
      return
    }
    setUrlErrors((e) => ({ ...e, soniox: undefined }))
    if (v !== config?.soniox.url) void put({ soniox: { url: v } })
  }

  if (loadError) {
    return (
      <SettingsSection title="Transcription" description="Speech-to-text engine and its settings.">
        <div className="grid justify-items-start gap-3">
          <StatusMessage tone="error">
            Couldn’t load transcription settings: {loadError}. Check that CatchM is still running, then try again.
          </StatusMessage>
          <Button size="sm" variant="outline" onClick={retryLoad}>
            Try again
          </Button>
        </div>
      </SettingsSection>
    )
  }

  if (!config) {
    return (
      <div aria-busy="true" className="grid gap-4">
        <span className="sr-only">Loading transcription settings…</span>
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
      </div>
    )
  }

  const keySet = config.soniox_api_key_set
  const engine: SttEngine = config.engine === "soniox" ? "soniox" : "whisper"

  return (
    <div>
      <SettingsSection
        title="Transcription"
        description="Defaults for turning speech into text. Saved to your CatchM config."
      >
        <SettingRow
          label="Engine"
          id="stt-engine"
          description={
            <>
              Used as the default when starting a meeting. {ENGINES.find((e) => e.value === engine)?.hint}
            </>
          }
          control={
            <SegmentedControl
              id="stt-engine"
              labelledBy={labelId("stt-engine")}
              describedBy={descId("stt-engine")}
              value={engine}
              onChange={setEngine}
              options={ENGINES.map((e) => ({ value: e.value, label: e.label }))}
            />
          }
        />
        <SettingRow
          label="Soniox API key"
          htmlFor={keyEditing ? "soniox-key" : undefined}
          id="soniox-key"
          description="Stored locally on this device."
          control={
            keyEditing ? (
              <form noValidate onSubmit={(e) => void saveKey(e)} className="grid w-72 justify-items-end gap-1.5">
                <Input
                  ref={keyInputRef}
                  id="soniox-key"
                  name="soniox_api_key"
                  type="password"
                  {...noAutofill}
                  value={keyInput}
                  onChange={(e) => setKeyInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Escape" && !keySaving) closeKeyEditor()
                  }}
                  placeholder="New API key…"
                  // readOnly, not disabled, so focus can return here on failure.
                  readOnly={keySaving}
                  aria-invalid={keyError ? true : undefined}
                  aria-describedby={keyError ? `${descId("soniox-key")} ${KEY_ERROR_ID}` : descId("soniox-key")}
                  // The user just asked to add/replace the key: move focus into the field.
                  autoFocus
                />
                <div className="flex gap-2">
                  <Button type="button" size="sm" variant="outline" disabled={keySaving} onClick={closeKeyEditor}>
                    Cancel
                  </Button>
                  <Button type="submit" size="sm" disabled={keySaving} aria-busy={keySaving || undefined}>
                    {keySaving ? "Saving…" : "Save key"}
                  </Button>
                </div>
                <StatusMessage id={KEY_ERROR_ID} tone="error">
                  {keyError}
                </StatusMessage>
              </form>
            ) : (
              <div className="flex items-center gap-2">
                {keySet ? (
                  <>
                    <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
                      <span aria-hidden="true" className="size-2 rounded-full bg-primary" />
                      Key set
                    </span>
                    <Button ref={keyButtonRef} size="sm" variant="outline" onClick={startReplaceKey}>
                      Replace key
                    </Button>
                  </>
                ) : (
                  <Button ref={keyButtonRef} size="sm" variant="outline" onClick={startReplaceKey}>
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
        <Disclosure label="Whisper & Soniox server URLs">
          <div className="grid gap-2">
            <SettingRow
              label="Whisper server URL"
              htmlFor="stt-whisper-url"
              description="Where the local Whisper server listens."
              control={
                <div className="grid w-96 justify-items-end gap-1">
                  <Input
                    id="stt-whisper-url"
                    name="whisper_url"
                    type="url"
                    {...noAutofill}
                    value={whisperUrl}
                    onChange={(e) => setWhisperUrl(e.target.value)}
                    onBlur={commitWhisperUrl}
                    placeholder="http://127.0.0.1:8080…"
                    aria-invalid={urlErrors.whisper ? true : undefined}
                    aria-describedby={
                      urlErrors.whisper
                        ? `${descId("stt-whisper-url")} ${WHISPER_URL_ERROR_ID}`
                        : descId("stt-whisper-url")
                    }
                    className="w-full"
                  />
                  <StatusMessage id={WHISPER_URL_ERROR_ID} tone="error" className="break-words">
                    {urlErrors.whisper}
                  </StatusMessage>
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
                    name="soniox_url"
                    type="url"
                    {...noAutofill}
                    value={sonioxUrl}
                    onChange={(e) => setSonioxUrl(e.target.value)}
                    onBlur={commitSonioxUrl}
                    placeholder="wss://stt-rt.soniox.com/transcribe-websocket…"
                    aria-invalid={urlErrors.soniox ? true : undefined}
                    aria-describedby={
                      urlErrors.soniox
                        ? `${descId("stt-soniox-url")} ${SONIOX_URL_ERROR_ID}`
                        : descId("stt-soniox-url")
                    }
                    className="w-full"
                  />
                  <StatusMessage id={SONIOX_URL_ERROR_ID} tone="error" className="break-words">
                    {urlErrors.soniox}
                  </StatusMessage>
                </div>
              }
            />
          </div>
        </Disclosure>
      </SettingsSection>
    </div>
  )
}
