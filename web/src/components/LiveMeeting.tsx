import { useEffect, useRef, useState } from "react"
import { ArrowDown, Radio } from "lucide-react"

import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import { addNote, endMeeting, getTranscript, type Meeting, type Utterance } from "@/lib/api"

/** How often the live transcript is polled for new utterances. */
const POLL_INTERVAL_MS = 1500

/**
 * Distance from the bottom (px) within which the view counts as
 * "at the bottom" for auto-scroll purposes. Generous enough that the
 * common case (watching live, one new utterance renders, user is now
 * ~one line above the bottom) keeps auto-scrolling; small enough that
 * anyone who scrolled up to re-read (a screenful or more) is clearly
 * outside it.
 */
const AT_BOTTOM_THRESHOLD_PX = 120

interface LiveMeetingProps {
  meeting: Meeting
  onEnded: (meeting: Meeting) => void
  /** M19 capture_error from the start response: the meeting exists but
   *  nothing is being recorded. Must stay visible, never a silent no-op. */
  captureError?: string | null
  /** Opens settings so the user can fix the cause (e.g. missing API key). */
  onOpenSettings?: () => void
}

export function LiveMeeting({ meeting, onEnded, captureError, onOpenSettings }: LiveMeetingProps) {
  const [utterances, setUtterances] = useState<Utterance[]>([])
  const cursorRef = useRef(-1)
  const bottomRef = useRef<HTMLDivElement>(null)
  // The element that actually scrolls inside Radix's ScrollArea.
  const viewportRef = useRef<HTMLDivElement | null>(null)
  // Live "is the view at the bottom" flag, kept current by the scroll
  // listener; read at the moment new utterances render.
  const atBottomRef = useRef(true)
  // Length of utterances the auto-scroll effect last saw (for the
  // initial-batch special case).
  const prevLenRef = useRef(0)
  // Non-null while new lines arrived off-screen and the user hasn't
  // come back down — drives the "New lines ↓" affordance.
  const [newLines, setNewLines] = useState(false)

  const [note, setNote] = useState("")
  const [noteStatus, setNoteStatus] = useState<string | null>(null)
  const [ending, setEnding] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    cursorRef.current = -1
    setUtterances([])
    prevLenRef.current = 0
    setNewLines(false)

    const poll = () => {
      getTranscript(meeting.id, cursorRef.current)
        .then(({ utterances: batch, cursor }) => {
          if (cancelled) return
          cursorRef.current = cursor
          if (batch.length > 0) {
            setUtterances((prev) => [...prev, ...batch])
          }
        })
        .catch((err: unknown) => {
          if (!cancelled) setError(err instanceof Error ? err.message : String(err))
        })
    }

    poll()
    const id = setInterval(poll, POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [meeting.id])

  // Locate Radix's scrolling viewport once mounted and keep atBottom
  // current on every scroll — this is also what dismisses the
  // "New lines ↓" affordance when the user scrolls back down manually.
  useEffect(() => {
    const el = bottomRef.current?.closest<HTMLDivElement>('[data-slot="scroll-area-viewport"]')
    if (!el) return
    viewportRef.current = el
    const onScroll = () => {
      atBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight <= AT_BOTTOM_THRESHOLD_PX
      if (atBottomRef.current) setNewLines(false)
    }
    el.addEventListener("scroll", onScroll, { passive: true })
    return () => el.removeEventListener("scroll", onScroll)
  }, [meeting.id])

  // Auto-scroll ONLY when the user was already at (near) the bottom.
  // The initial batch is exempt: a transcript that just loaded should
  // land on the newest line regardless (matching the old behavior for
  // the only case where it was actually desirable).
  useEffect(() => {
    const prevLen = prevLenRef.current
    prevLenRef.current = utterances.length
    if (utterances.length === 0) return
    if (prevLen === 0) {
      scrollToBottom()
      return
    }
    if (atBottomRef.current) {
      scrollToBottom()
    } else {
      setNewLines(true)
    }
  }, [utterances])

  function scrollToBottom() {
    const el = viewportRef.current
    if (el) el.scrollTo({ top: el.scrollHeight })
    else bottomRef.current?.scrollIntoView({ block: "end" })
  }

  const handleAddNote = async () => {
    const text = note.trim()
    if (!text) return
    try {
      await addNote(meeting.id, text)
      setNote("")
      setNoteStatus("Saved")
      setTimeout(() => setNoteStatus(null), 2000)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleEnd = async () => {
    setEnding(true)
    setError(null)
    try {
      await endMeeting(meeting.id)
      onEnded({ ...meeting, ended_at: new Date().toISOString() })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setEnding(false)
    }
  }

  return (
    <div className="mx-auto flex max-w-3xl flex-col px-6 py-8">
      <div className="mb-4 flex items-center justify-between gap-4">
        <div>
          <h1 className="flex items-start gap-2 break-words text-2xl font-semibold">
            <span className="relative mt-1.5 inline-flex shrink-0">
              <Radio className="size-5 text-primary" />
              <span className="absolute -top-0.5 -right-0.5 size-1.5 motion-safe:animate-pulse rounded-full bg-primary" />
            </span>{" "}
            {meeting.title}
          </h1>
          <p className="text-sm text-muted-foreground">Live meeting — transcript updates automatically.</p>
        </div>
        <Button variant="destructive" onClick={handleEnd} disabled={ending}>
          {ending ? "Ending…" : "End meeting"}
        </Button>
      </div>

      {error && <p className="mb-4 text-sm text-destructive">{error}</p>}

      {captureError && (
        <div role="alert" className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/10 px-4 py-3">
          <p className="text-sm text-destructive">
            <span className="font-medium">Not recording:</span> {captureError}
          </p>
          {onOpenSettings && (
            <Button size="sm" variant="outline" onClick={onOpenSettings}>
              Open settings
            </Button>
          )}
        </div>
      )}

      <div className="relative mb-4">
        <ScrollArea className="h-96 rounded-xl border border-border bg-card">
          <div className="grid gap-3 p-4">
            {utterances.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                Waiting for the first utterance. Start talking to fill the transcript.
              </p>
            ) : (
              utterances.map((u) => (
                <div key={u.seq}>
                  {u.speaker && <p className="text-xs font-medium text-muted-foreground">{u.speaker}</p>}
                  <p className="break-words text-base">{u.text}</p>
                  {u.translated && (
                    <p className="break-words text-base text-muted-foreground">
                      {u.lang && <span className="mr-1 text-xs text-muted-foreground/70">→ {u.lang}</span>}
                      {u.translated}
                    </p>
                  )}
                </div>
              ))
            )}
            <div ref={bottomRef} />
          </div>
        </ScrollArea>
        {newLines && (
          <Button
            size="sm"
            variant="secondary"
            className="absolute bottom-3 left-1/2 -translate-x-1/2 shadow-md"
            onClick={() => {
              setNewLines(false)
              scrollToBottom()
            }}
          >
            <ArrowDown /> New lines
          </Button>
        )}
      </div>

      <div className="grid gap-2">
        <Textarea
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Add a note about this meeting…"
          rows={3}
        />
        <div className="flex items-center gap-3">
          <Button variant="secondary" onClick={handleAddNote} disabled={!note.trim()}>
            Add note
          </Button>
          {noteStatus && <span className="text-sm text-muted-foreground">{noteStatus}</span>}
        </div>
      </div>
    </div>
  )
}
