import { useEffect, useRef, useState, type FormEvent, type RefObject } from "react"
import { ArrowLeftRight } from "lucide-react"
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
  const titleRef = useRef<HTMLInputElement>(null)

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

  const handleStart = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (submitting) return
    if (twoWay && (!lang.trim() || !translateTo.trim())) {
      setError("Two-way translation needs both languages.")
      return
    }
    setSubmitting(true)
    setError(null)
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
      onOpenChange(false)
      setTitle("")
      setError(null)
    } catch (err) {
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
                  <SelectValue placeholder={devices.length === 0 ? "Default device" : "Default device"} />
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
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">Translation</span>
              <button
                type="button"
                onClick={() => setTwoWay((v) => !v)}
                aria-pressed={twoWay}
                title="Two-way: both languages translate into each other"
                className={cn(
                  "flex h-7 items-center gap-1.5 rounded-md border border-border px-2 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                  twoWay ? "bg-muted text-foreground" : "text-muted-foreground hover:text-foreground",
                )}
              >
                <ArrowLeftRight className="size-3.5" />
                Two-way
              </button>
            </div>
            <div className="flex items-center gap-2">
              <Input
                id="start-lang"
                value={lang}
                onChange={(e) => setLang(e.target.value)}
                placeholder={twoWay ? "Language A, e.g. en" : "From, e.g. en"}
                aria-label={twoWay ? "Language A" : "Source language"}
                className="w-36"
              />
              <span aria-hidden className="text-sm text-muted-foreground">
                →
              </span>
              <Input
                id="start-translate-to"
                value={translateTo}
                onChange={(e) => setTranslateTo(e.target.value)}
                placeholder={twoWay ? "Language B, e.g. vi" : "To, e.g. es"}
                aria-label={twoWay ? "Language B" : "Translate to"}
                className="w-36"
              />
            </div>
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
