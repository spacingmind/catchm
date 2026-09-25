import { useEffect, useRef, useState } from "react"
import { AlertTriangle } from "lucide-react"
import { cn } from "cn"

import { SegmentedControl } from "@/components/settings/pieces"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import {
  getAudioSources,
  getMeetingLevels,
  setMeetingSource,
  type AudioSource,
  type Device,
  type EngineLevelsStatus,
  type MeetingLevels,
} from "@/lib/api"

/** GET /api/meetings/{id}/levels polling rate (M21 §1: "~5 Hz"). */
const LEVELS_POLL_MS = 200

/** How long a source may sit near-zero before the "No signal" hint
 *  shows — long enough that normal pauses between sentences don't
 *  flicker it on and off. */
const NO_SIGNAL_AFTER_MS = 4000

/** How long the drops warning stays up after the last increase, so a
 *  single burst doesn't flash on and off between polls. */
const DROPS_WARNING_HOLD_MS = 5000

const SOURCE_OPTIONS: { value: AudioSource; label: string }[] = [
  { value: "mic", label: "Mic" },
  { value: "system", label: "System" },
  { value: "both", label: "Both" },
]

function formatElapsed(startedAt: string): string {
  const ms = Date.now() - new Date(startedAt).getTime()
  const total = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const pad = (n: number) => String(n).padStart(2, "0")
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}

function engineLabel(engine: EngineLevelsStatus | undefined): { label: string; className: string } {
  if (!engine) {
    return { label: "Transcription: unknown", className: "border-border text-muted-foreground" }
  }
  switch (engine.state) {
    case "streaming":
      return { label: "Transcription connected", className: "border-transparent bg-primary/15 text-primary" }
    case "connecting":
    case "reconnecting":
      return {
        label: engine.state === "connecting" ? "Transcription connecting…" : "Transcription reconnecting…",
        className: "border-amber-500/30 bg-amber-500/15 text-amber-700",
      }
    case "error":
      return {
        label: engine.error ? `Transcription error: ${engine.error}` : "Transcription error",
        className: "border-transparent bg-destructive/10 text-destructive",
      }
    default:
      return { label: `Transcription: ${engine.state}`, className: "border-border text-muted-foreground" }
  }
}

/** Compact horizontal bar meter, filled by peak (0-1). */
function LevelMeter({ label, levels, noSignal }: { label: string; levels: { rms: number; peak: number } | undefined; noSignal: boolean }) {
  const pct = Math.min(100, Math.round((levels?.peak ?? 0) * 100))
  return (
    <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
      <span className="w-14 shrink-0">{label}</span>
      <div className="h-1.5 w-16 shrink-0 overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-primary transition-[width]" style={{ width: `${pct}%` }} />
      </div>
      {noSignal && <span className="shrink-0 text-muted-foreground/70">No signal</span>}
    </div>
  )
}

interface StatusRowProps {
  meetingId: string
  startedAt: string
  initialSource: AudioSource
}

export function StatusRow({ meetingId, startedAt, initialSource }: StatusRowProps) {
  const [elapsed, setElapsed] = useState(() => formatElapsed(startedAt))
  const [source, setSource] = useState<AudioSource>(initialSource)
  const [device, setDevice] = useState("")
  const [devices, setDevices] = useState<Device[]>([])
  const [systemAvailable, setSystemAvailable] = useState(false)
  const [systemReason, setSystemReason] = useState("")
  const [switching, setSwitching] = useState(false)
  const [sourceError, setSourceError] = useState<string | null>(null)

  const [levels, setLevels] = useState<MeetingLevels | null>(null)
  const [dropsWarning, setDropsWarning] = useState(false)
  const lastDropsRef = useRef({ ticks: 0, micChunks: 0, micSamples: 0, sysChunks: 0, sysSamples: 0 })
  const dropsWarningUntilRef = useRef(0)
  const lastSignalRef = useRef<{ mic: number; system: number }>({ mic: 0, system: 0 })

  // Elapsed time ticker.
  useEffect(() => {
    const id = setInterval(() => setElapsed(formatElapsed(startedAt)), 1000)
    return () => clearInterval(id)
  }, [startedAt])

  // Devices + system capability, for the source switch and mic select.
  useEffect(() => {
    let cancelled = false
    getAudioSources()
      .then((s) => {
        if (cancelled) return
        setDevices(s.mic_devices ?? [])
        setSystemAvailable(s.system?.supported === true)
        setSystemReason(s.system?.reason ?? "")
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [])

  // Level meters + drops + engine state: polled at ~5 Hz, paused while
  // the tab isn't visible (M21 §1).
  useEffect(() => {
    let cancelled = false
    let id: number | undefined

    const poll = () => {
      getMeetingLevels(meetingId)
        .then((lv) => {
          if (cancelled) return
          setLevels(lv)

          const now = Date.now()
          if ((lv.mic?.peak ?? 0) > 0.01) lastSignalRef.current.mic = now
          if ((lv.system?.peak ?? 0) > 0.01) lastSignalRef.current.system = now

          const prev = lastDropsRef.current
          const increased =
            lv.dropped_ticks > prev.ticks ||
            (lv.mic?.dropped_chunks ?? 0) > prev.micChunks ||
            (lv.mic?.dropped_samples ?? 0) > prev.micSamples ||
            (lv.system?.dropped_chunks ?? 0) > prev.sysChunks ||
            (lv.system?.dropped_samples ?? 0) > prev.sysSamples
          lastDropsRef.current = {
            ticks: lv.dropped_ticks,
            micChunks: lv.mic?.dropped_chunks ?? 0,
            micSamples: lv.mic?.dropped_samples ?? 0,
            sysChunks: lv.system?.dropped_chunks ?? 0,
            sysSamples: lv.system?.dropped_samples ?? 0,
          }
          if (increased) dropsWarningUntilRef.current = now + DROPS_WARNING_HOLD_MS
          setDropsWarning(now < dropsWarningUntilRef.current)
        })
        .catch(() => {
          if (!cancelled) setLevels(null)
        })
    }

    const start = () => {
      poll()
      id = window.setInterval(poll, LEVELS_POLL_MS)
    }
    const stop = () => {
      if (id !== undefined) window.clearInterval(id)
      id = undefined
    }

    if (document.visibilityState === "visible") start()
    const onVisibility = () => {
      if (document.visibilityState === "visible") start()
      else stop()
    }
    document.addEventListener("visibilitychange", onVisibility)

    return () => {
      cancelled = true
      stop()
      document.removeEventListener("visibilitychange", onVisibility)
    }
  }, [meetingId])

  const handleSourceChange = async (next: AudioSource) => {
    if (next === source || switching) return
    setSwitching(true)
    setSourceError(null)
    try {
      await setMeetingSource(meetingId, next, next === "system" ? undefined : device || undefined)
      setSource(next)
    } catch (err) {
      // 422: the old input stays live server-side — keep the old
      // selection here too and surface the reason inline.
      setSourceError(err instanceof Error ? err.message : String(err))
    } finally {
      setSwitching(false)
    }
  }

  // Ctrl/⌘+1/2/3 while the Live view is mounted (my-translator does
  // this); ignore when a modifier we don't handle is also held.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey) return
      const map: Record<string, AudioSource> = { "1": "mic", "2": "system", "3": "both" }
      const next = map[e.key]
      if (!next) return
      if ((next === "system" || next === "both") && !systemAvailable) return
      e.preventDefault()
      void handleSourceChange(next)
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [systemAvailable, source, device, switching])

  const engine = engineLabel(levels?.engine)
  const now = Date.now()
  const micNoSignal = source !== "system" && now - lastSignalRef.current.mic > NO_SIGNAL_AFTER_MS
  const sysNoSignal = source !== "mic" && now - lastSignalRef.current.system > NO_SIGNAL_AFTER_MS

  return (
    <div className="mb-4 grid gap-2 rounded-xl border border-border bg-card px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <span className="font-mono text-sm tabular-nums text-muted-foreground" aria-label="Elapsed time">
          {elapsed}
        </span>

        <SegmentedControl
          label="Audio source"
          value={source}
          onChange={(v) => void handleSourceChange(v)}
          options={SOURCE_OPTIONS.map((o) => ({
            ...o,
            disabled: (o.value === "system" || o.value === "both") && !systemAvailable,
            title: (o.value === "system" || o.value === "both") && !systemAvailable ? systemReason : undefined,
          }))}
        />

        {source !== "system" && devices.length > 0 && (
          <Select value={device || undefined} onValueChange={setDevice}>
            <SelectTrigger className="h-8 w-48" aria-label="Microphone">
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
        )}

        <span className={cn("inline-flex h-5 items-center rounded-4xl border px-2 text-xs font-medium", engine.className)}>
          {engine.label}
        </span>
      </div>

      {sourceError && (
        <p role="alert" className="text-sm text-destructive">
          Couldn't switch source: {sourceError}
        </p>
      )}

      <div className="flex flex-wrap items-center gap-4">
        {source !== "system" && <LevelMeter label="Mic" levels={levels?.mic} noSignal={micNoSignal} />}
        {source !== "mic" && <LevelMeter label="System" levels={levels?.system} noSignal={sysNoSignal} />}
      </div>

      {dropsWarning && (
        <p className="flex items-center gap-1.5 text-xs font-medium text-amber-700">
          <AlertTriangle className="size-3.5" />
          Audio is being dropped — connection or machine too slow.
        </p>
      )}
    </div>
  )
}
