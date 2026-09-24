import { useEffect, useState, type FormEvent, type ReactNode } from "react"
import { CheckSquare, GitBranch, Loader2, Pencil, Trash2 } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  deleteMeeting,
  getMeeting,
  getTranscript,
  refineMeeting,
  updateActionItemStatus,
  updateMeeting,
  type Meeting,
  type Utterance,
} from "@/lib/api"

interface MeetingDetailProps {
  meetingId: string
  initialMeeting?: Meeting
  onBack: () => void
}

const done = (status: string | undefined) => status === "done"

/** Load a whole (ended) transcript by walking the cursor until a page
 *  comes back empty. Capped so a broken backend can't loop forever. */
async function loadFullTranscript(id: string): Promise<Utterance[]> {
  const all: Utterance[] = []
  let cursor = -1
  for (let page = 0; page < 500; page++) {
    const { utterances, cursor: next } = await getTranscript(id, cursor)
    if (utterances.length === 0) break
    all.push(...utterances)
    cursor = next
  }
  return all
}

function groupBySpeaker(us: Utterance[]): { speaker?: string; items: Utterance[] }[] {
  const groups: { speaker?: string; items: Utterance[] }[] = []
  for (const u of us) {
    const last = groups[groups.length - 1]
    if (last && last.speaker === u.speaker) last.items.push(u)
    else groups.push({ speaker: u.speaker, items: [u] })
  }
  return groups
}

function Section({ title, icon, children }: { title: string; icon?: ReactNode; children: ReactNode }) {
  return (
    <section className="mb-8">
      <h2 className="mb-2 flex items-center gap-2 text-lg font-semibold">
        {icon}
        {title}
      </h2>
      {children}
    </section>
  )
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

  const [transcript, setTranscript] = useState<Utterance[] | null>(null)
  const [transcriptError, setTranscriptError] = useState<string | null>(null)

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

  // Transcript is fetched when its tab is first opened, not up front.
  const loadTranscriptOnce = () => {
    if (transcript !== null || transcriptError) return
    loadFullTranscript(meetingId)
      .then(setTranscript)
      .catch((err: unknown) => setTranscriptError(err instanceof Error ? err.message : String(err)))
  }

  const openRename = () => {
    setRenameTitle(meeting?.title ?? "")
    setRenameOpen(true)
  }

  const handleRename = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    const title = renameTitle.trim()
    if (!title || renaming) return
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

  // Inline check-off, same API as the Action items page: optimistic flip,
  // PATCH by index, revert on error.
  const toggleActionItem = (index: number) => {
    if (!meeting) return
    const prev = meeting
    const item = meeting.action_items?.[index]
    if (!item) return
    const status = done(item.status) ? "" : "done"
    setMeeting({
      ...meeting,
      action_items: meeting.action_items?.map((a, i) => (i === index ? { ...a, status } : a)),
    })
    updateActionItemStatus(meetingId, index, status).catch((err: unknown) => {
      setMeeting(prev)
      setError(err instanceof Error ? err.message : String(err))
    })
  }

  if (loading && !meeting) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-8" aria-busy="true">
        <Skeleton className="mb-4 h-9 w-32" />
        <Skeleton className="mb-6 h-8 w-3/5" />
        <Skeleton className="mb-2 h-4 w-2/5" />
        <div className="grid gap-4">
          {[0, 1, 2].map((i) => (
            <div key={i} className="rounded-xl border border-border bg-card">
              <div className="p-6 pb-2">
                <Skeleton className="h-4 w-24" />
              </div>
              <div className="grid gap-2 p-6 pt-0">
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-4/5" />
              </div>
            </div>
          ))}
        </div>
      </div>
    )
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
          <h1 className="flex flex-wrap items-center gap-2 text-2xl font-semibold">
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

      <Tabs defaultValue="overview">
        <TabsList className="mb-6 flex w-fit">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="transcript" onClick={loadTranscriptOnce}>
            Transcript
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <Section title="Decisions" icon={<GitBranch className="size-4" />}>
            {meeting.decisions && meeting.decisions.length > 0 ? (
              <ul className="grid gap-2 text-base">
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
          </Section>

          <Section title="Action items" icon={<CheckSquare className="size-4" />}>
            {meeting.action_items && meeting.action_items.length > 0 ? (
              <ul className="grid gap-3">
                {meeting.action_items.map((a, i) => (
                  <li key={i} className="flex items-start gap-3">
                    <Checkbox
                      id={`detail-item-${i}`}
                      checked={done(a.status)}
                      onCheckedChange={() => toggleActionItem(i)}
                      aria-label={`Mark "${a.text}" done`}
                      className="mt-0.5"
                    />
                    <div className="grid min-w-0 flex-1 gap-1">
                      <label
                        htmlFor={`detail-item-${i}`}
                        className={`cursor-pointer break-words ${done(a.status) ? "line-through" : ""}`}
                      >
                        {a.text}
                      </label>
                      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                        {a.owner && <Badge variant="outline">{a.owner}</Badge>}
                        {a.due && <span>due {a.due}</span>}
                      </div>
                    </div>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">No loose ends.</p>
            )}
          </Section>

          <Section title="Summary">
            {meeting.summary ? (
              <p className="text-base">{meeting.summary}</p>
            ) : (
              <p className="text-sm text-muted-foreground">
                No summary yet. Refine the transcript to generate one.
              </p>
            )}
          </Section>

          <Section title="Participants">
            {meeting.participants && meeting.participants.length > 0 ? (
              <p className="text-base">{meeting.participants.join(", ")}</p>
            ) : (
              <p className="text-sm text-muted-foreground">No participants recorded.</p>
            )}
          </Section>

          <Section title="Notes">
            {meeting.notes && meeting.notes.length > 0 ? (
              <ul className="grid gap-1 text-base">
                {meeting.notes.map((n, i) => (
                  <li key={i}>{n}</li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">No notes.</p>
            )}
          </Section>
        </TabsContent>

        <TabsContent value="transcript">
          {transcriptError ? (
            <p className="text-sm text-destructive">{transcriptError}</p>
          ) : transcript === null ? (
            <div className="grid gap-2" aria-busy="true">
              {[0, 1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-4 w-full" />
              ))}
            </div>
          ) : transcript.length === 0 ? (
            <p className="text-sm text-muted-foreground">No transcript was captured.</p>
          ) : (
            <div className="grid gap-6">
              {groupBySpeaker(transcript).map((g, gi) => (
                <div key={gi}>
                  {g.speaker && (
                    <p className="mb-1 text-xs font-medium text-muted-foreground">{g.speaker}</p>
                  )}
                  <div className="grid gap-2">
                    {g.items.map((u) => (
                      <div key={u.seq}>
                        <p className="break-words text-base">{u.text}</p>
                        {u.translated && (
                          <p className="break-words text-base text-muted-foreground">
                            {u.lang && <span className="mr-1 text-xs text-muted-foreground/70">→ {u.lang}</span>}
                            {u.translated}
                          </p>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </TabsContent>
      </Tabs>

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
