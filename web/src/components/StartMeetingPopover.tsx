import { useEffect, useRef, useState, type FormEvent, type RefObject } from "react"
import { ArrowLeftRight, ChevronDown } from "lucide-react"
import { cn } from "cn"

import { SegmentedControl } from "@/components/settings/pieces"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Popover, PopoverAnchor, PopoverContent } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  getAudioSources,
  getConfig,
  getDevices,
  startMeeting,
  type AudioSources,
  type AudioSource,
  type Device,
  type StartMeetingResponse,
  type SttEngine,
} from "@/lib/api"
import { effectiveSource, readDevicePref, readSourcePref } from "@/lib/recordPrefs"
import { LANGUAGES } from "@/lib/languages"

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

function usableEngineLabel(e: SttEngine): string {
  return e === "soniox" ? "Soniox" : "Whisper"
}

interface StartMeetingPopoverProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onStarted: (meeting: StartMeetingResponse) => void
  /** Sidebar Start button the popover anchors to. */
  anchor?: RefObject<HTMLButtonElement | null>
}

export function StartMeetingPopover({ open, onOpenChange, onStarted, anchor }: StartMeetingPopoverProps) {
  const [last] = useState<StartPrefs>(readLast)
  const [title, setTitle] = useState("")
  const [source, setSource] = useState<AudioSource>("mic")
  const [systemAvailable, setSystemAvailable] = useState(false)
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
        setSource(effectiveSource(readSourcePref(), s))
      })
      .catch(() =>
        getDevices()
          .then((ds) => {
            if (cancelled) return
            setDevices(ds)
            setSystemAvailable(false)
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
    return () => {
      cancelled = true
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
          { value: "whisper", label: "Whisper" },
          { value: "soniox", label: "Soniox" },
        ]
      : []
  const activeEngine = usableEngines.length > 0 ? engine : configEngine

  const optionsSummary = [
    lang || translateTo ? `${lang || "auto"} → ${translateTo || "auto"}` : null,
    twoWay ? "two-way" : null,
    usableEngineLabel(activeEngine),
  ]
    .filter(Boolean)
    .join(" · ")

  const handleStart = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (submitting) return
    if (twoWay && (!lang || !translateTo)) {
      setPairError("Two-way translation needs both languages.")
      langRef.current?.focus()
      return
    }
    setPairError(null)
    setSubmitting(true)
    setError(null)
    // Close now rather than after the request resolves: its exit
    // transition then plays out over this (still-visible) screen instead
    // of over the Live view we're about to navigate to. Reopen on failure
    // so the inline error below still has somewhere to show.
    onOpenChange(false)
    try {
      const meeting = await startMeeting({
        title: title.trim() || defaultTitle(),
        source,
        device: source === "system" ? undefined : device || undefined,
        engine: activeEngine,
        lang: lang.trim() || undefined,
        translate_to: translateTo.trim() || undefined,
        two_way: twoWay || undefined,
      })
      writeLast({ lang: lang.trim(), translateTo: translateTo.trim(), twoWay, engine: activeEngine })
      onStarted(meeting)
      setTitle("")
      setError(null)
    } catch (err) {
      onOpenChange(true)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      {anchor && <PopoverAnchor virtualRef={anchor} />}
      <PopoverContent className="w-96" collisionPadding={16}>
        <form onSubmit={handleStart} className="grid gap-4">
          <div className="grid gap-1.5">
            <label htmlFor="start-title" className="text-sm font-medium">
              Title
            </label>
            <Input
              id="start-title"
              ref={titleRef}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={defaultTitle()}
              aria-describedby="start-title-hint"
            />
            <p id="start-title-hint" className="text-xs text-muted-foreground">
              Optional — generated from the date and time when left blank.
            </p>
          </div>

          <div className="grid gap-1.5">
            <span className="text-sm font-medium">Audio source</span>
            <SegmentedControl
              label="Audio source"
              value={source}
              onChange={setSource}
              options={[
                { value: "mic", label: "Microphone" },
                { value: "system", label: "System", disabled: !systemAvailable, title: "System audio needs a newer CatchM backend" },
                { value: "both", label: "Both", disabled: !systemAvailable, title: "System audio needs a newer CatchM backend" },
              ]}
            />
          </div>

          {source !== "system" && (
            <div className="grid gap-1.5">
              <label htmlFor="start-device" className="text-sm font-medium">
                Microphone
              </label>
              <Select value={device || undefined} onValueChange={setDevice}>
                <SelectTrigger id="start-device" className="w-full">
                  <SelectValue placeholder="Default device" />
                </SelectTrigger>
                <SelectContent>
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

          <div className="grid gap-1.5">
            <button
              type="button"
              onClick={() => setOptionsOpen((o) => !o)}
              aria-expanded={optionsOpen}
              className="flex w-full cursor-pointer items-center justify-between gap-2 rounded-md border border-border px-3 py-2 text-left text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              <span className="font-medium">Options</span>
              <span className="flex min-w-0 items-center gap-2">
                <span className="truncate text-muted-foreground">{optionsSummary || "Defaults"}</span>
                <ChevronDown className={cn("size-4 shrink-0 text-muted-foreground transition-transform", optionsOpen && "rotate-180")} />
              </span>
            </button>

            {optionsOpen && (
              <div className="grid gap-4">
                <div className="grid gap-1.5">
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium">Translation</span>
                    <button
                      type="button"
                      onClick={() => setTwoWay((v) => !v)}
                      aria-pressed={twoWay}
                      title="Two-way: both languages translate into each other"
                      className={cn(
                        "flex h-7 cursor-pointer items-center gap-1.5 rounded-md border border-border px-2 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                        twoWay ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground",
                      )}
                    >
                      <ArrowLeftRight className="size-3.5" />
                      Two-way
                    </button>
                  </div>
                  <div className="flex items-center gap-2">
                    <Select value={lang || undefined} onValueChange={setLang}>
                      <SelectTrigger
                        id="start-lang"
                        ref={langRef}
                        className="w-40"
                        aria-label={twoWay ? "Language A" : "From language"}
                        aria-describedby={pairError ? "start-pair-error" : undefined}
                      >
                        <SelectValue placeholder={twoWay ? "Language A" : "From"} />
                      </SelectTrigger>
                      <SelectContent>
                        {LANGUAGES.map((l) => (
                          <SelectItem key={l.code} value={l.code}>
                            {l.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <span aria-hidden className="text-sm text-muted-foreground">
                      →
                    </span>
                    <Select value={translateTo || undefined} onValueChange={setTranslateTo}>
                      <SelectTrigger id="start-translate-to" className="w-40" aria-label={twoWay ? "Language B" : "To language"}>
                        <SelectValue placeholder={twoWay ? "Language B" : "To"} />
                      </SelectTrigger>
                      <SelectContent>
                        {LANGUAGES.map((l) => (
                          <SelectItem key={l.code} value={l.code}>
                            {l.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  {pairError && (
                    <p id="start-pair-error" className="text-sm text-destructive" role="alert">
                      {pairError}
                    </p>
                  )}
                </div>

                {usableEngines.length > 1 && (
                  <div className="grid gap-1.5">
                    <span className="text-sm font-medium">Engine</span>
                    <SegmentedControl
                      label="Transcription engine"
                      value={engine}
                      onChange={setEngine}
                      options={usableEngines}
                    />
                  </div>
                )}
              </div>
            )}
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}

          <div className="flex justify-end">
            <Button type="submit" disabled={submitting}>
              {submitting ? "Starting…" : "Start meeting"}
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  )
}
