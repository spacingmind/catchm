import { useEffect, useRef, useState, type FormEvent, type RefObject } from "react"
import { flushSync } from "react-dom"
import { ArrowLeftRight, Loader2 } from "lucide-react"

import { Disclosure } from "@/components/Disclosure"
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
import { ENGINE_HINT, ENGINE_LABEL, SOURCE_HINT, SOURCE_LABEL } from "@/lib/labels"
import { LANGUAGES, languageLabel } from "@/lib/languages"

/** Last-used start-flow values (non-secret UI prefs only). */
const LAST_KEY = "catchm.prefs.startflow"

interface StartPrefs {
  lang?: string
  translateTo?: string
  twoWay?: boolean
  engine?: SttEngine
}

function readLast(): StartPrefs {
  try {
    const raw = window.localStorage.getItem(LAST_KEY)
    return raw ? (JSON.parse(raw) as StartPrefs) : {}
  } catch {
    return {}
  }
}

function writeLast(p: StartPrefs) {
  try {
    window.localStorage.setItem(LAST_KEY, JSON.stringify(p))
  } catch {
    // Storage unavailable — prefs just won't persist.
  }
}

/** Backend requires a non-empty title; generate from date/time. */
function defaultTitle(): string {
  const now = new Date()
  return `Meeting ${now.toLocaleDateString()} ${now.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`
}

/** Radix Select can't have an item with value "", which is what "auto"
 *  (from) / "none" (to) mean on the wire; this stands in for it. */
const UNSET_LANG = "__unset__"

/** Why Computer audio/Both are disabled when an older (pre-M19) backend
 *  has no /api/audio/sources at all. */
const OLD_BACKEND_REASON = "Update CatchM to use computer audio."
/** When the backend says unsupported but gives no reason. */
const SYSTEM_UNAVAILABLE_FALLBACK = "Computer audio can’t be recorded on this computer."
/** When /api/stt/status says not ready but gives no reason. */
const STT_NOT_READY_FALLBACK = "It isn’t set up yet."
const TRANSLATE_NOT_READY =
  "Translation needs Ollama running and its model downloaded — set it up in Settings → Translation."

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
  const [last] = useState<StartPrefs>(readLast)
  const [title, setTitle] = useState("")
  const [source, setSource] = useState<AudioSource>("mic")
  const [systemAvailable, setSystemAvailable] = useState(false)
  const [systemReason, setSystemReason] = useState("")
  const [device, setDevice] = useState(readDevicePref)
  const [devices, setDevices] = useState<Device[]>([])
  const [engine, setEngine] = useState<SttEngine>(last.engine ?? "whisper")
  const [configEngine, setConfigEngine] = useState<SttEngine>("whisper")
  const [sonioxKeySet, setSonioxKeySet] = useState<boolean | null>(null)
  const [lang, setLang] = useState(last.lang ?? "")
  const [translateTo, setTranslateTo] = useState(last.translateTo ?? "")
  const [twoWay, setTwoWay] = useState(last.twoWay ?? false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [pairError, setPairError] = useState<string | null>(null)
  const [optionsOpen, setOptionsOpen] = useState(false)
  // null = unknown (loading or the check failed): never blocks a start.
  const [sttStatus, setSttStatus] = useState<SttStatus | null>(null)
  const [translateStatus, setTranslateStatus] = useState<TranslateStatus | null>(null)
  const titleRef = useRef<HTMLInputElement>(null)
  const langRef = useRef<HTMLButtonElement | null>(null)

  // Devices + system capability (M19 contract; graceful degrade), plus
  // the config engine and Soniox key state.
  useEffect(() => {
    if (!open) return
    let cancelled = false
    getAudioSources()
      .then((s: AudioSources) => {
        if (cancelled) return
        setDevices(s.mic_devices ?? [])
        setSystemAvailable(s.system?.supported === true)
        setSystemReason(s.system?.reason || SYSTEM_UNAVAILABLE_FALLBACK)
        setSource(effectiveSource(readSourcePref(), s))
      })
      .catch(() =>
        getDevices()
          .then((ds) => {
            if (cancelled) return
            setDevices(ds)
            setSystemAvailable(false)
            setSystemReason(OLD_BACKEND_REASON)
            setSource(effectiveSource(readSourcePref(), null))
          })
          .catch(() => {
            if (!cancelled) setSource(readSourcePref())
          }),
      )
    getConfig()
      .then((c) => {
        if (cancelled) return
        setConfigEngine(c.engine === "soniox" ? "soniox" : "whisper")
        setSonioxKeySet(c.soniox_api_key_set)
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

  // Engine is only a real choice when Soniox is usable (key set);
  // otherwise the config engine runs unselected.
  const usableEngines: { value: SttEngine; label: string }[] =
    sonioxKeySet === true
      ? [
          { value: "whisper", label: ENGINE_LABEL.whisper },
          { value: "soniox", label: ENGINE_LABEL.soniox },
        ]
      : []
  const activeEngine = usableEngines.length > 0 ? engine : configEngine

  // Only an engine the status check positively reports as not ready
  // blocks Start (and "Start anyway" still gets past it).
  const engineStatus = sttStatus?.[activeEngine]
  const sttBlocked = engineStatus?.ready === false
  const sttWarning = sttBlocked
    ? `Transcription isn’t ready. ${engineStatus.reason || STT_NOT_READY_FALLBACK}`
    : null
  const canSwitchToSoniox =
    sttBlocked && activeEngine === "whisper" && usableEngines.length > 1 && sttStatus?.soniox.ready === true

  // Ollama only runs when translation is requested and the engine can't
  // translate itself (Soniox does, one-way and two-way; see
  // internal/pipeline Start and internal/stt SupportsTranslation).
  const needsOllama = (twoWay || translateTo !== "") && activeEngine !== "soniox"
  useEffect(() => {
    if (!open || !needsOllama) return
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
  }, [open, needsOllama])
  const translateWarning =
    needsOllama && translateStatus && (!translateStatus.ollama_reachable || !translateStatus.live_model.installed)
      ? TRANSLATE_NOT_READY
      : null

  const openSettings = (section: SettingsSection) => {
    onOpenChange(false)
    onOpenSettings?.(section)
  }

  const optionsSummary = [
    translateTo
      ? `${lang ? languageLabel(lang) : "Auto-detect"} ${twoWay ? "↔" : "→"} ${languageLabel(translateTo)}`
      : lang
        ? languageLabel(lang)
        : null,
    twoWay ? "two-way" : null,
    ENGINE_LABEL[activeEngine],
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
    if (twoWay && (!lang || !translateTo)) {
      setPairError("Two-way translation needs both languages. Pick the first and second language.")
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
      })
      writeLast({ lang: lang.trim(), translateTo: translateTo.trim(), twoWay, engine: activeEngine })
      writeStartedDevice(meeting.id, deviceForStart)
      onStarted(meeting)
      onOpenChange(false)
      setTitle("")
      setError(null)
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err)
      setError(`Couldn’t start the meeting: ${reason}`)
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
            <Label htmlFor="start-title">Title</Label>
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
              Optional. If left blank, it’s named after today’s date and time.
            </p>
          </div>

          <div className="grid gap-1.5">
            <span id="start-source-label" className="text-sm font-medium">
              Audio source
            </span>
            <SegmentedControl
              id="start-source"
              labelledBy="start-source-label"
              describedBy="start-source-hint"
              value={source}
              onChange={setSource}
              options={[
                { value: "mic", label: SOURCE_LABEL.mic },
                {
                  value: "system",
                  label: SOURCE_LABEL.system,
                  disabled: !systemAvailable,
                  reason: systemReason || undefined,
                },
                { value: "both", label: SOURCE_LABEL.both, disabled: !systemAvailable, reason: systemReason || undefined },
              ]}
            />
            <p id="start-source-hint" className="text-xs text-muted-foreground">
              {SOURCE_HINT}
            </p>
          </div>

          {source !== "system" && (
            <div className="grid gap-1.5">
              <Label htmlFor="start-device">Microphone</Label>
              <Select value={deviceSelectValue(startDevice, devices)} onValueChange={(v) => setDevice(v === DEFAULT_DEVICE_VALUE ? "" : v)}>
                <SelectTrigger id="start-device" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={DEFAULT_DEVICE_VALUE}>Default microphone</SelectItem>
                  {devices.map((d) => (
                    <SelectItem key={d.ID} value={d.ID}>
                      {d.Name}
                      {d.IsDefault ? " (default)" : ""}
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
                Options
                <span className="min-w-0 truncate font-normal text-muted-foreground">
                  <span aria-hidden="true">· </span>
                  {optionsSummary || "Defaults"}
                </span>
              </>
            }
          >
            <div className="grid gap-4">
              <div role="group" aria-labelledby="start-translation-label" className="grid gap-1.5">
                <div className="flex items-center justify-between">
                  <span id="start-translation-label" className="text-sm font-medium">
                    Translation
                  </span>
                  <Toggle
                    size="sm"
                    variant="outline"
                    pressed={twoWay}
                    onPressedChange={changeTwoWay}
                    aria-describedby="start-two-way-hint"
                  >
                    <ArrowLeftRight aria-hidden="true" />
                    Two-way
                  </Toggle>
                </div>
                <div className="flex items-center gap-2">
                  <Select value={lang || (twoWay ? undefined : UNSET_LANG)} onValueChange={changeLang}>
                    <SelectTrigger
                      id="start-lang"
                      ref={langRef}
                      className="w-40"
                      aria-label={twoWay ? "First language" : "Spoken language"}
                      aria-invalid={pairError && !lang ? true : undefined}
                      aria-describedby={pairError ? "start-pair-error" : undefined}
                    >
                      <SelectValue placeholder="First language" />
                    </SelectTrigger>
                    <SelectContent>
                      {!twoWay && <SelectItem value={UNSET_LANG}>Detect automatically</SelectItem>}
                      {LANGUAGES.map((l) => (
                        <SelectItem key={l.code} value={l.code}>
                          {l.label}
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
                      aria-label={twoWay ? "Second language" : "Translate into"}
                      aria-invalid={pairError && !translateTo ? true : undefined}
                      aria-describedby={pairError ? "start-pair-error" : undefined}
                    >
                      <SelectValue placeholder="Second language" />
                    </SelectTrigger>
                    <SelectContent>
                      {!twoWay && <SelectItem value={UNSET_LANG}>No translation</SelectItem>}
                      {LANGUAGES.map((l) => (
                        <SelectItem key={l.code} value={l.code}>
                          {l.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <p id="start-two-way-hint" className="text-xs text-muted-foreground">
                  {twoWay
                    ? "Whatever is said in one language is translated into the other."
                    : "People speaking two languages? Turn on two-way to translate both ways."}
                </p>
                <StatusMessage id="start-pair-error" tone="error">
                  {pairError}
                </StatusMessage>
              </div>

              {usableEngines.length > 1 && (
                <div className="grid gap-1.5">
                  <span id="start-engine-label" className="text-sm font-medium">
                    Transcribe with
                  </span>
                  <SegmentedControl
                    id="start-engine"
                    labelledBy="start-engine-label"
                    describedBy="start-engine-hint"
                    value={engine}
                    onChange={setEngine}
                    options={usableEngines}
                  />
                  <p id="start-engine-hint" className="text-xs text-muted-foreground">
                    {ENGINE_HINT[engine]}
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
                {canSwitchToSoniox && (
                  <Button type="button" size="sm" variant="outline" onClick={() => setEngine("soniox")}>
                    Use {ENGINE_LABEL.soniox} instead
                  </Button>
                )}
                {onOpenSettings && (
                  <Button type="button" size="sm" variant="outline" onClick={() => openSettings("transcription")}>
                    Open settings
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
                <Button type="button" size="sm" variant="outline" onClick={() => openSettings("translation")}>
                  Open translation settings
                </Button>
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
                Start anyway
              </Button>
            )}
            <Button
              type="submit"
              disabled={submitting || sttBlocked}
              aria-busy={submitting || undefined}
              aria-describedby={sttBlocked ? "start-stt-warning" : undefined}
            >
              {submitting && <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />}
              {submitting ? "Starting…" : "Start meeting"}
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  )
}
