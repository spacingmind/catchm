import { useEffect, useState } from "react"
import { CheckSquare, GitBranch, Loader2 } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { getMeeting, refineMeeting, type Meeting } from "@/lib/api"

interface MeetingDetailProps {
  meetingId: string
  initialMeeting?: Meeting
  onBack: () => void
}

export function MeetingDetail({ meetingId, initialMeeting, onBack }: MeetingDetailProps) {
  const [meeting, setMeeting] = useState<Meeting | null>(initialMeeting ?? null)
  const [loading, setLoading] = useState(!initialMeeting)
  const [refining, setRefining] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setLoading(true)
    getMeeting(meetingId)
      .then((m) => {
        setMeeting(m)
        setError(null)
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false))
  }, [meetingId])

  const handleRefine = async () => {
    setRefining(true)
    setError(null)
    try {
      const m = await refineMeeting(meetingId)
      setMeeting(m)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setRefining(false)
    }
  }

  if (loading && !meeting) {
    return <div className="mx-auto max-w-3xl px-6 py-8 text-sm text-muted-foreground">Loading…</div>
  }
  if (!meeting) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-8">
        <Button variant="ghost" onClick={onBack}>
          ← Back to meetings
        </Button>
        <p className="mt-4 text-sm text-destructive">{error ?? "Meeting not found."}</p>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <Button variant="ghost" onClick={onBack} className="mb-4">
        ← Back to meetings
      </Button>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="flex items-center gap-2 text-2xl font-semibold">
            {meeting.title}
            {meeting.refined && <Badge variant="secondary">Refined</Badge>}
          </h1>
          <p className="text-sm text-muted-foreground">
            {new Date(meeting.started_at).toLocaleString()}
            {meeting.ended_at ? ` – ${new Date(meeting.ended_at).toLocaleString()}` : ""}
          </p>
        </div>
        <Button onClick={handleRefine} disabled={refining}>
          {refining && <Loader2 className="size-4 animate-spin" />}
          {refining ? "Refining…" : "Refine transcript"}
        </Button>
      </div>

      {error && <p className="mb-4 text-sm text-destructive">{error}</p>}

      <div className="grid gap-4">
        <Card>
          <CardHeader>
            <CardTitle>Summary</CardTitle>
          </CardHeader>
          <CardContent>
            {meeting.summary ? (
              <p className="text-sm">{meeting.summary}</p>
            ) : (
              <p className="text-sm text-muted-foreground">
                No summary yet. Refine the transcript to generate one.
              </p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Participants</CardTitle>
          </CardHeader>
          <CardContent>
            {meeting.participants && meeting.participants.length > 0 ? (
              <p className="text-sm">{meeting.participants.join(", ")}</p>
            ) : (
              <p className="text-sm text-muted-foreground">No participants recorded.</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <GitBranch className="size-4" /> Decisions
            </CardTitle>
          </CardHeader>
          <CardContent>
            {meeting.decisions && meeting.decisions.length > 0 ? (
              <ul className="grid gap-2 text-sm">
                {meeting.decisions.map((d, i) => (
                  <li key={i}>
                    {d.text}
                    {d.scope && <span className="ml-2 text-muted-foreground">({d.scope})</span>}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">No decisions recorded.</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <CheckSquare className="size-4" /> Action items
            </CardTitle>
          </CardHeader>
          <CardContent>
            {meeting.action_items && meeting.action_items.length > 0 ? (
              <ul className="grid gap-2 text-sm">
                {meeting.action_items.map((a, i) => (
                  <li key={i} className="flex flex-wrap items-center gap-2">
                    <span>{a.text}</span>
                    {a.owner && <Badge variant="outline">{a.owner}</Badge>}
                    {a.due && <span className="text-xs text-muted-foreground">due {a.due}</span>}
                    {a.status && <span className="text-xs text-muted-foreground">{a.status}</span>}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">No loose ends.</p>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Notes</CardTitle>
          </CardHeader>
          <CardContent>
            {meeting.notes && meeting.notes.length > 0 ? (
              <ul className="grid gap-1 text-sm">
                {meeting.notes.map((n, i) => (
                  <li key={i}>{n}</li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">No notes.</p>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
