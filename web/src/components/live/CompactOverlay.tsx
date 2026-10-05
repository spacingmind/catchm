import { useEffect, useState } from "react"
import i18next from "i18next"
import { useTranslation } from "react-i18next"
import { Captions, Eye, EyeOff, List, Radio, X } from "lucide-react"
import { cn } from "cn"

import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
import { useStickToBottom } from "@/hooks/useStickToBottom"
import { getMeetingLevels, type AudioSource, type EngineLevelsStatus, type Utterance } from "@/lib/api"
import { sourceShortLabel } from "@/lib/labels"
import {
  readCompactLayout,
  readCompactTransparent,
  writeCompactLayout,
  writeCompactTransparent,
} from "@/lib/recordPrefs"
import type { TranscriptViewMode } from "@/lib/livePrefs"
import { compactTransparentSupported, setCompactTransparent } from "@/lib/wailsRuntime"
import { resyncWindowBackground } from "@/lib/theme"

/** How many of the most recent utterances the compact strip keeps on
 *  screen — a reading aid, not a transcript, so only the tail matters. */
const VISIBLE_LINES = 4

/** Level polling here is just for the tiny strip's dot, so a slower
 *  cadence than the full status row's ~5 Hz is enough. */
const LEVEL_POLL_MS = 1000

/** Subtitle layout: how many of the most recent lines stay on screen —
 *  bottom-anchored, centred, larger. */
const SUBTITLE_LINES = 2

/** Subtitle layout's font scale over the compact font size. */
const SUBTITLE_SCALE = 1.25

/** Subtitle layout: silence fades the lines out after this long, and
 *  the next utterance brings them back (docs/plans/active/
 *  compact-transparent.md, "Subtitle mode"). */
const SUBTITLE_IDLE_MS = 5000

/** Short engine note for the strip; null while streaming (the normal
 *  case needs no words in a 480px window). */
function engineNote(engine: EngineLevelsStatus | undefined): { text: string; error: boolean } | null {
  if (!engine || engine.state === "streaming") return null
  if (engine.state === "connecting") return { text: i18next.t("compactOverlay.connecting"), error: false }
  if (engine.state === "reconnecting") return { text: i18next.t("compactOverlay.reconnecting"), error: false }
  if (engine.state === "error")
    return {
      text: engine.error ? i18next.t("compactOverlay.stoppedWithError", { error: engine.error }) : i18next.t("compactOverlay.stopped"),
      error: true,
    }
  return null
}

interface CompactOverlayProps {
  meetingId: string
  /** Current input (tracked by LiveMeeting across mid-meeting switches). */
  audioSource: AudioSource
  utterances: Utterance[]
  viewMode: TranscriptViewMode
  /** Transcript text size (px), the same pref as the full view. */
  fontSize: number
  /** Transcript polling error from LiveMeeting. */
  error?: string | null
  /** M19 capture_error: the meeting exists but nothing is recording. */
  captureError?: string | null
  onExit: () => void
}

/** The M21 §4 compact always-on-top reading layout: a tiny status strip
 *  (recording dot, source, level, exit) plus the last few lines, shown
 *  per the current view mode. Desktop-only — mounted only while
 *  useCompactMode().compact is true, which itself never turns true
 *  outside the Wails webview. */
export function CompactOverlay({
  meetingId,
  audioSource,
  utterances,
  viewMode,
  fontSize,
  error,
  captureError,
  onExit,
}: CompactOverlayProps) {
  const { t } = useTranslation()
  const [peak, setPeak] = useState(0)
  const [engine, setEngine] = useState<EngineLevelsStatus | undefined>(undefined)
  const [transparent, setTransparent] = useState(readCompactTransparent)
  const [layout, setLayout] = useState(readCompactLayout)
  const [supported, setSupported] = useState(false)
  // Subtitle layout: idle is DERIVED (mark + current activity key) rather
  // than set in the effect, so a new utterance makes it false again
  // during render; the only setState is the timer's, per lint.
  const [idleMark, setIdleMark] = useState<{ key: string } | null>(null)

  // The see-through toggle exists only where the platform shim does
  // (macOS — DesktopBridge.CompactTransparentSupported).
  useEffect(() => {
    let cancelled = false
    void compactTransparentSupported().then((ok) => {
      if (!cancelled) setSupported(ok)
    })
    return () => {
      cancelled = true
    }
  }, [])

  // Apply the transparency pref: NSWindow state plus the
  // compact-transparent class that strips the overlay's backgrounds.
  // No cleanup here on purpose: a cleanup would run between toggles and
  // re-apply the opaque theme colour, racing the shim's clearColor (both
  // are async) and leaving a non-opaque but solid-cream window.
  useEffect(() => {
    if (!supported) return
    document.documentElement.classList.toggle("compact-transparent", transparent)
    setCompactTransparent(transparent)
    if (!transparent) resyncWindowBackground()
  }, [supported, transparent])

  // Leaving compact mode: drop the class. useCompactMode's exit() makes
  // the window opaque and re-syncs its colour.
  useEffect(() => () => document.documentElement.classList.remove("compact-transparent"), [])

  // Subtitle layout fade: reset on every new utterance (the length and
  // the last seq together, so an in-place update of the tail also
  // counts), cleared on unmount or the next change.
  const lastSeq = utterances.length ? utterances[utterances.length - 1].seq : 0
  const subtitle = layout === "subtitle"
  const activityKey = `${utterances.length}:${lastSeq}`
  const idle = idleMark?.key === activityKey
  useEffect(() => {
    if (!subtitle) return
    const id = setTimeout(() => setIdleMark({ key: activityKey }), SUBTITLE_IDLE_MS)
    return () => clearTimeout(id)
  }, [subtitle, activityKey])

  useEffect(() => {
    let cancelled = false
    const poll = () => {
      getMeetingLevels(meetingId)
        .then((lv) => {
          if (cancelled) return
          setPeak(Math.max(lv.mic?.peak ?? 0, lv.system?.peak ?? 0))
          setEngine(lv.engine)
        })
        .catch(() => undefined)
    }
    poll()
    const id = setInterval(poll, LEVEL_POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [meetingId])

  const lines = utterances.slice(subtitle ? -SUBTITLE_LINES : -VISIBLE_LINES)
  const { ref: bottomRef } = useStickToBottom(utterances.length)
  const note = engineNote(engine)
  const problem = captureError
    ? t("compactOverlay.notRecordingWith", { error: captureError })
    : error
      ? t("compactOverlay.notUpdatingWith", { error })
      : null

  // Subtitle layout: lines fade out on silence. Transparency: the strip
  // and the error line appear on hover or focus within (keyboard users
  // reach them) — two independent options.
  const fade = cn(subtitle && "motion-safe:transition-opacity motion-safe:duration-700", subtitle && idle && "opacity-0")
  const reveal = "opacity-0 motion-safe:transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"

  return (
    <div data-compact-root className="group flex h-svh flex-col bg-background px-3 py-2 text-foreground">
      {/* select-none (M22 §4): status chrome only — the transcript lines
          below keep the default selectable text. Subtitle mode hides the
          strip and the error line unless hovered/focused (kept in the
          DOM: the buttons stay focusable and screen-reader reachable). */}
      <div
        className={cn(
          "mb-1.5 flex shrink-0 items-center gap-2 border-b border-border pb-1.5 text-xs text-muted-foreground select-none",
          transparent && supported && reveal,
        )}
      >
        <span className="relative inline-flex shrink-0">
          <Radio aria-hidden="true" className={cn("size-3.5", captureError ? "text-destructive" : "text-primary")} />
          {!captureError && (
            <span
              aria-hidden="true"
              className="absolute -top-0.5 -right-0.5 size-1 motion-safe:animate-pulse rounded-full bg-primary"
            />
          )}
          <span className="sr-only">{captureError ? t("compactOverlay.notRecordingSr") : t("compactOverlay.recordingSr")}</span>
        </span>
        <span className="shrink-0">
          <span className="sr-only">{t("compactOverlay.sourceSr")} </span>
          {sourceShortLabel(audioSource)}
        </span>
        <div aria-hidden="true" className="h-1 w-10 shrink-0 overflow-hidden rounded-full bg-muted">
          <div
            className="h-full origin-left bg-primary motion-safe:transition-transform"
            style={{ transform: `scaleX(${Math.min(1, Math.max(0, peak))})` }}
          />
        </div>
        <span
          role="status"
          title={note?.text}
          className={cn("min-w-0 truncate", note?.error ? "text-destructive" : "text-warning")}
        >
          {note?.text}
        </span>
        <Button
          size="icon"
          variant="ghost"
          className="ml-auto shrink-0"
          aria-pressed={layout === "subtitle"}
          aria-label={t("compactOverlay.subtitleLayout")}
          title={layout === "subtitle" ? t("compactOverlay.subtitleOff") : t("compactOverlay.subtitleOn")}
          onClick={() => {
            const next = layout === "subtitle" ? "list" : "subtitle"
            writeCompactLayout(next)
            setLayout(next)
          }}
        >
          {layout === "subtitle" ? <List className="size-3.5" /> : <Captions className="size-3.5" />}
        </Button>
        {supported && (
          <Button
            size="icon"
            variant="ghost"
            className="shrink-0"
            aria-pressed={transparent}
            aria-label={t("compactOverlay.transparentOn")}
            title={transparent ? t("compactOverlay.transparentOff") : t("compactOverlay.transparentOn")}
            onClick={() => {
              const next = !transparent
              writeCompactTransparent(next)
              setTransparent(next)
            }}
          >
            {transparent ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
          </Button>
        )}
        <Button size="icon" variant="ghost" className="shrink-0" aria-label={t("compactOverlay.exitAria")} title={t("compactOverlay.exitAria")} onClick={onExit}>
          <X className="size-3.5" />
        </Button>
      </div>

      <StatusMessage
        tone="error"
        className={cn(
          "mb-1.5 line-clamp-2 shrink-0 break-words text-xs",
          transparent && supported && reveal,
        )}
      >
        {problem}
      </StatusMessage>

      <div
        className={cn("min-h-0 flex-1 overflow-y-auto", subtitle && "flex flex-col justify-end text-center")}
        style={{ fontSize: subtitle ? Math.round(fontSize * SUBTITLE_SCALE) : fontSize }}
      >
        <div
          role="log"
          aria-label={t("compactOverlay.logAria")}
          className={cn("grid gap-2", fade)}
        >
          {lines.length === 0 ? (
            <p className="text-muted-foreground">{t("compactOverlay.waiting")}</p>
          ) : (
            lines.map((u) => (
              <div key={u.seq} className="leading-snug">
                {viewMode === "translation-only" ? (
                  <p className="break-words">{u.translated || u.text}</p>
                ) : viewMode === "side-by-side" ? (
                  <div className={cn("grid gap-2", subtitle ? "grid-cols-1" : "grid-cols-2")}>
                    <p className="min-w-0 break-words">{u.text}</p>
                    {u.translated && (
                      <p className="min-w-0 break-words text-muted-foreground">
                        {u.translated}
                      </p>
                    )}
                  </div>
                ) : (
                  <>
                    <p className="break-words">{u.text}</p>
                    {u.translated && (
                      <p className="break-words text-muted-foreground">
                        {u.translated}
                      </p>
                    )}
                  </>
                )}
              </div>
            ))
          )}
        </div>
        <div ref={bottomRef} />
      </div>
    </div>
  )
}
