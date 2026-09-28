import { useEffect, useState } from "react"
import { Radio, X } from "lucide-react"
import { cn } from "cn"

import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
import { useStickToBottom } from "@/hooks/useStickToBottom"
import { getMeetingLevels, type AudioSource, type EngineLevelsStatus, type Utterance } from "@/lib/api"
import { SOURCE_SHORT_LABEL } from "@/lib/labels"
import { languageLabel } from "@/lib/languages"
import type { TranscriptViewMode } from "@/lib/livePrefs"

/** How many of the most recent utterances the compact strip keeps on
 *  screen — a reading aid, not a transcript, so only the tail matters. */
const VISIBLE_LINES = 4

/** Level polling here is just for the tiny strip's dot, so a slower
 *  cadence than the full status row's ~5 Hz is enough. */
const LEVEL_POLL_MS = 1000

/** Short engine note for the strip; null while streaming (the normal
 *  case needs no words in a 480px window). */
function engineNote(engine: EngineLevelsStatus | undefined): { text: string; error: boolean } | null {
  if (!engine || engine.state === "streaming") return null
  if (engine.state === "connecting") return { text: "Connecting…", error: false }
  if (engine.state === "reconnecting") return { text: "Reconnecting…", error: false }
  if (engine.state === "error") return { text: engine.error ? `Transcription stopped: ${engine.error}` : "Transcription stopped", error: true }
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
  const [peak, setPeak] = useState(0)
  const [engine, setEngine] = useState<EngineLevelsStatus | undefined>(undefined)

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

  const lines = utterances.slice(-VISIBLE_LINES)
  const { ref: bottomRef } = useStickToBottom(utterances.length)
  const note = engineNote(engine)
  const problem = captureError ? `Not recording: ${captureError}` : error ? `Transcript not updating: ${error}` : null

  return (
    <div className="flex h-svh flex-col bg-background px-3 py-2 text-foreground">
      {/* select-none (M22 §4): status chrome only — the transcript lines
          below keep the default selectable text. */}
      <div className="mb-1.5 flex shrink-0 items-center gap-2 border-b border-border pb-1.5 text-xs text-muted-foreground select-none">
        <span className="relative inline-flex shrink-0">
          <Radio aria-hidden="true" className={cn("size-3.5", captureError ? "text-destructive" : "text-primary")} />
          {!captureError && (
            <span
              aria-hidden="true"
              className="absolute -top-0.5 -right-0.5 size-1 motion-safe:animate-pulse rounded-full bg-primary"
            />
          )}
          <span className="sr-only">{captureError ? "Not recording" : "Recording"}</span>
        </span>
        <span className="shrink-0">
          <span className="sr-only">Source: </span>
          {SOURCE_SHORT_LABEL[audioSource]}
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
        <Button size="icon" variant="ghost" className="ml-auto shrink-0" aria-label="Back to full window" title="Back to full window" onClick={onExit}>
          <X className="size-3.5" />
        </Button>
      </div>

      <StatusMessage tone="error" className="mb-1.5 line-clamp-2 shrink-0 break-words text-xs">
        {problem}
      </StatusMessage>

      <div className="min-h-0 flex-1 overflow-y-auto" style={{ fontSize }}>
        <div role="log" aria-label="Latest transcript lines" className="grid gap-2">
          {lines.length === 0 ? (
            <p className="text-muted-foreground">Waiting for someone to speak…</p>
          ) : (
            lines.map((u) => (
              <div key={u.seq} className="leading-snug">
                {viewMode === "translation-only" ? (
                  <p className="break-words">{u.translated || u.text}</p>
                ) : viewMode === "side-by-side" ? (
                  <div className="grid grid-cols-2 gap-2">
                    <p className="min-w-0 break-words">{u.text}</p>
                    {u.translated && (
                      <p className="min-w-0 break-words text-muted-foreground">
                        {u.lang && (
                          <span className="mr-1 text-xs text-muted-foreground">
                            <span aria-hidden="true">→</span> {languageLabel(u.lang)}
                          </span>
                        )}
                        {u.translated}
                      </p>
                    )}
                  </div>
                ) : (
                  <>
                    <p className="break-words">{u.text}</p>
                    {u.translated && (
                      <p className="break-words text-muted-foreground">
                        {u.lang && (
                          <span className="mr-1 text-xs text-muted-foreground">
                            <span aria-hidden="true">→</span> {languageLabel(u.lang)}
                          </span>
                        )}
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
