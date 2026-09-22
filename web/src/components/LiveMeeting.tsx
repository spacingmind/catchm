import { useEffect, useRef, useState } from "react"
import { Radio } from "lucide-react"

import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import { addNote, endMeeting, getTranscript, type Meeting, type Utterance } from "@/lib/api"

/** How often the live transcript is polled for new utterances. */
const POLL_INTERVAL_MS = 1500

interface LiveMeetingProps {
  meeting: Meeting
  onEnded: (meeting: Meeting) => void
}

export function LiveMeeting({ meeting, onEnded }: LiveMeetingProps) {
  const [utterances, setUtterances] = useState<Utterance[]>([])
  const cursorRef = useRef(-1)
  const bottomRef = useRef<HTMLDivElement>(null)

  const [note, setNote] = useState("")
  const [noteStatus, setNoteStatus] = useState<string | null>(null)
  const [ending, setEnding] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    cursorRef.current = -1
    setUtterances([])

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

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" })
  }, [utterances])

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
          <h1 className="flex items-start gap-2 text-2xl font-semibold">
            <span className="relative mt-1.5 inline-flex shrink-0">
              <Radio className="size-5 text-primary" />
              <span className="absolute -top-0.5 -right-0.5 size-1.5 animate-pulse rounded-full bg-primary" />
            </span>{" "}
            {meeting.title}
          </h1>
          <p className="text-sm text-muted-foreground">Live meeting — transcript updates automatically.</p>
        </div>
        <Button variant="destructive" onClick={handleEnd} disabled={ending}>
          {ending ? "Ending…" : "End meeting"}
        </Button>
      </div>

      {error && <p className="mb-2 text-sm text-destructive">{error}</p>}

      <ScrollArea className="mb-4 h-96 rounded-xl border border-border bg-card">
        <div className="grid gap-3 p-4">
          {utterances.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Waiting for the first utterance. Start talking to fill the transcript.
            </p>
          ) : (
            utterances.map((u) => (
              <div key={u.seq}>
                {u.speaker && <p className="text-xs font-medium text-muted-foreground">{u.speaker}</p>}
                <p className="text-base">{u.text}</p>
                {u.translated && (
                  <p className="text-base text-muted-foreground">
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
