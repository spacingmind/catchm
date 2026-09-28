import { useEffect, useRef, useState } from "react"
import { AlertTriangle } from "lucide-react"
import { cn } from "cn"

import { SegmentedControl } from "@/components/settings/pieces"
import { StatusMessage } from "@/components/StatusMessage"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
import { SOURCE_LABEL, SOURCE_SHORT_LABEL } from "@/lib/labels"
import { DEFAULT_DEVICE_VALUE, deviceSelectValue } from "@/lib/recordPrefs"

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
  { value: "mic", label: SOURCE_LABEL.mic },
  { value: "system", label: SOURCE_LABEL.system },
  { value: "both", label: SOURCE_LABEL.both },
]

/** Shown when the backend says system audio is unsupported but gives no
 *  reason. */
const SYSTEM_UNAVAILABLE_FALLBACK = "Computer audio isn’t available on this computer."

/** Shortcuts as the user would type them: ⌘ only on macOS (the key
 *  handlers accept either modifier). */
const IS_MAC = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform)
const MOD = IS_MAC ? "⌘" : "Ctrl+"
const COMPACT_SHORTCUT = IS_MAC ? "⌘⇧L" : "Ctrl+Shift+L"

/** The Ctrl/⌘+1/2/3 source shortcuts, as a hover hint on the source
 *  control (SegmentedControl has no per-option tooltip). */
const SOURCE_SHORTCUTS_HINT = `Shortcuts: ${MOD}1 ${SOURCE_LABEL.mic}, ${MOD}2 ${SOURCE_LABEL.system}, ${MOD}3 ${SOURCE_LABEL.both}`

/** What to do about an engine error, per engine: the local Whisper
 *  server and the Soniox cloud fail for different reasons. */
function engineErrorHint(engine: string | undefined): string {
  if (engine === "soniox") {
    return "Check your internet connection and the Soniox API key in Settings → Transcription."
  }
  if (engine === "whisper") {
    return "The Whisper server on this computer isn’t responding. Set it up, or switch to the cloud engine, in Settings → Transcription."
  }
  return "Check the transcription settings in Settings → Transcription."
}

/** A server message as a sentence, so a next step can follow it. */
function sentence(msg: string): string {
  const t = msg.trim()
  return /[.!?]$/.test(t) ? t : `${t}.`
}

function formatElapsed(startedAt: string): string {
  const ms = Date.now() - new Date(startedAt).getTime()
  const total = Math.max(0, Math.floor(ms / 1000))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const pad = (n: number) => String(n).padStart(2, "0")
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}

/** Short pill label + tone for the engine state. The raw error text is
 *  shown in full on its own line, not squeezed into the pill. */
function engineLabel(engine: EngineLevelsStatus | undefined): { label: string; className: string } {
  if (!engine) {
    return { label: "Transcription: checking…", className: "border-border text-muted-foreground" }
  }
  switch (engine.state) {
    case "streaming":
      return { label: "Transcription on", className: "border-transparent bg-primary/15 text-primary" }
    case "connecting":
    case "reconnecting":
      return {
        label: engine.state === "connecting" ? "Transcription connecting…" : "Transcription reconnecting…",
        className: "border-warning/30 bg-warning/10 text-warning",
      }
    case "error":
      return { label: "Transcription error", className: "border-transparent bg-destructive/10 text-destructive" }
    default:
      return { label: "Transcription: checking…", className: "border-border text-muted-foreground" }
  }
}

/** Compact horizontal bar meter, filled by peak (0-1). Decorative — the
 *  "No signal" text carries the meaning for assistive tech. */
function LevelMeter({ label, levels, noSignal }: { label: string; levels: { rms: number; peak: number } | undefined; noSignal: boolean }) {
  const fill = Math.min(1, Math.max(0, levels?.peak ?? 0))
  return (
    <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
      <span className="w-16 shrink-0">{label}</span>
      <div aria-hidden="true" className="h-1.5 w-16 shrink-0 overflow-hidden rounded-full bg-muted">
        <div
          className="h-full origin-left rounded-full bg-primary motion-safe:transition-transform"
          style={{ transform: `scaleX(${fill})` }}
        />
      </div>
      {noSignal && <span className="shrink-0 text-muted-foreground">No signal</span>}
    </div>
  )
}

interface StatusRowProps {
  meetingId: string
  startedAt: string
  /** The meeting's engine id ("whisper" / "soniox"), for the error hint. */
  engine?: string
  /** Current input, owned by LiveMeeting so it survives this row
   *  unmounting (compact mode) and the compact strip can show it. */
  source: AudioSource
  /** Current mic device ID; "" = default device. */
  device: string
  /** Called after the server accepted a source or mic switch. */
  onSourceChange: (source: AudioSource, device: string) => void
  /** Shown as an "Open settings" button next to an engine error. */
  onOpenTranscriptionSettings?: () => void
  /** M21 §4: desktop-only compact overlay toggle. Omit both (or leave
   *  compactAvailable false) to render no button at all — a plain
   *  browser (catchm serve) never gets one, not just a disabled one. */
  compactAvailable?: boolean
  onToggleCompact?: () => void
}

export function StatusRow({
  meetingId,
  startedAt,
  engine: engineId,
  source,
  device,
  onSourceChange,
  onOpenTranscriptionSettings,
  compactAvailable,
  onToggleCompact,
}: StatusRowProps) {
  const [elapsed, setElapsed] = useState(() => formatElapsed(startedAt))
  const [devices, setDevices] = useState<Device[]>([])
  const [systemAvailable, setSystemAvailable] = useState(false)
  const [systemReason, setSystemReason] = useState("")
  // The switch in flight (non-null = switching): the controls show the
  // requested value until the server answers, then snap to the real one.
  const [pending, setPending] = useState<{ source: AudioSource; device: string } | null>(null)
  const [switchError, setSwitchError] = useState<string | null>(null)

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
      // Already polling: a repeated "visible" event must not stack a
      // second interval, which cleanup (holding only the last id)
      // would leak past unmount.
      if (id !== undefined) return
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

  /** POST the switch. The server accepts the same source with a new
   *  device (it reopens the mic), so this also serves the mic select. */
  const switchInput = async (nextSource: AudioSource, nextDevice: string, what: "source" | "microphone") => {
    if (pending || (nextSource === source && nextDevice === device)) return
    setPending({ source: nextSource, device: nextDevice })
    setSwitchError(null)
    try {
      await setMeetingSource(meetingId, nextSource, nextSource === "system" ? undefined : nextDevice || undefined)
      onSourceChange(nextSource, nextDevice)
    } catch (err) {
      // 422: the old input stays live server-side — keep the old
      // selection here too and surface the reason inline.
      setSwitchError(`Couldn’t switch ${what}: ${sentence(err instanceof Error ? err.message : String(err))} Try another input.`)
    } finally {
      setPending(null)
    }
  }

  const handleSourceChange = (next: AudioSource) => switchInput(next, device, "source")
  const handleDeviceChange = (value: string) =>
    switchInput(source, value === DEFAULT_DEVICE_VALUE ? "" : value, "microphone")

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
  }, [systemAvailable, source, device, pending])

  const shownSource = pending?.source ?? source
  const shownDevice = pending?.device ?? device
  const engine = engineLabel(levels?.engine)
  const engineError = levels?.engine?.state === "error" ? levels.engine.error : undefined
  const unavailableReason = systemReason || SYSTEM_UNAVAILABLE_FALLBACK
  const now = Date.now()
  const micNoSignal = source !== "system" && now - lastSignalRef.current.mic > NO_SIGNAL_AFTER_MS
  const sysNoSignal = source !== "mic" && now - lastSignalRef.current.system > NO_SIGNAL_AFTER_MS

  return (
    <div className="mb-4 grid gap-2 rounded-xl border border-border bg-card px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        {/* role=timer is not announced every tick (implicit aria-live
            off); the sr-only prefix names the value when read. */}
        <span role="timer" className="font-mono text-sm tabular-nums text-muted-foreground">
          <span className="sr-only">Elapsed time </span>
          {elapsed}
        </span>

        <div title={SOURCE_SHORTCUTS_HINT}>
          <SegmentedControl
            id="live-source"
            label="Audio source"
            value={shownSource}
            onChange={(v) => void handleSourceChange(v)}
            options={SOURCE_OPTIONS.map((o) => {
              const disabled = (o.value === "system" || o.value === "both") && !systemAvailable
              return { ...o, disabled, reason: disabled ? unavailableReason : undefined }
            })}
          />
        </div>

        {shownSource !== "system" && devices.length > 0 && (
          <Select
            value={deviceSelectValue(shownDevice, devices)}
            onValueChange={(v) => void handleDeviceChange(v)}
          >
            <SelectTrigger className="h-8 w-48" aria-label="Microphone">
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
        )}

        <Badge
          role="status"
          variant="outline"
          className={cn("max-w-64", engine.className)}
          aria-describedby={engineError ? "live-engine-error" : undefined}
        >
          <span className="truncate">{engine.label}</span>
        </Badge>

        {compactAvailable && onToggleCompact && (
          <Button
            size="sm"
            variant="outline"
            className="ml-auto"
            onClick={onToggleCompact}
            title={`Shrink to a small window that stays on top of other apps (${COMPACT_SHORTCUT})`}
            aria-keyshortcuts={IS_MAC ? "Meta+Shift+L" : "Control+Shift+L"}
          >
            Mini window
          </Button>
        )}
      </div>

      {engineError && (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p id="live-engine-error" className="min-w-0 break-words text-sm text-destructive">
            Transcription stopped working: {sentence(engineError)} {engineErrorHint(engineId)}
          </p>
          {onOpenTranscriptionSettings && (
            <Button size="sm" variant="outline" onClick={onOpenTranscriptionSettings}>
              Open settings
            </Button>
          )}
        </div>
      )}

      <StatusMessage tone={switchError ? "error" : "muted"} className="break-words">
        {switchError ?? (pending ? "Switching input…" : null)}
      </StatusMessage>

      <div className="flex flex-wrap items-center gap-4">
        {source !== "system" && <LevelMeter label={SOURCE_SHORT_LABEL.mic} levels={levels?.mic} noSignal={micNoSignal} />}
        {source !== "mic" && <LevelMeter label={SOURCE_SHORT_LABEL.system} levels={levels?.system} noSignal={sysNoSignal} />}
      </div>

      <StatusMessage tone="warning" className="flex items-center gap-1.5 text-xs font-medium">
        {dropsWarning && (
          <>
            <AlertTriangle aria-hidden="true" className="size-3.5 shrink-0" />
            Some audio is being lost because this computer or the connection can’t keep up. Close other busy apps or
            check your internet connection.
          </>
        )}
      </StatusMessage>
    </div>
  )
}
