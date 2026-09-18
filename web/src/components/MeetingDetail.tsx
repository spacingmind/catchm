import { useEffect, useState, type FormEvent } from "react"
import { CheckSquare, GitBranch, Loader2, Pencil, Trash2 } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { deleteMeeting, getMeeting, refineMeeting, updateMeeting, type Meeting } from "@/lib/api"

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
  const [renameOpen, setRenameOpen] = useState(false)
  const [renameTitle, setRenameTitle] = useState("")
  const [renaming, setRenaming] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)

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

  const openRename = () => {
    setRenameTitle(meeting?.title ?? "")
    setRenameOpen(true)
  }

  const handleRename = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const title = renameTitle.trim()
    if (!title || !meetingId || renaming) return
    setRenaming(true)
    try {
      const m = await updateMeeting(meetingId, { title })
      setMeeting(m)
      setRenameOpen(false)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setRenaming(false)
    }
  }

  const handleDelete = async () => {
    if (!meetingId || deleting) return
    setDeleting(true)
    try {
      await deleteMeeting(meetingId)
      onBack()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setDeleting(false)
    }
  }

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
            <Button
              variant="ghost"
              size="icon"
              className="size-7 text-muted-foreground"
              onClick={openRename}
              aria-label="Rename meeting"
            >
              <Pencil className="size-4" />
            </Button>
          </h1>
          <p className="text-sm text-muted-foreground">
            {new Date(meeting.started_at).toLocaleString()}
            {meeting.ended_at ? ` – ${new Date(meeting.ended_at).toLocaleString()}` : ""}
          </p>
          {meeting.tags && meeting.tags.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1.5">
              {meeting.tags.map((t) => (
                <Badge key={t} variant="secondary">
                  {t}
                </Badge>
              ))}
            </div>
          )}
        </div>
        <div className="flex gap-2">
          <Button onClick={handleRefine} disabled={refining}>
            {refining && <Loader2 className="size-4 animate-spin" />}
            {refining ? "Refining…" : "Refine transcript"}
          </Button>
          <Button variant="destructive" onClick={() => setDeleteOpen(true)}>
            <Trash2 /> Delete
          </Button>
        </div>
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

      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent>
          <form onSubmit={handleRename}>
            <DialogHeader>
              <DialogTitle>Rename meeting</DialogTitle>
              <DialogDescription>Give this meeting a new title.</DialogDescription>
            </DialogHeader>
            <div className="grid gap-1.5 py-4">
              <Input
                value={renameTitle}
                onChange={(e) => setRenameTitle(e.target.value)}
                autoFocus
                aria-label="New title"
              />
            </div>
            <DialogFooter>
              <Button type="submit" disabled={renaming || !renameTitle.trim()}>
                {renaming ? "Saving…" : "Save"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete meeting?</DialogTitle>
            <DialogDescription>
              This permanently removes “{meeting.title}” and its transcript. This can't be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteOpen(false)} disabled={deleting}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleDelete} disabled={deleting}>
              {deleting ? "Deleting…" : "Delete meeting"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
