import { useEffect, useState } from "react"
import { Radio, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import { getMeetingLevels, type AudioSource, type Utterance } from "@/lib/api"
import type { TranscriptViewMode } from "@/lib/livePrefs"

/** How many of the most recent utterances the compact strip keeps on
 *  screen — a reading aid, not a transcript, so only the tail matters. */
const VISIBLE_LINES = 4

/** Level polling here is just for the tiny strip's dot, so a slower
 *  cadence than the full status row's ~5 Hz is enough. */
const LEVEL_POLL_MS = 1000

interface CompactOverlayProps {
  meetingId: string
  audioSource: AudioSource
  utterances: Utterance[]
  viewMode: TranscriptViewMode
  onExit: () => void
}

/** The M21 §4 compact always-on-top reading layout: a tiny status strip
 *  (recording dot, source, level, exit) plus the last few lines, shown
 *  per the current view mode. Desktop-only — mounted only while
 *  useCompactMode().compact is true, which itself never turns true
 *  outside the Wails webview. */
export function CompactOverlay({ meetingId, audioSource, utterances, viewMode, onExit }: CompactOverlayProps) {
  const [peak, setPeak] = useState(0)

  useEffect(() => {
    let cancelled = false
    const poll = () => {
      getMeetingLevels(meetingId)
        .then((lv) => {
          if (cancelled) return
          setPeak(Math.max(lv.mic?.peak ?? 0, lv.system?.peak ?? 0))
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

  return (
    <div className="flex h-svh flex-col bg-background px-3 py-2 text-foreground">
      <div className="mb-1.5 flex shrink-0 items-center gap-2 border-b border-border pb-1.5 text-xs text-muted-foreground">
        <span className="relative inline-flex shrink-0">
          <Radio className="size-3.5 text-primary" />
          <span className="absolute -top-0.5 -right-0.5 size-1 motion-safe:animate-pulse rounded-full bg-primary" />
        </span>
        <span className="capitalize">{audioSource}</span>
        <div className="h-1 w-10 overflow-hidden rounded-full bg-muted">
          <div className="h-full bg-primary transition-[width]" style={{ width: `${Math.min(100, Math.round(peak * 100))}%` }} />
        </div>
        <Button size="icon-sm" variant="ghost" className="ml-auto size-5" aria-label="Exit compact mode" onClick={onExit}>
          <X className="size-3.5" />
        </Button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {lines.length === 0 ? (
          <p className="text-sm text-muted-foreground">Waiting for the first utterance…</p>
        ) : (
          <div className="grid gap-2">
            {lines.map((u) => (
              <div key={u.seq} className="text-sm leading-snug">
                {viewMode === "translation-only" ? (
                  <p className="break-words">{u.translated || u.text}</p>
                ) : viewMode === "side-by-side" ? (
                  <div className="grid grid-cols-2 gap-2">
                    <p className="break-words">{u.text}</p>
                    {u.translated && <p className="break-words text-muted-foreground">{u.translated}</p>}
                  </div>
                ) : (
                  <>
                    <p className="break-words">{u.text}</p>
                    {u.translated && <p className="break-words text-muted-foreground">{u.translated}</p>}
                  </>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
