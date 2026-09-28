import { useEffect, useEffectEvent, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react"
import { AudioLines, CheckSquare, Cloud, GitBranch, HardDrive, Loader2, Pencil, Sparkles, Trash2 } from "lucide-react"

import { ConfirmDialog } from "@/components/ConfirmDialog"
import { CloudConsentDialog } from "@/components/settings/SettingsSummaries"
import { StatusMessage } from "@/components/StatusMessage"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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
import { useAnnounceChange, useStatusCheck } from "@/hooks/useStatusCheck"
import {
  ApiError,
  deleteMeeting,
  getConfig,
  getMeeting,
  getSummaryStatus,
  getTranscript,
  refineMeeting,
  RefineError,
  updateActionItemStatus,
  updateConfig,
  updateMeeting,
  type Meeting,
  type Utterance,
} from "@/lib/api"
import { formatDateRange, formatDateTime, formatDay } from "@/lib/format"
import { noAutofill } from "@/lib/inputProps"
import { languageLabel } from "@/lib/languages"
import { formatGb, modelDownloadGb } from "@/lib/modelSizes"
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

/** CatchM Cloud's free daily summaries, when the quota endpoint doesn't say. */
const CLOUD_DEFAULT_LIMIT = 5

/** Soniox's async transcription rate. */
const SONIOX_RATE_PER_HOUR = 0.1
const SONIOX_PRIVACY = "The audio is uploaded to Soniox, processed, and deleted right after."

/** The meeting's length in seconds (0 while live). */
function meetingSeconds(m: Meeting): number {
  if (!m.ended_at) return 0
  const start = new Date(m.started_at).getTime()
  const end = new Date(m.ended_at).getTime()
  return Number.isFinite(start) && Number.isFinite(end) && end > start ? (end - start) / 1000 : 0
}

/** "about $0.05" for a Soniox pass over the recording. */
function sonioxCost(m: Meeting): string {
  return `about $${((meetingSeconds(m) / 3600) * SONIOX_RATE_PER_HOUR).toFixed(2)}`
}

/** What "Create summary & action items" does, shown next to the button. */
const SUMMARY_HELP =
  "CatchM listens to the whole recording again for a more accurate transcript, then the AI writes the summary, decisions and action items. It takes a few minutes, so keep this page open."

/** A refine failure in plain words, with what to do next. Summary
 *  failures carry a `kind` (RefineError); the rest are the raw messages
 *  handleRefine passes through: batch.Refine's sentinels and the Whisper
 *  engine (the only network call before the summary). consent_required
 *  never reaches here: handleRefine asks for consent instead. */
function refineErrorText(err: unknown): string {
  const raw = errorText(err)
  if (err instanceof RefineError) {
    switch (err.kind) {
      case "summary_not_ready":
        return `Summaries aren’t set up yet. ${sentence(raw)} Fix this in Settings → Summaries, then try again.`
      case "quota_exceeded": {
        const after = err.reset_at ? formatDateTime(err.reset_at) : "tomorrow"
        return `Today’s free summaries are used up. Try again after ${after}, or switch to “On this computer” in Settings → Summaries.`
      }
      case "summary_failed":
        return "CatchM couldn’t write the summary this time. Try again in a moment."
    }
  }
  if (err instanceof ApiError && err.status === 404) return "This meeting no longer exists. Go back to your meetings."
  if (/still active/i.test(raw)) return "This meeting is still recording. End it first, then create the summary."
  if (/no audio/i.test(raw)) return "This meeting has no saved recording, so a summary can’t be created."
  if (/whisper|inference|connection refused|dial tcp/i.test(raw)) {
    return "Transcription on this computer (the Whisper server) isn’t responding. Check Settings → Transcription, then try again."
  }
  return `Couldn’t create the summary: ${sentence(raw)} Try again.`
}

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
  // The last refine failed because summaries aren't set up: offer a way there.
  const [needsSetup, setNeedsSetup] = useState(false)
  const [consentOpen, setConsentOpen] = useState(false)
  // Set by the consent dialog's accept, so its close doesn't read as "declined".
  const consentAccepted = useRef(false)
  const [renameOpen, setRenameOpen] = useState(false)
  const [renameTitle, setRenameTitle] = useState("")
  const [renaming, setRenaming] = useState(false)
  const [renameError, setRenameError] = useState<string | null>(null)
  const renameInputRef = useRef<HTMLInputElement>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  // "Use CatchM Cloud" from the not-set-up card: the consent dialog (which
  // also selects the provider), or a direct save when consent was given.
  const [cloudConsentOpen, setCloudConsentOpen] = useState(false)
  const [cloudSaving, setCloudSaving] = useState(false)
  const [cloudError, setCloudError] = useState<string | null>(null)
  // "Improve with Soniox": only offered when a key is set.
  const [sonioxKeySet, setSonioxKeySet] = useState<boolean | null>(null)
  const [improveOpen, setImproveOpen] = useState(false)
  const [improving, setImproving] = useState(false)
  const [improveError, setImproveError] = useState<string | null>(null)

  // An ended meeting without a summary leads with "Create summary", so
  // check first whether summaries are set up; if not, the card offers the
  // two ways to set them up instead of an error after the click. It
  // re-checks on focus, e.g. after installing Ollama in another window.
  const {
    data: summaryStatus,
    error: summaryStatusError,
    refresh: refreshSummaryStatus,
  } = useStatusCheck(getSummaryStatus, { enabled: Boolean(meeting?.ended_at) && !meeting?.refined })
  const summaryAnnouncement = useAnnounceChange(
    summaryStatus
      ? summaryStatus.ready
        ? "Summaries are set up. You can create the summary now."
        : "Summaries aren’t set up yet."
      : null,
  )

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

  // A Soniox key decides whether "Improve with Soniox" is offered.
  const ended = Boolean(meeting?.ended_at)
  useEffect(() => {
    if (!ended) return
    let cancelled = false
    getConfig()
      .then((c) => {
        if (!cancelled) setSonioxKeySet(c.soniox_api_key_set)
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [ended, meetingId])

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
    setNeedsSetup(false)
    try {
      const m = await refineMeeting(meetingId)
      setMeeting(m)
    } catch (err) {
      // Nothing was sent yet: ask, and retry once the user agrees.
      if (err instanceof RefineError && err.kind === "consent_required") {
        setConsentOpen(true)
      } else {
        setError(refineErrorText(err))
        const setup = err instanceof RefineError && (err.kind === "summary_not_ready" || err.kind === "quota_exceeded")
        setNeedsSetup(setup)
        // The card should now show what's missing, not the old "ready".
        if (setup) refreshSummaryStatus()
      }
    } finally {
      setRefining(false)
    }
  }

  const handleImprove = async () => {
    if (improving) return
    setImproving(true)
    setImproveError(null)
    try {
      const m = await refineMeeting(meetingId, { engine: "soniox" })
      setMeeting(m)
      setImproveOpen(false)
    } catch (err) {
      setImproveError(refineErrorText(err))
    } finally {
      setImproving(false)
    }
  }

  // CatchM Cloud is chosen (and agreed to). Logged in: go ahead with the
  // summary, like the consent_required retry. Logged out: the re-check
  // turns the card's cloud option into "Open Cloud sync".
  const afterCloudChosen = () => {
    refreshSummaryStatus()
    if (summaryStatus?.cloud?.logged_in !== false) void handleRefine()
  }

  const chooseCloud = async () => {
    if (cloudSaving) return
    setCloudError(null)
    if (!summaryStatus?.cloud?.consent) {
      setCloudConsentOpen(true)
      return
    }
    setCloudSaving(true)
    try {
      await updateConfig({ summary: { provider: "catchm-cloud" } })
      afterCloudChosen()
    } catch (err) {
      setCloudError(`Couldn’t switch to CatchM Cloud: ${sentence(errorText(err))} Try again.`)
    } finally {
      setCloudSaving(false)
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

  const refined = Boolean(meeting.refined)
  // Offered for an ended meeting transcribed locally (or not at all)
  // that hasn't already had a Soniox pass, when a key is set.
  const showImprove =
    Boolean(meeting.ended_at) && meeting.engine !== "soniox" && meeting.refined_with !== "soniox" && sonioxKeySet === true
  // Summaries are manual: ending a meeting doesn't create one, so an
  // ended, unsummarized meeting leads with the call to action.
  const showSummaryCta = !refined && Boolean(meeting.ended_at)
  const findHint = meeting.ended_at
    ? "Create a summary to find them."
    : "End the meeting, then create a summary to find them."
  const summaryButton = (
    <Button onClick={handleRefine} disabled={refining} variant={refined ? "outline" : "default"}>
      {refining ? (
        <Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin" />
      ) : (
        !refined && <Sparkles aria-hidden="true" />
      )}
      {refining ? "Creating summary…" : refined ? "Redo summary" : "Create summary & action items"}
    </Button>
  )

  // Not set up (and not mid-summary): the card offers the two ways to set
  // summaries up. A failed check falls back to the plain button.
  const setupNeeded = summaryStatus != null && !summaryStatus.ready && !refining
  // The first check is still running: hold the button's place, so the
  // card doesn't swap from "Create" to "set up" under the pointer.
  const checkingSetup = summaryStatus == null && summaryStatusError == null
  const localGb = modelDownloadGb(summaryStatus?.ollama?.model)
  const cloud = summaryStatus?.cloud
  const usingCloud = summaryStatus?.provider === "catchm-cloud"
  const cloudNeedsLogin = usingCloud && cloud?.logged_in === false
  // CatchM Cloud chosen, agreed to and logged in, yet not ready (e.g.
  // today's free summaries are used up): the reason says why, and there's
  // nothing to switch to.
  const offerCloud = !usingCloud || cloudNeedsLogin || !cloud?.consent

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <Button variant="ghost" onClick={onBack} className="mb-4">
        <span aria-hidden="true">←</span> Back to meetings
      </Button>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-start gap-2">
            <h1 className="min-w-0 text-balance break-words text-2xl font-semibold">{meeting.title}</h1>
            {refined && (
              <Badge variant="secondary" className="mt-1.5 shrink-0">
                Summary ready
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
        {showImprove && (
          <div className="mb-4 flex flex-wrap items-center gap-3">
            <Button
              variant="outline"
              size="sm"
              aria-describedby="improve-soniox-desc"
              onClick={() => {
                setImproveError(null)
                setImproveOpen(true)
              }}
            >
              <AudioLines aria-hidden="true" className="size-4" />
              Improve with Soniox
            </Button>
            <p id="improve-soniox-desc" className="min-w-0 flex-1 text-xs text-muted-foreground">
              Adds who said what (speaker names) and a more accurate transcript, then rewrites the summary.
            </p>
          </div>
        )}
        <div className="flex gap-2">
          {refined && summaryButton}
          <Button variant="destructive" onClick={openDelete}>
            <Trash2 aria-hidden="true" /> Delete
          </Button>
        </div>
      </div>

      <div className={needsSetup && error ? "mb-4 flex flex-wrap items-center gap-3" : undefined}>
        <StatusMessage tone="error" className={needsSetup && error ? "min-w-0 flex-1" : "mb-4"}>
          {error}
        </StatusMessage>
        {needsSetup && error && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => navigate({ name: "settings", section: "summaries" })}
          >
            Open settings
          </Button>
        )}
      </div>
      <StatusMessage className="sr-only">{summaryAnnouncement}</StatusMessage>
      {meeting.refined_with === "soniox" && (
        <StatusMessage className="mb-4">Transcript improved with Soniox.</StatusMessage>
      )}
      {refined && (
        <StatusMessage className="mb-4">
          {refining ? "Creating a new summary. This takes a few minutes, so keep this page open." : null}
        </StatusMessage>
      )}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className="mb-6 flex w-fit">
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="transcript">
            Transcript
            {meeting.refined_with === "soniox" && (
              <Badge variant="secondary" className="ml-1.5">
                Soniox
              </Badge>
            )}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          {showSummaryCta &&
            (setupNeeded ? (
              <Card className="mb-8 border-primary/40">
                <CardHeader>
                  <CardTitle className="text-lg">
                    <h2 className="flex items-center gap-2">
                      <Sparkles aria-hidden="true" className="size-4" />
                      Summaries aren’t set up yet
                    </h2>
                  </CardTitle>
                  <CardDescription className="text-pretty">
                    {summaryStatus.reason && `${sentence(summaryStatus.reason)} `}
                    Choose how CatchM writes the summary, decisions and action items. You only do this once.
                  </CardDescription>
                </CardHeader>
                <CardContent className="grid gap-3">
                  <ul className="grid gap-3 sm:grid-cols-2">
                    <li className="grid content-between justify-items-start gap-3 rounded-lg border border-border p-4">
                      <div>
                        <h3 className="flex items-center gap-2 text-sm font-medium">
                          <HardDrive aria-hidden="true" className="size-4" />
                          On this computer
                        </h3>
                        <p id="summary-setup-local-desc" className="mt-1 text-sm text-muted-foreground">
                          Free and private. One-time download of{" "}
                          {localGb != null ? `about ${formatGb(localGb)}` : "a few GB"}.
                        </p>
                      </div>
                      <Button
                        size="sm"
                        onClick={() => navigate({ name: "settings", section: "summaries" })}
                        aria-describedby="summary-setup-local-desc"
                      >
                        Set up on this computer
                      </Button>
                    </li>
                    {offerCloud && (
                      <li className="grid content-between justify-items-start gap-3 rounded-lg border border-border p-4">
                        <div>
                          <h3 className="flex items-center gap-2 text-sm font-medium">
                            <Cloud aria-hidden="true" className="size-4" />
                            CatchM Cloud
                          </h3>
                          <p id="summary-setup-cloud-desc" className="mt-1 text-sm text-muted-foreground">
                            Free, {cloud?.limit ?? CLOUD_DEFAULT_LIMIT} per day, needs a CatchM account.
                            {cloudNeedsLogin && " Log in or sign up on the Cloud sync page, then come back here."}
                          </p>
                        </div>
                        {cloudNeedsLogin ? (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => navigate({ name: "settings", section: "cloud" })}
                            aria-describedby="summary-setup-cloud-desc"
                          >
                            Open Cloud sync
                          </Button>
                        ) : (
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => void chooseCloud()}
                            disabled={cloudSaving}
                            aria-describedby="summary-setup-cloud-desc"
                          >
                            {cloudSaving ? "Saving…" : "Use CatchM Cloud"}
                          </Button>
                        )}
                      </li>
                    )}
                  </ul>
                  <StatusMessage tone="error" className="break-words">
                    {cloudError}
                  </StatusMessage>
                  <Button
                    variant="link"
                    size="sm"
                    className="h-auto justify-self-start px-0 text-muted-foreground"
                    onClick={() => void handleRefine()}
                  >
                    Try anyway
                  </Button>
                </CardContent>
              </Card>
            ) : (
              <Card className="mb-8 border-primary/40">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 text-lg">
                    <Sparkles aria-hidden="true" className="size-4" />
                    Get a summary of this meeting
                  </CardTitle>
                  <CardDescription className="text-pretty">{SUMMARY_HELP}</CardDescription>
                </CardHeader>
                <CardContent>
                  {checkingSetup ? <Skeleton aria-hidden="true" className="h-8 w-60" /> : summaryButton}
                </CardContent>
              </Card>
            ))}
          <Section title="Decisions" icon={<GitBranch aria-hidden="true" className="size-4" />}>
            {meeting.decisions && meeting.decisions.length > 0 ? (
              <ul className="grid gap-2 text-base">
                {meeting.decisions.map((d, i) => (
                  <li key={i} className="break-words">
                    {d.text}
                    {d.scope && <span className="block text-sm text-muted-foreground">{d.scope}</span>}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">
                {refined ? "No decisions were found in this meeting." : `No decisions yet. ${findHint}`}
              </p>
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
              <p className="text-sm text-muted-foreground">
                {refined
                  ? "No action items were found in this meeting."
                  : `No action items yet. ${findHint}`}
              </p>
            )}
          </Section>

          <Section title="Summary">
            {meeting.summary ? (
              <p className="break-words text-base">{meeting.summary}</p>
            ) : (
              <p className="text-sm text-muted-foreground">
                {refined ? "No summary was written for this meeting." : showSummaryCta
                  ? "No summary yet. Create one with the button above."
                  : "No summary yet. End the meeting, then create one."}
              </p>
            )}
          </Section>

          <Section title="Participants">
            {meeting.participants && meeting.participants.length > 0 ? (
              <p className="break-words text-base">{meeting.participants.join(", ")}</p>
            ) : (
              <p className="text-sm text-muted-foreground">No participants found.</p>
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
                              <span className="mr-1 text-xs text-muted-foreground">
                                <span aria-hidden="true">→</span> {languageLabel(u.lang)}
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

      <CloudConsentDialog
        open={consentOpen}
        onOpenChange={(open) => {
          setConsentOpen(open)
          if (!open && !consentAccepted.current) {
            setError(
              "No summary was created. CatchM Cloud needs your OK before it gets the transcript. To keep everything on this computer, switch to “On this computer” in Settings → Summaries.",
            )
            setNeedsSetup(true)
          }
          consentAccepted.current = false
        }}
        onAccepted={() => {
          consentAccepted.current = true
          void handleRefine()
        }}
      />

      <CloudConsentDialog
        open={cloudConsentOpen}
        onOpenChange={setCloudConsentOpen}
        onAccepted={afterCloudChosen}
        select
      />

      <ConfirmDialog
        open={improveOpen}
        onOpenChange={(next) => {
          if (!next && improving) return
          setImproveOpen(next)
        }}
        title="Improve this transcript with Soniox?"
        description={
          <>
            <span className="block">
              CatchM listens to the recording again and adds who said what (speaker names) and a more accurate
              transcript, then rewrites the summary.
            </span>
            <span className="mt-2 block">
              Estimated cost: {sonioxCost(meeting)} ($0.10 per hour). {SONIOX_PRIVACY}
            </span>
          </>
        }
        confirmLabel={improving ? "Improving…" : "Improve with Soniox"}
        pending={improving}
        error={improveError}
        onConfirm={() => void handleImprove()}
      />

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
