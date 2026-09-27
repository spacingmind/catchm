import { useEffect, useEffectEvent, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react"
import { CheckSquare, GitBranch, Loader2, Pencil, Trash2 } from "lucide-react"

import { ConfirmDialog } from "@/components/ConfirmDialog"
import { StatusMessage } from "@/components/StatusMessage"
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
import { Label } from "@/components/ui/label"
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
import { formatDateRange, formatDay } from "@/lib/format"
import { noAutofill } from "@/lib/inputProps"
import { navigate, useRoute, type MeetingTab } from "@/lib/route"

interface MeetingDetailProps {
  meetingId: string
  initialMeeting?: Meeting
  onBack: () => void
  /** After a successful delete; defaults to onBack. App replaces the
   *  history entry so Back can't land on the deleted meeting. */
  onDeleted?: () => void
}

const done = (status: string | undefined) => status === "done"

const errorText = (err: unknown) => (err instanceof Error ? err.message : String(err))
/** Ends with exactly one period, so a next step can follow. */
const sentence = (s: string) => `${s.replace(/[.\s]+$/, "")}.`

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

export function MeetingDetail({ meetingId, initialMeeting, onBack, onDeleted = onBack }: MeetingDetailProps) {
  const [meeting, setMeeting] = useState<Meeting | null>(initialMeeting ?? null)
  const [refining, setRefining] = useState(false)
  // Page-level errors (load, refine, action-item toggle). Rename/delete
  // errors stay inside their dialogs, where the user is looking.
  const [error, setError] = useState<string | null>(null)
  const [renameOpen, setRenameOpen] = useState(false)
  const [renameTitle, setRenameTitle] = useState("")
  const [renaming, setRenaming] = useState(false)
  const [renameError, setRenameError] = useState<string | null>(null)
  const renameInputRef = useRef<HTMLInputElement>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  const [transcript, setTranscript] = useState<Utterance[] | null>(null)
  const [transcriptError, setTranscriptError] = useState<string | null>(null)
  const groups = useMemo(() => (transcript ? groupBySpeaker(transcript) : []), [transcript])

  // Refresh on mount (initialMeeting may be a stale list row). No
  // loading flag: the parent remounts this per meeting (key={id}), so
  // "no meeting and no error yet" is the loading state.
  useEffect(() => {
    let cancelled = false
    getMeeting(meetingId)
      .then((m) => {
        if (cancelled) return
        setMeeting(m)
        setError(null)
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errorText(err))
      })
    return () => {
      cancelled = true
    }
  }, [meetingId])

  // Transcript is fetched when its tab is first opened, not up front.
  // The cancelled flag ignores the result if the component unmounted or
  // the meeting changed mid-walk (paired with the key={selected.id} remount).
  const fetchTranscript = () => {
    if (loadingRef.current) return
    loadingRef.current = true
    loadFullTranscript(meetingId)
      .then((us) => {
        if (!cancelledRef.current) setTranscript(us)
      })
      .catch((err: unknown) => {
        if (!cancelledRef.current) setTranscriptError(errorText(err))
      })
      .finally(() => {
        loadingRef.current = false
      })
  }
  const loadTranscriptOnce = () => {
    if (transcript === null && !transcriptError) fetchTranscript()
  }
  const retryTranscript = () => {
    setTranscriptError(null)
    fetchTranscript()
  }

  const cancelledRef = useRef(false)
  const loadingRef = useRef(false)
  useEffect(() => {
    // Reset on (re)mount: StrictMode's dev-only unmount/remount would
    // otherwise leave this true and drop every transcript load.
    cancelledRef.current = false
    return () => {
      cancelledRef.current = true
    }
  }, [])

  // The tab lives in the URL (#/meetings/{id}?tab=transcript), so reload
  // and deep links reopen it. Switching tabs replaces rather than pushes:
  // Back leaves the meeting instead of stepping through tab clicks.
  const route = useRoute()
  const tab: MeetingTab = route.name === "meeting" ? route.tab : "overview"
  const setTab = (value: string) => {
    if (route.name !== "meeting") return
    navigate({ ...route, tab: value === "transcript" ? "transcript" : "overview" }, { replace: true })
  }
  // Load on arrival too, not just on a tab click: the URL may open here.
  const onTabShown = useEffectEvent((shown: MeetingTab) => {
    if (shown === "transcript") loadTranscriptOnce()
  })
  useEffect(() => onTabShown(tab), [tab])

  const openRename = () => {
    setRenameTitle(meeting?.title ?? "")
    setRenameError(null)
    setRenameOpen(true)
  }

  const handleRename = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (renaming) return
    const title = renameTitle.trim()
    if (!title) {
      setRenameError("Title is required.")
      renameInputRef.current?.focus()
      return
    }
    setRenaming(true)
    setRenameError(null)
    try {
      const m = await updateMeeting(meetingId, { title })
      setMeeting(m)
      setRenameOpen(false)
    } catch (err) {
      setRenameError(`${sentence(errorText(err))} Try again.`)
    } finally {
      setRenaming(false)
    }
  }

  const openDelete = () => {
    setDeleteError(null)
    setDeleteOpen(true)
  }

  const handleDelete = async () => {
    if (!meetingId || deleting) return
    setDeleting(true)
    setDeleteError(null)
    try {
      await deleteMeeting(meetingId)
      onDeleted()
    } catch (err) {
      setDeleteError(`${sentence(errorText(err))} Try again.`)
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
      setError(`Couldn’t refine the transcript: ${sentence(errorText(err))} Try again.`)
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
      setError(`Couldn’t update “${item.text}”: ${sentence(errorText(err))} Try again.`)
    })
  }

  if (!meeting && !error) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-8" aria-busy="true">
        <StatusMessage className="sr-only">Loading meeting…</StatusMessage>
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
          <span aria-hidden="true">←</span> Back to meetings
        </Button>
        <StatusMessage tone="error" className="mt-4">
          Couldn’t load this meeting{error ? ` (${error})` : ""}. Go back and open it again.
        </StatusMessage>
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <Button variant="ghost" onClick={onBack} className="mb-4">
        <span aria-hidden="true">←</span> Back to meetings
      </Button>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-start gap-2">
            <h1 className="min-w-0 text-balance break-words text-2xl font-semibold">{meeting.title}</h1>
            {meeting.refined && (
              <Badge variant="secondary" className="mt-1.5 shrink-0">
                Refined
              </Badge>
            )}
            <Button
              variant="ghost"
              size="icon"
              className="mt-0.5 size-7 shrink-0 text-muted-foreground"
              onClick={openRename}
              aria-label="Rename meeting"
            >
              <Pencil aria-hidden="true" className="size-4" />
            </Button>
          </div>
          <p className="text-sm text-muted-foreground">{formatDateRange(meeting.started_at, meeting.ended_at)}</p>
          {meeting.tags && meeting.tags.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1.5">
              {meeting.tags.map((t) => (
                <Badge key={t} variant="secondary" className="max-w-full">
                  <span className="min-w-0 truncate">{t}</span>
                </Badge>
              ))}
            </div>
          )}
        </div>
        <div className="flex gap-2">
          <Button onClick={handleRefine} disabled={refining}>
            {refining && <Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin" />}
            {refining ? "Refining…" : "Refine transcript"}
          </Button>
          <Button variant="destructive" onClick={openDelete}>
            <Trash2 aria-hidden="true" /> Delete
          </Button>
        </div>
      </div>

      <StatusMessage tone="error" className="mb-4">
        {error}
      </StatusMessage>

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className="mb-6 flex w-fit">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="transcript">Transcript</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <Section title="Decisions" icon={<GitBranch aria-hidden="true" className="size-4" />}>
            {meeting.decisions && meeting.decisions.length > 0 ? (
              <ul className="grid gap-2 text-base">
                {meeting.decisions.map((d, i) => (
                  <li key={i} className="break-words">
                    {d.text}
                    {d.scope && <span className="ml-2 text-muted-foreground">({d.scope})</span>}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">No decisions recorded.</p>
            )}
          </Section>

          <Section title="Action items" icon={<CheckSquare aria-hidden="true" className="size-4" />}>
            {meeting.action_items && meeting.action_items.length > 0 ? (
              <ul className="grid gap-3">
                {meeting.action_items.map((a, i) => (
                  // The checkbox's hit area (after:-inset-x-3) spans the
                  // gap-3, so box and label form one target.
                  <li key={i} className="flex items-start gap-3">
                    <Checkbox
                      id={`detail-item-${i}`}
                      checked={done(a.status)}
                      onCheckedChange={() => toggleActionItem(i)}
                      className="mt-1"
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
                        {a.due && <span>due {formatDay(a.due)}</span>}
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
              <p className="break-words text-base">{meeting.summary}</p>
            ) : (
              <p className="text-sm text-muted-foreground">
                No summary yet. Refine the transcript to generate one.
              </p>
            )}
          </Section>

          <Section title="Participants">
            {meeting.participants && meeting.participants.length > 0 ? (
              <p className="break-words text-base">{meeting.participants.join(", ")}</p>
            ) : (
              <p className="text-sm text-muted-foreground">No participants recorded.</p>
            )}
          </Section>

          <Section title="Notes">
            {meeting.notes && meeting.notes.length > 0 ? (
              <ul className="grid gap-1 text-base">
                {meeting.notes.map((n, i) => (
                  <li key={i} className="break-words">
                    {n}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">No notes.</p>
            )}
          </Section>
        </TabsContent>

        <TabsContent value="transcript">
          {transcriptError ? (
            <div className="grid justify-items-start gap-3">
              <StatusMessage tone="error">
                Couldn’t load the transcript ({transcriptError}). Try again.
              </StatusMessage>
              <Button variant="outline" onClick={retryTranscript}>
                Try again
              </Button>
            </div>
          ) : transcript === null ? (
            <div className="grid gap-2" aria-busy="true">
              <StatusMessage className="sr-only">Loading transcript…</StatusMessage>
              {[0, 1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-4 w-full" />
              ))}
            </div>
          ) : transcript.length === 0 ? (
            <p className="text-sm text-muted-foreground">No transcript was captured.</p>
          ) : (
            <div className="grid gap-6">
              {groups.map((g, gi) => (
                // content-visibility skips layout/paint for off-screen
                // speaker turns — long transcripts, no virtualization dep.
                <div key={gi} className="[contain-intrinsic-size:auto_6rem] [content-visibility:auto]">
                  {g.speaker && (
                    <p className="mb-1 break-words text-xs font-medium text-muted-foreground">{g.speaker}</p>
                  )}
                  <div className="grid gap-2">
                    {g.items.map((u) => (
                      <div key={u.seq}>
                        <p className="break-words text-base">{u.text}</p>
                        {u.translated && (
                          <p className="break-words text-base text-muted-foreground">
                            {u.lang && (
                              <span translate="no" className="mr-1 text-xs text-muted-foreground">
                                <span aria-hidden="true">→</span> {u.lang}
                              </span>
                            )}
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

      <Dialog
        open={renameOpen}
        onOpenChange={(open) => {
          if (!open && renaming) return
          setRenameOpen(open)
        }}
      >
        <DialogContent>
          <form onSubmit={handleRename} noValidate>
            <DialogHeader>
              <DialogTitle>Rename meeting</DialogTitle>
              <DialogDescription>Give this meeting a new title.</DialogDescription>
            </DialogHeader>
            <div className="grid gap-1.5 py-4">
              <Label htmlFor="rename-title">Title</Label>
              <Input
                ref={renameInputRef}
                id="rename-title"
                name="title"
                {...noAutofill}
                spellCheck
                value={renameTitle}
                onChange={(e) => {
                  setRenameTitle(e.target.value)
                  if (renameError) setRenameError(null)
                }}
                // The dialog's single field; focus lands here on open.
                autoFocus
                aria-invalid={Boolean(renameError)}
                aria-describedby="rename-title-error"
              />
              <StatusMessage id="rename-title-error" tone="error">
                {renameError}
              </StatusMessage>
            </div>
            <DialogFooter>
              <Button type="submit" disabled={renaming} aria-busy={renaming || undefined}>
                {renaming && <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />}
                {renaming ? "Renaming…" : "Rename meeting"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title="Delete meeting?"
        description={<>This permanently removes “{meeting.title}” and its transcript. This can’t be undone.</>}
        confirmLabel={deleting ? "Deleting…" : "Delete meeting"}
        destructive
        pending={deleting}
        error={deleteError}
        onConfirm={() => void handleDelete()}
      />
    </div>
  )
}
