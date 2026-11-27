import { useEffect, useRef, useState, type FormEvent, type RefObject } from "react"
import { flushSync } from "react-dom"
import { useTranslation } from "react-i18next"
import i18next from "i18next"
import { ArrowLeftRight, Loader2 } from "lucide-react"

import { Disclosure } from "@/components/Disclosure"
import { ProjectSelectField } from "@/components/Projects"
import { SegmentedControl } from "@/components/settings/pieces"
import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Toggle } from "@/components/ui/toggle"
import type { Section as SettingsSection } from "@/components/settings/Settings"
import {
  getAudioSources,
  getConfig,
  getDevices,
  getSttStatus,
  getTranslateStatus,
  startMeeting,
  type AudioSources,
  type AudioSource,
  type Device,
  type StartMeetingResponse,
  type SttEngine,
  type SttStatus,
  type TranslateProvider,
  type TranslateStatus,
} from "@/lib/api"
import { noAutofill } from "@/lib/inputProps"
import {
  DEFAULT_DEVICE_VALUE,
  deviceSelectValue,
  effectiveSource,
  readDevicePref,
  readSourcePref,
  writeStartedDevice,
} from "@/lib/recordPrefs"
import { engineHint, engineLabel, sourceHint, sourceLabel } from "@/lib/labels"
import { activeLocale } from "@/i18n"
import { languageLabel, sortedLanguageCodes } from "@/lib/languages"
import {
  engineHasKey,
  engineOptionName,
  engineTranslates,
  toEngine,
  translateNeed,
  translatingEngineNames,
  visibleEngines,
} from "@/lib/setup"
import { choiceProject, readLastProject, refreshProjects, useProjects, writeLastProject, type ProjectChoice } from "@/lib/projects"
import { readStartPrefs, writeStartPrefs, type StartPrefs } from "@/lib/startPrefs"

/** Backend requires a non-empty title; generate from date/time. */
function defaultTitle(): string {
  const now = new Date()
  return i18next.t("startMeeting.defaultTitle", {
    date: now.toLocaleDateString(activeLocale()),
    time: now.toLocaleTimeString(activeLocale(), { hour: "2-digit", minute: "2-digit" }),
  })
}

/** Radix Select can't have an item with value "", which is what "auto"
 *  (from) / "none" (to) mean on the wire; this stands in for it. */
const UNSET_LANG = "__unset__"

interface StartMeetingPopoverProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onStarted: (meeting: StartMeetingResponse) => void
  /** Sidebar Start button the popover anchors to. */
  anchor?: RefObject<HTMLButtonElement | null>
  /** Jump to a Settings section (from the readiness warnings). */
  onOpenSettings?: (section: SettingsSection) => void
}

export function StartMeetingPopover({ open, onOpenChange, onStarted, anchor, onOpenSettings }: StartMeetingPopoverProps) {
  const { t } = useTranslation()
  const [last] = useState<StartPrefs>(readStartPrefs)
  const [title, setTitle] = useState("")
  const [source, setSource] = useState<AudioSource>("mic")
  const [systemAvailable, setSystemAvailable] = useState(false)
  const [systemReason, setSystemReason] = useState("")
  const [device, setDevice] = useState(readDevicePref)
  const [devices, setDevices] = useState<Device[]>([])
  const [engine, setEngine] = useState<SttEngine>(last.engine ?? "whisper")
  const [configEngine, setConfigEngine] = useState<SttEngine>("whisper")
  // Engines the user can pick from: the setup shows them and, for a
  // cloud engine, its key is set (null until the config loads).
  const [pickable, setPickable] = useState<SttEngine[] | null>(null)
  const [translateProvider, setTranslateProvider] = useState<TranslateProvider>("ollama")
  const [lang, setLang] = useState(last.lang ?? "")
  const [translateTo, setTranslateTo] = useState(last.translateTo ?? "")
  const [twoWay, setTwoWay] = useState(last.twoWay ?? false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pairError, setPairError] = useState<string | null>(null)
  const [optionsOpen, setOptionsOpen] = useState(false)
  const projects = useProjects()
  // Defaults to the last used project (if it still exists).
  const [projectChoice, setProjectChoice] = useState<ProjectChoice>(() => ({ name: readLastProject(), isNew: false }))
  const [projectError, setProjectError] = useState<string | null>(null)
  // null = unknown (loading or the check failed): never blocks a start.
  const [sttStatus, setSttStatus] = useState<SttStatus | null>(null)
  const [translateStatus, setTranslateStatus] = useState<TranslateStatus | null>(null)
  const titleRef = useRef<HTMLInputElement>(null)
  const langRef = useRef<HTMLButtonElement | null>(null)

  // Devices + system capability (M19 contract; graceful degrade), plus
  // the config engine and which engines have their keys.
  useEffect(() => {
    if (!open) return
    let cancelled = false
    void refreshProjects()
    getAudioSources()
      .then((s: AudioSources) => {
        if (cancelled) return
        setDevices(s.mic_devices ?? [])
        setSystemAvailable(s.system?.supported === true)
        setSystemReason(s.system?.reason || i18next.t("startMeeting.systemUnavailableFallback"))
        setSource(effectiveSource(readSourcePref(), s))
      })
      .catch(() =>
        getDevices()
          .then((ds) => {
            if (cancelled) return
            setDevices(ds)
            setSystemAvailable(false)
            setSystemReason(i18next.t("startMeeting.oldBackendReason"))
            setSource(effectiveSource(readSourcePref(), null))
          })
          .catch(() => {
            if (!cancelled) setSource(readSourcePref())
          }),
      )
    getConfig()
      .then((c) => {
        if (cancelled) return
        const configured = toEngine(c.engine)
        setConfigEngine(configured)
        // The configured engine stays listed without its key, so the
        // picker never sits on nothing; the status check explains it.
        setPickable(visibleEngines(c.setup).filter((e) => e === configured || engineHasKey(c, e)))
        setTranslateProvider(c.translate.provider)
      })
      .catch(() => undefined)
    getSttStatus()
      .then((st) => {
        if (!cancelled) setSttStatus(st)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
      // Recheck on every open (the user may have just fixed it in Settings).
      setSttStatus(null)
    }
  }, [open])

  // Focus the first field each time the popover opens.
  useEffect(() => {
    if (!open) return
    const id = window.setTimeout(() => titleRef.current?.focus(), 50)
    return () => window.clearTimeout(id)
  }, [open])

  // Engine is only a real choice when more than one is pickable (the
  // setup shows it and its key is set); otherwise the config engine runs
  // unselected. A remembered engine that's no longer pickable falls back
  // to the config engine.
  const usableEngines = pickable ?? []
  const activeEngine = usableEngines.length > 1 && usableEngines.includes(engine) ? engine : configEngine

  // Only an engine the status check positively reports as not ready
  // blocks Start (and "Start anyway" still gets past it).
  const engineStatus = sttStatus?.[activeEngine]
  const sttBlocked = engineStatus?.ready === false
  const sttWarning = sttBlocked
    ? t("startMeeting.sttNotReady", { reason: engineStatus.reason || t("startMeeting.sttNotReadyFallback") })
    : null
  // Another pickable engine that's ready, offered as "Use … instead".
  const switchTo = sttBlocked
    ? usableEngines.find((e) => e !== activeEngine && sttStatus?.[e]?.ready === true)
    : undefined

  // A text translator only runs when translation is requested and the
  // engine can't translate itself (setup.ts engineTranslates; see
  // internal/pipeline Start and internal/stt SupportsTranslation). With
  // provider "none" there's no translator at all; any other provider
  // (Ollama, Google, the LLM) reports its own readiness.
  const needsTranslator = (twoWay || translateTo !== "") && !engineTranslates(activeEngine)
  const checkTranslator = needsTranslator && translateProvider !== "none"
  const needsProvider = needsTranslator && translateProvider === "none"
  useEffect(() => {
    if (!open || !checkTranslator) return
    let cancelled = false
    getTranslateStatus()
      .then((st) => {
        if (!cancelled) setTranslateStatus(st)
      })
      .catch(() => {
        if (!cancelled) setTranslateStatus(null)
      })
    return () => {
      cancelled = true
    }
  }, [open, checkTranslator])
  const translateWarning = needsProvider
    ? t("startMeeting.translateNeedsProvider", {
        engine: engineOptionName(activeEngine),
        translators: translatingEngineNames(),
      })
    : checkTranslator && translateStatus?.provider === translateProvider && !translateStatus.ready
      ? translateStatus.provider === "ollama"
        ? t("startMeeting.translateNotReady")
        : t("startMeeting.translateNotReadyCloud", { reason: translateNeed(translateStatus) })
      : null

  const openSettings = (section: SettingsSection) => {
    onOpenChange(false)
    onOpenSettings?.(section)
  }

  const optionsSummary = [
    translateTo
      ? `${lang ? languageLabel(lang) : t("startMeeting.autoDetect")} ${twoWay ? "↔" : "→"} ${languageLabel(translateTo)}`
      : lang
        ? languageLabel(lang)
        : null,
    twoWay ? t("startMeeting.twoWayShort") : null,
    engineLabel(activeEngine),
  ]
    .filter(Boolean)
    .join(" · ")

  // A saved device that's since been unplugged falls back to the default
  // rather than failing the start. Unknown (list didn't load): send as is.
  const startDevice = devices.length === 0 || devices.some((d) => d.ID === device) ? device : ""

  const changeLang = (v: string) => {
    setLang(v === UNSET_LANG ? "" : v)
    setPairError(null)
  }
  const changeTranslateTo = (v: string) => {
    setTranslateTo(v === UNSET_LANG ? "" : v)
    setPairError(null)
  }
  const changeTwoWay = (on: boolean) => {
    setTwoWay(on)
    setPairError(null)
  }

  // While the request is in flight the popover stays put (showing
  // "Starting…"), so a failure has its error right where the user is.
  const handleOpenChange = (next: boolean) => {
    if (!next && submitting) return
    onOpenChange(next)
  }

  const handleStart = (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    // Enter in the title field mustn't bypass a not-ready engine; that
    // takes the explicit "Start anyway".
    if (sttBlocked) return
    void start()
  }

  const start = async () => {
    if (submitting) return
    const project = choiceProject(projectChoice, projects)
    if (projectChoice.isNew && !project) {
      setProjectError(t("startMeeting.projectError"))
      document.getElementById("start-project-name")?.focus()
      return
    }
    setProjectError(null)
    if (twoWay && (!lang || !translateTo)) {
      setPairError(t("startMeeting.pairError"))
      // The language selects live inside Options, which unmounts its
      // content while collapsed: open it synchronously so there's a
      // trigger to focus.
      flushSync(() => setOptionsOpen(true))
      langRef.current?.focus()
      return
    }
    setPairError(null)
    setSubmitting(true)
    setError(null)
    try {
      const deviceForStart = source === "system" ? "" : startDevice
      const meeting = await startMeeting({
        title: title.trim() || defaultTitle(),
        source,
        device: deviceForStart || undefined,
        engine: activeEngine,
        lang: lang.trim() || undefined,
        translate_to: translateTo.trim() || undefined,
        two_way: twoWay || undefined,
        project: project || undefined,
      })
      writeLastProject(project)
      setProjectChoice({ name: project, isNew: false })
      void refreshProjects()
      writeStartPrefs({ lang: lang.trim(), translateTo: translateTo.trim(), twoWay, engine: activeEngine })
      writeStartedDevice(meeting.id, deviceForStart)
      onStarted(meeting)
      onOpenChange(false)
      setTitle("")
      setError(null)
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err)
      setError(t("startMeeting.startFailed", { reason }))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      {anchor && <PopoverAnchor virtualRef={anchor} />}
      <PopoverContent className="w-96" collisionPadding={16}>
        <form onSubmit={handleStart} className="grid gap-4" aria-busy={submitting || undefined}>
          <div className="grid gap-1.5">
            <Label htmlFor="start-title">{t("startMeeting.title")}</Label>
            <Input
              id="start-title"
              name="title"
              ref={titleRef}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={defaultTitle()}
              aria-describedby="start-title-hint"
              {...noAutofill}
            />
            <p id="start-title-hint" className="text-xs text-muted-foreground">
              {t("startMeeting.titleHint")}
            </p>
          </div>

          <ProjectSelectField
            id="start-project"
            value={projectChoice}
            onChange={(c) => {
              setProjectChoice(c)
              setProjectError(null)
            }}
            projects={projects}
            error={projectError}
          />

          <div className="grid gap-1.5">
            <span id="start-source-label" className="text-sm font-medium">
              {t("startMeeting.audioSource")}
            </span>
            <SegmentedControl
              id="start-source"
              labelledBy="start-source-label"
              describedBy="start-source-hint"
              value={source}
              onChange={setSource}
              options={[
                { value: "mic", label: sourceLabel("mic") },
                {
                  value: "system",
                  label: sourceLabel("system"),
                  disabled: !systemAvailable,
                  reason: systemReason || undefined,
                },
                { value: "both", label: sourceLabel("both"), disabled: !systemAvailable, reason: systemReason || undefined },
              ]}
            />
            <p id="start-source-hint" className="text-xs text-muted-foreground">
              {sourceHint()}
            </p>
          </div>

          {source !== "system" && (
            <div className="grid gap-1.5">
              <Label htmlFor="start-device">{t("startMeeting.microphone")}</Label>
              <Select value={deviceSelectValue(startDevice, devices)} onValueChange={(v) => setDevice(v === DEFAULT_DEVICE_VALUE ? "" : v)}>
                <SelectTrigger id="start-device" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={DEFAULT_DEVICE_VALUE}>{t("startMeeting.defaultMic")}</SelectItem>
                  {devices.map((d) => (
                    <SelectItem key={d.ID} value={d.ID}>
                      {d.Name}
                      {d.IsDefault ? t("startMeeting.defaultSuffix") : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          <Disclosure
            open={optionsOpen}
            onOpenChange={setOptionsOpen}
            label={
              <>
                {t("startMeeting.options")}
                <span className="min-w-0 truncate font-normal text-muted-foreground">
                  <span aria-hidden="true">· </span>
                  {optionsSummary || t("startMeeting.defaults")}
                </span>
              </>
            }
          >
            <div className="grid gap-4">
              <div role="group" aria-labelledby="start-translation-label" className="grid gap-1.5">
                <div className="flex items-center justify-between">
                  <span id="start-translation-label" className="text-sm font-medium">
                    {t("startMeeting.translation")}
                  </span>
                  <Toggle
                    size="sm"
                    variant="outline"
                    pressed={twoWay}
                    onPressedChange={changeTwoWay}
                    aria-describedby="start-two-way-hint"
                  >
                    <ArrowLeftRight aria-hidden="true" />
                    {t("startMeeting.twoWayLabel")}
                  </Toggle>
                </div>
                <div className="flex items-center gap-2">
                  <Select value={lang || (twoWay ? undefined : UNSET_LANG)} onValueChange={changeLang}>
                    <SelectTrigger
                      id="start-lang"
                      ref={langRef}
                      className="w-40"
                      aria-label={twoWay ? t("startMeeting.firstLanguage") : t("startMeeting.spokenLanguage")}
                      aria-invalid={pairError && !lang ? true : undefined}
                      aria-describedby={pairError ? "start-pair-error" : undefined}
                    >
                      <SelectValue placeholder={t("startMeeting.firstLanguage")} />
                    </SelectTrigger>
                    <SelectContent>
                      {!twoWay && <SelectItem value={UNSET_LANG}>{t("startMeeting.detectAuto")}</SelectItem>}
                      {sortedLanguageCodes().map((code) => (
                        <SelectItem key={code} value={code}>
                          {languageLabel(code)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <span aria-hidden="true" className="text-sm text-muted-foreground">
                    {twoWay ? "↔" : "→"}
                  </span>
                  <Select value={translateTo || (twoWay ? undefined : UNSET_LANG)} onValueChange={changeTranslateTo}>
                    <SelectTrigger
                      id="start-translate-to"
                      className="w-40"
                      aria-label={twoWay ? t("startMeeting.secondLanguage") : t("startMeeting.translateInto")}
                      aria-invalid={pairError && !translateTo ? true : undefined}
                      aria-describedby={pairError ? "start-pair-error" : undefined}
                    >
                      <SelectValue placeholder={t("startMeeting.secondLanguage")} />
                    </SelectTrigger>
                    <SelectContent>
                      {!twoWay && <SelectItem value={UNSET_LANG}>{t("startMeeting.noTranslation")}</SelectItem>}
                      {sortedLanguageCodes().map((code) => (
                        <SelectItem key={code} value={code}>
                          {languageLabel(code)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <p id="start-two-way-hint" className="text-xs text-muted-foreground">
                  {twoWay ? t("startMeeting.twoWayHint") : t("startMeeting.twoWayOffHint")}
                </p>
                <StatusMessage id="start-pair-error" tone="error">
                  {pairError}
                </StatusMessage>
              </div>

              {usableEngines.length > 1 && (
                <div className="grid gap-1.5">
                  <Label htmlFor="start-engine">{t("startMeeting.transcribeWith")}</Label>
                  <Select value={activeEngine} onValueChange={(v) => setEngine(v as SttEngine)}>
                    <SelectTrigger id="start-engine" className="w-full" aria-describedby="start-engine-hint">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {usableEngines.map((e) => (
                        <SelectItem key={e} value={e}>
                          {engineLabel(e)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p id="start-engine-hint" className="text-xs text-muted-foreground">
                    {engineHint(activeEngine)}
                  </p>
                </div>
              )}
            </div>
          </Disclosure>

          <div className="grid gap-1.5">
            <StatusMessage id="start-stt-warning" tone="warning" className="break-words">
              {sttWarning}
            </StatusMessage>
            {sttBlocked && (
              <div className="flex flex-wrap gap-2">
                {switchTo && (
                  <Button type="button" size="sm" variant="outline" onClick={() => setEngine(switchTo)}>
                    {t("startMeeting.useInstead", { engine: engineLabel(switchTo) })}
                  </Button>
                )}
                {onOpenSettings && (
                  <Button type="button" size="sm" variant="outline" onClick={() => openSettings("transcription")}>
                    {t("startMeeting.openSettings")}
                  </Button>
                )}
              </div>
            )}
          </div>

          <div className="grid gap-1.5">
            <StatusMessage id="start-translate-warning" tone="warning" className="break-words">
              {translateWarning}
            </StatusMessage>
            {translateWarning && onOpenSettings && (
              <div className="flex">
                {needsProvider ? (
                  <Button type="button" size="sm" variant="outline" onClick={() => openSettings("setup")}>
                    {t("startMeeting.openSetup")}
                  </Button>
                ) : (
                  <Button type="button" size="sm" variant="outline" onClick={() => openSettings("translation")}>
                    {t("startMeeting.openTranslationSettings")}
                  </Button>
                )}
              </div>
            )}
          </div>

          <StatusMessage tone="error" className="break-words">
            {error}
          </StatusMessage>

          <div className="flex justify-end gap-2">
            {sttBlocked && (
              <Button
                type="button"
                variant="ghost"
                disabled={submitting}
                onClick={() => void start()}
                aria-describedby="start-stt-warning"
              >
                {t("startMeeting.startAnyway")}
              </Button>
            )}
            <Button
              type="submit"
              disabled={submitting || sttBlocked}
              aria-busy={submitting || undefined}
              aria-describedby={sttBlocked ? "start-stt-warning" : undefined}
            >
              {submitting && <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />}
              {submitting ? t("startMeeting.starting") : t("sidebar.startMeeting")}
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  )
}
