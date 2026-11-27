import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { Disclosure } from "@/components/Disclosure"
import { StatusMessage } from "@/components/StatusMessage"
import { ApiKeyField } from "@/components/settings/ApiKeyField"
import { ChoiceCard } from "@/components/settings/ChoiceCard"
import {
  descId,
  ExternalLink,
  SaveStatus,
  SettingRow,
  SettingsSection,
} from "@/components/settings/pieces"
import { OptionBadges } from "@/components/settings/SetupBuilder"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { getConfig, verifyProvider, type AppConfig, type CloudSttEngine, type SttEngine } from "@/lib/api"
import { noAutofill } from "@/lib/inputProps"
import { engineHint, engineLabel } from "@/lib/labels"
import { navigate } from "@/lib/route"
import {
  CLOUD_ENGINES,
  engineKeyPatch,
  engineKeySet,
  engineOptionName,
  isCloudEngine,
  isOptionVisible,
  isSelfHostVisible,
  keyAdded,
  modulePatch,
  optionsVisible,
  toEngine,
} from "@/lib/setup"
import { isAbsoluteUrl } from "@/lib/url"

const WHISPER_SERVER_DOCS = "https://github.com/ggml-org/whisper.cpp/tree/master/examples/server"

const WHISPER_URL_ERROR_ID = "stt-whisper-url-error"
const SONIOX_URL_ERROR_ID = "stt-soniox-url-error"

export function SettingsTranscription() {
  const { t } = useTranslation()
  const [config, setConfig] = useState<AppConfig | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [whisperUrl, setWhisperUrl] = useState("")
  const [sonioxUrl, setSonioxUrl] = useState("")
  const [urlErrors, setUrlErrors] = useState<{ whisper?: string; soniox?: string }>({})

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

  const { save, put } = useConfigSave(config, setConfig)

  // Same rules as Settings → Setup: an engine from the other side makes
  // the setup Custom, and one that translates locks translation to its own.
  const setEngine = (engine: SttEngine) => {
    if (config && engine !== toEngine(config.engine)) void put(modulePatch(config, "transcription", engine))
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

  const setup = config.setup
  const engine = toEngine(config.engine)
  // The setup hides the other side: no Whisper under Cloud, no cloud
  // engines under Self-host. A single engine is shown, not picked.
  const options = optionsVisible(setup, "transcription")
  const showWhisper = isSelfHostVisible(setup)
  const showSonioxUrl = isOptionVisible(setup, "transcription", "soniox")
  const advancedWhisper = showWhisper && engine !== "whisper"
  // Keys for the cloud engines that aren't the chosen one still matter:
  // import and "Improve with …" can use them.
  const otherKeyEngines = options.map((o) => o.id).filter((e): e is CloudSttEngine => isCloudEngine(e) && e !== engine)

  const keyRow = (e: CloudSttEngine) => {
    const provider = engineOptionName(e)
    const info = CLOUD_ENGINES[e]
    return (
      <ApiKeyField
        key={e}
        id={`${e}-key`}
        label={t("settings.transcription.keyLabel", { provider })}
        description={
          <>
            {t("settings.transcription.keyDesc", { engine: engineLabel(e) })}{" "}
            <ExternalLink href={info.keyUrl}>{t("settings.transcription.getKeyLink", { provider })}</ExternalLink>
          </>
        }
        keySet={engineKeySet(config, e)}
        onSave={(key) => put(engineKeyPatch(e, key))}
        verify={info.verifiable ? (key) => verifyProvider(e, key) : undefined}
      />
    )
  }

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
        {options.length > 1 ? (
          <fieldset className="border-b border-border pb-4">
            <legend className="text-sm font-medium">{t("settings.transcription.engineLabel")}</legend>
            <p className="mt-0.5 text-sm text-muted-foreground">{t("settings.transcription.engineDescPrefix")}</p>
            <div className="mt-2 grid gap-3 sm:grid-cols-2">
              {options.map((o) => (
                <ChoiceCard
                  key={o.id}
                  name="stt-engine"
                  value={o.id}
                  checked={engine === o.id}
                  onChange={(v) => setEngine(v as SttEngine)}
                  title={t(o.labelKey)}
                  description={t(o.descKey)}
                  badges={
                    o.badges.length > 0 || keyAdded(config, o) ? <OptionBadges config={config} option={o} /> : undefined
                  }
                />
              ))}
            </div>
          </fieldset>
        ) : (
          <SettingRow
            label={t("settings.transcription.engineLabel")}
            id="stt-engine"
            description={
              <>
                {t("settings.transcription.engineDescPrefix")} {engineHint(engine)}
              </>
            }
            control={
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
            }
          />
        )}
        {showWhisper && engine === "whisper" && whisperRow}
        {isCloudEngine(engine) && keyRow(engine)}
        {otherKeyEngines.length > 0 && (
          <Disclosure label={t("settings.transcription.otherKeysLabel")} className="py-2">
            <p className="text-sm text-muted-foreground">{t("settings.transcription.otherKeysDesc")}</p>
            {otherKeyEngines.map(keyRow)}
          </Disclosure>
        )}
        <div className="flex items-center justify-end">
          <SaveStatus state={save.state} error={save.error} />
        </div>
      </SettingsSection>

      {(advancedWhisper || showSonioxUrl) && (
        <SettingsSection title={t("settings.transcription.advancedTitle")} description={t("settings.transcription.advancedDesc")}>
          <Disclosure
            label={
              advancedWhisper && showSonioxUrl
                ? t("settings.transcription.urlsLabel")
                : advancedWhisper
                  ? t("settings.transcription.whisperUrlLabel")
                  : t("settings.transcription.sonioxUrlLabel")
            }
          >
            <div className="grid gap-2">
              {advancedWhisper && whisperRow}
              {showSonioxUrl && (
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
