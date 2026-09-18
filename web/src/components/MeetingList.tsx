import { useCallback, useEffect, useState } from "react"
import { Mic } from "lucide-react"

import logo from "@/assets/logo.png"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { NewMeetingDialog } from "@/components/NewMeetingDialog"
import { listMeetings, type Meeting } from "@/lib/api"

interface MeetingListProps {
  onOpenMeeting: (meeting: Meeting) => void
}

export function MeetingList({ onOpenMeeting }: MeetingListProps) {
  const [meetings, setMeetings] = useState<Meeting[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)

  const refresh = useCallback(() => {
    listMeetings()
      .then((ms) => {
        setMeetings(ms)
        setError(null)
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  const handleStarted = (meeting: Meeting) => {
    setDialogOpen(false)
    onOpenMeeting(meeting)
  }

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Meetings</h1>
        <Button onClick={() => setDialogOpen(true)}>
          <Mic /> Record a meeting
        </Button>
      </div>

      {error && <p className="mb-4 text-sm text-destructive">{error}</p>}

      {meetings === null ? (
        <p className="text-sm text-muted-foreground">Loading…</p>
      ) : meetings.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border py-16 text-center">
          <img src={logo} alt="" className="mx-auto mb-4 size-16 rounded-xl" />
          <p className="text-lg font-medium">Your burrow is quiet.</p>
          <p className="mt-1 text-sm text-muted-foreground">
            Start a meeting to capture the conversation, decisions, and follow-ups in one place.
          </p>
        </div>
      ) : (
        <div className="grid gap-3">
          {meetings.map((m) => (
            <Card
              key={m.id}
              className="cursor-pointer transition-colors hover:bg-secondary/40"
              onClick={() => onOpenMeeting(m)}
            >
              <CardHeader>
                <div className="flex items-center justify-between gap-2">
                  <CardTitle>{m.title}</CardTitle>
                  {!m.ended_at && <Badge>Live</Badge>}
                </div>
                <CardDescription>
                  {formatMeetingSpan(m)}
                  {m.engine ? ` · ${m.engine}` : ""}
                </CardDescription>
              </CardHeader>
              <CardContent className="flex gap-4 text-sm text-muted-foreground">
                <span>{m.decisions?.length ?? 0} decisions</span>
                <span>{m.action_items?.length ?? 0} action items</span>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <NewMeetingDialog open={dialogOpen} onOpenChange={setDialogOpen} onStarted={handleStarted} />
    </div>
  )
}

function formatMeetingSpan(m: Meeting): string {
  const start = new Date(m.started_at)
  if (!m.ended_at) return `Started ${start.toLocaleString()}`

  const end = new Date(m.ended_at)
  if (start.toDateString() === end.toDateString()) {
    return `${start.toLocaleDateString()}, ${start.toLocaleTimeString()} – ${end.toLocaleTimeString()}`
  }
  return `${start.toLocaleString()} – ${end.toLocaleString()}`
}
