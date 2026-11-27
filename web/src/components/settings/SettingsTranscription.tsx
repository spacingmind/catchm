import { useCallback, useEffect, useRef, useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"

import { Disclosure } from "@/components/Disclosure"
import { StatusMessage } from "@/components/StatusMessage"
import {
  descId,
  ExternalLink,
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
import { engineHint, engineLabel } from "@/lib/labels"
import { navigate } from "@/lib/route"
import { isCloudVisible, isSelfHostVisible, visibleEngines } from "@/lib/setup"
import { isAbsoluteUrl } from "@/lib/url"

// Display order of the engine picker (setup.ts lists cloud first).
const ENGINE_ORDER: SttEngine[] = ["whisper", "soniox"]

const WHISPER_SERVER_DOCS = "https://github.com/ggml-org/whisper.cpp/tree/master/examples/server"
const SONIOX_SITE = "https://soniox.com"

const KEY_ERROR_ID = "soniox-key-error"
const WHISPER_URL_ERROR_ID = "stt-whisper-url-error"
const SONIOX_URL_ERROR_ID = "stt-soniox-url-error"

export function SettingsTranscription() {
  const { t } = useTranslation()
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
      setKeyError(t("settings.transcription.enterKey"))
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
      setKeyError(t("settings.transcription.keySaveError"))
      keyInputRef.current?.focus()
    }
  }

  const commitWhisperUrl = () => {
    const v = whisperUrl.trim()
    if (!isAbsoluteUrl(v, ["http", "https"])) {
      setUrlErrors((e) => ({ ...e, whisper: t("settings.transcription.whisperUrlError") }))
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
      setUrlErrors((e) => ({ ...e, soniox: t("settings.transcription.sonioxUrlError") }))
      return
    }
    setUrlErrors((e) => ({ ...e, soniox: undefined }))
    if (v !== config?.soniox.url) void put({ soniox: { url: v } })
  }

  if (loadError) {
    return (
      <SettingsSection title={t("settings.nav.transcription")} description={t("settings.transcription.desc")}>
        <div className="grid justify-items-start gap-3">
          <StatusMessage tone="error">
            {t("settings.transcription.loadError", { error: loadError })}
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
        <span className="sr-only">{t("settings.transcription.loading")}</span>
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-16 w-full rounded-xl" />
      </div>
    )
  }

  const keySet = config.soniox_api_key_set
  const engine: SttEngine = config.engine === "soniox" ? "soniox" : "whisper"
  // The setup hides the other side: no Whisper under Cloud, no Soniox
  // under Self-host. A single engine is shown, not picked.
  const shown = visibleEngines(config.setup)
  const engines = ENGINE_ORDER.filter((e) => shown.includes(e))
  const showWhisper = isSelfHostVisible(config.setup)
  const showSoniox = isCloudVisible(config.setup)
  const advancedWhisper = showWhisper && engine !== "whisper"

  // "On this computer" doesn't work until the user runs a Whisper
  // server, so while it's the engine its address sits in the main
  // section with a set-up link; otherwise it stays under Advanced.
  const whisperRow = (
    <SettingRow
      label={t("settings.transcription.whisperUrlLabel")}
      htmlFor="stt-whisper-url"
      description={
        engine === "whisper" ? (
          <>
            {t("settings.transcription.whisperUrlActive", { engine: engineLabel("whisper") })}{" "}
            <ExternalLink href={WHISPER_SERVER_DOCS}>{t("settings.transcription.whisperSetupLink")}</ExternalLink>
          </>
        ) : (
          t("settings.transcription.whisperUrlDesc")
        )
      }
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
              urlErrors.whisper ? `${descId("stt-whisper-url")} ${WHISPER_URL_ERROR_ID}` : descId("stt-whisper-url")
            }
            className="w-full"
          />
          <StatusMessage id={WHISPER_URL_ERROR_ID} tone="error" className="break-words">
            {urlErrors.whisper}
          </StatusMessage>
        </div>
      }
    />
  )

  return (
    <div>
      <SettingsSection
        title={t("settings.nav.transcription")}
        description={t("settings.transcription.descSaved")}
      >
        <SettingRow
          label={t("settings.transcription.engineLabel")}
          id="stt-engine"
          description={
            <>
              {t("settings.transcription.engineDescPrefix")} {engineHint(engine)}
            </>
          }
          control={
            engines.length > 1 ? (
              <SegmentedControl
                id="stt-engine"
                labelledBy={labelId("stt-engine")}
                describedBy={descId("stt-engine")}
                value={engine}
                onChange={setEngine}
                options={engines.map((e) => ({ value: e, label: engineLabel(e) }))}
              />
            ) : (
              <div className="flex items-center gap-3">
                <span className="text-sm font-medium">{engineLabel(engine)}</span>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => navigate({ name: "settings", section: "setup" })}
                  aria-describedby={descId("stt-engine")}
                >
                  {t("settings.transcription.changeSetup")}
                </Button>
              </div>
            )
          }
        />
        {showWhisper && engine === "whisper" && whisperRow}
        {showSoniox && (
          <SettingRow
            label={t("settings.transcription.keyLabel")}
            htmlFor={keyEditing ? "soniox-key" : undefined}
            id="soniox-key"
            description={
              <>
                {t("settings.transcription.keyDesc", { engine: engineLabel("soniox") })}{" "}
                <ExternalLink href={SONIOX_SITE}>{t("settings.transcription.getKeyLink")}</ExternalLink>
              </>
            }
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
                    placeholder={t("settings.transcription.keyPlaceholder")}
                    // readOnly, not disabled, so focus can return here on failure.
                    readOnly={keySaving}
                    aria-invalid={keyError ? true : undefined}
                    aria-describedby={keyError ? `${descId("soniox-key")} ${KEY_ERROR_ID}` : descId("soniox-key")}
                    // The user just asked to add/replace the key: move focus into the field.
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
                        {t("settings.transcription.keySet")}
                      </span>
                      <Button ref={keyButtonRef} size="sm" variant="outline" onClick={startReplaceKey}>
                        {t("settings.transcription.replaceKey")}
                      </Button>
                    </>
                  ) : (
                    <Button ref={keyButtonRef} size="sm" variant="outline" onClick={startReplaceKey}>
                      {t("settings.transcription.addKey")}
                    </Button>
                  )}
                </div>
              )
            }
          />
        )}
        <div className="flex items-center justify-end">
          <SaveStatus state={save.state} error={save.error} />
        </div>
      </SettingsSection>

      {(advancedWhisper || showSoniox) && (
        <SettingsSection title={t("settings.transcription.advancedTitle")} description={t("settings.transcription.advancedDesc")}>
          <Disclosure
            label={
              advancedWhisper && showSoniox
                ? t("settings.transcription.urlsLabel")
                : advancedWhisper
                  ? t("settings.transcription.whisperUrlLabel")
                  : t("settings.transcription.sonioxUrlLabel")
            }
          >
            <div className="grid gap-2">
              {advancedWhisper && whisperRow}
              {showSoniox && (
                <SettingRow
                  label={t("settings.transcription.sonioxUrlLabel")}
                  htmlFor="stt-soniox-url"
                  description={t("settings.transcription.sonioxUrlDesc", { engine: engineLabel("soniox") })}
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
              )}
            </div>
          </Disclosure>
        </SettingsSection>
      )}
    </div>
  )
}
