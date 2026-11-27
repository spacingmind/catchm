import { useEffect, useEffectEvent, useMemo, useRef, useState, type FormEvent, type ReactNode } from "react"
import i18next from "i18next"
import { useTranslation } from "react-i18next"
import { AudioLines, CheckSquare, Cloud, Copy, Download, FileCode, FileText, FileType, GitBranch, HardDrive, Loader2, Pencil, Sparkles, Trash2 } from "lucide-react"

import { ConfirmDialog } from "@/components/ConfirmDialog"
import { EvidenceBar, SourceLink } from "@/components/insights/Evidence"
import { evidenceMatchesTranscript, prefersReducedMotion } from "@/components/insights/evidenceData"
import { FlowsTab } from "@/components/insights/FlowsTab"
import { MindMapTab } from "@/components/insights/MindMapTab"
import { useFindFlows } from "@/components/insights/useFindFlows"
import { CloudConsentDialog } from "@/components/settings/SettingsSummaries"
import { MeetingOrganize } from "@/components/MeetingOrganize"
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useAnnounceChange, useStatusCheck } from "@/hooks/useStatusCheck"
import {
  ApiError,
  deleteMeeting,
  exportUrl,
  fetchExportText,
  getConfig,
  getMeeting,
  getMeetingAnalysis,
  getSummaryStatus,
  getTranscript,
  refineMeeting,
  RefineError,
  updateActionItemStatus,
  type ExportFormat,
  updateConfig,
  updateMeeting,
  type Meeting,
  type MeetingAnalysis,
  type Setup,
  type TranscriptSource,
  type Utterance,
} from "@/lib/api"
import { formatDateRange, formatDateTime, formatDay } from "@/lib/format"
import { noAutofill } from "@/lib/inputProps"
import { copyText } from "@/lib/clipboard"
import { hasWindowControls, saveExport } from "@/lib/wailsRuntime"
import { formatGb, modelDownloadGb } from "@/lib/modelSizes"
import { meetingTab, navigate, useRoute, type MeetingTab } from "@/lib/route"
import { isOptionVisible, isSelfHostVisible } from "@/lib/setup"

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
/** Shown next to the estimated cost in the Soniox confirm dialog. */
function sonioxPrivacy(): string {
  return i18next.t("meetingDetail.sonioxPrivacy")
}

/** The meeting's length in seconds (0 while live). */
function meetingSeconds(m: Meeting): number {
  if (!m.ended_at) return 0
  const start = new Date(m.started_at).getTime()
  const end = new Date(m.ended_at).getTime()
  return Number.isFinite(start) && Number.isFinite(end) && end > start ? (end - start) / 1000 : 0
}

/** "about $0.05" for a Soniox pass over the recording. */
function sonioxCost(m: Meeting): string {
  const cost = ((meetingSeconds(m) / 3600) * SONIOX_RATE_PER_HOUR).toFixed(2)
  return i18next.t("meetingDetail.sonioxCost", { cost })
}

/** What "Create summary & action items" does, shown next to the button. */
function summaryHelp(): string {
  return i18next.t("meetingDetail.summaryHelp")
}

/** A refine failure in plain words, with what to do next. Summary
 *  failures carry a `kind` (RefineError); the rest are the raw messages
 *  handleRefine passes through: batch.Refine's sentinels and the Whisper
 *  engine (the only network call before the summary), matched only when
 *  the setup can use Whisper. consent_required never reaches here:
 *  handleRefine asks for consent instead. */
function refineErrorText(err: unknown, setup: Setup): string {
  const raw = errorText(err)
  if (err instanceof RefineError) {
    switch (err.kind) {
      case "summary_not_ready":
        return i18next.t("meetingDetail.refineNotReady", { reason: sentence(raw) })
      case "quota_exceeded": {
        const after = err.reset_at ? formatDateTime(err.reset_at) : i18next.t("meetingDetail.tomorrow")
        // Under Cloud there's no "On this computer" to switch to.
        return isSelfHostVisible(setup)
          ? i18next.t("meetingDetail.refineQuota", { after })
          : i18next.t("meetingDetail.refineQuotaCloud", { after })
      }
      case "summary_failed":
        return i18next.t("meetingDetail.refineFailed")
    }
  }
  if (err instanceof ApiError && err.status === 404) return i18next.t("meetingDetail.refineGone")
  // Codes, not message text: the message is already translated.
  if (err instanceof ApiError && err.code === "meeting_active") return i18next.t("meetingDetail.refineStillRecording")
  if (err instanceof ApiError && err.code === "no_audio") return i18next.t("meetingDetail.refineNoAudio")
  if (isSelfHostVisible(setup) && /whisper|inference|connection refused|dial tcp/i.test(raw)) {
    return i18next.t("meetingDetail.refineWhisperDown")
  }
  return i18next.t("meetingDetail.refineGeneric", { reason: sentence(raw) })
}

/** Load a whole (ended) transcript by walking the cursor until a page
 *  comes back empty. Capped so a broken backend can't loop forever. */
async function loadFullTranscript(
  id: string,
  source: TranscriptSource,
): Promise<{ utterances: Utterance[]; source: "live" | "v2" }> {
  const all: Utterance[] = []
  let cursor = -1
  let served: "live" | "v2" = source === "v2" ? "v2" : "live"
  for (let page = 0; page < 500; page++) {
    const res = await getTranscript(id, cursor, source)
    served = res.source
    if (res.utterances.length === 0) break
    all.push(...res.utterances)
    cursor = res.cursor
  }
  return { utterances: all, source: served }
}

/** The analysis item's evidence for the frontmatter item at the same
 *  index (the frontmatter is written from the analysis in index order),
 *  or none when the two no longer line up. An action's frontmatter text
 *  may carry " (due: …)" after the analysis text. */
function evidenceAt(items: { text: string; evidence?: number[] }[] | undefined, i: number, text: string, count: number) {
  if (!items || items.length !== count) return []
  const item = items[i]
  if (!item || !text.trim().startsWith(item.text.trim())) return []
  return item.evidence ?? []
}

/** A jump to evidence lines in the Transcript tab. */
interface EvidenceJump {
  seqs: number[]
  index: number
  /** The tab the jump started from, for "Back to …". */
  from: MeetingTab
  /** Move focus to the line: on arrival, not on next/previous (focus stays on the arrows). */
  focus: boolean
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
  const { t } = useTranslation()
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
  // Which providers the setup shows ("" shows all, like Custom).
  const [setup, setSetup] = useState<Setup>("")
  const [improveOpen, setImproveOpen] = useState(false)
  const [improving, setImproving] = useState(false)
  const [improveError, setImproveError] = useState<string | null>(null)
  // Why "Improve with Soniox" left the summary as it was (summaries not set up).
  const [improveSkipped, setImproveSkipped] = useState<string | null>(null)
  // Export menu: busy guard and its inline status (null = none).
  const [exporting, setExporting] = useState(false)
  const [exportStatus, setExportStatus] = useState<{ tone: "muted" | "error"; text: string } | null>(null)
  const exportTimer = useRef<number | null>(null)

  useEffect(
    () => () => {
      if (exportTimer.current !== null) window.clearTimeout(exportTimer.current)
    },
    [],
  )

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
        ? t("meetingDetail.summariesReady")
        : t("meetingDetail.summariesNotReady")
      : null,
  )

  const [transcript, setTranscript] = useState<Utterance[] | null>(null)
  const [transcriptError, setTranscriptError] = useState<string | null>(null)
  // The source the loaded transcript was requested as, so a later
  // analysis (or a new summary) that points at the other transcript
  // reloads it instead of jumping to the wrong lines.
  const [transcriptFor, setTranscriptFor] = useState<TranscriptSource | null>(null)
  const groups = useMemo(() => (transcript ? groupBySpeaker(transcript) : []), [transcript])

  // analysis.json and its mind map (docs/plans/active/meeting-insights.md):
  // undefined while loading, null when the meeting has none yet (404).
  // Loaded up front: Overview's "Source" links need it too. Bumping
  // analysisVersion reloads it after a new summary.
  const [analysis, setAnalysis] = useState<MeetingAnalysis | null | undefined>(undefined)
  const [analysisError, setAnalysisError] = useState<string | null>(null)
  const [analysisVersion, setAnalysisVersion] = useState(0)
  useEffect(() => {
    let cancelled = false
    getMeetingAnalysis(meetingId)
      .then((a) => {
        if (cancelled) return
        setAnalysis(a)
        setAnalysisError(null)
      })
      .catch((err: unknown) => {
        if (cancelled) return
        if (err instanceof ApiError && err.status === 404) {
          setAnalysis(null)
          setAnalysisError(null)
        } else {
          setAnalysisError(errorText(err))
        }
      })
    return () => {
      cancelled = true
    }
  }, [meetingId, analysisVersion])
  const reloadAnalysis = () => {
    setAnalysisError(null)
    setAnalysisVersion((v) => v + 1)
  }
  const flowFinder = useFindFlows(meetingId, (a) => {
    setAnalysis(a)
    setAnalysisError(null)
  })

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
        if (cancelled) return
        setSonioxKeySet(c.soniox_api_key_set)
        setSetup(c.setup)
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
    const want = wantedSource
    loadFullTranscript(meetingId, want)
      .then((res) => {
        if (cancelledRef.current) return
        setTranscript(res.utterances)
        setTranscriptFor(want)
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
    const next = meetingTab(value)
    // Leaving the transcript ends a jump; coming back shows it plainly.
    if (next !== "transcript") setJump(null)
    navigate({ ...route, tab: next }, { replace: true })
  }

  // Evidence jumps: a decision, action, mind-map leaf or flow step opens
  // the Transcript tab at its first source line (utt-<seq>), centred and
  // briefly highlighted, with next/previous when it has several.
  const [jump, setJump] = useState<EvidenceJump | null>(null)
  const jumpToEvidence = (seqs: number[]) => {
    const sorted = [...new Set(seqs)].sort((a, b) => a - b)
    if (sorted.length === 0) return
    setJump({ seqs: sorted, index: 0, from: tab, focus: true })
    setTab("transcript")
  }
  const jumpSeq = jump ? jump.seqs[jump.index] : null
  const jumpMissing = jumpSeq !== null && transcript !== null && !transcript.some((u) => u.seq === jumpSeq)
  useEffect(() => {
    if (jumpSeq === null || tab !== "transcript" || !transcript) return
    let el: HTMLElement | null = null
    let timer = 0
    // Next frame: Radix mounts the tab's content a render after the
    // switch, so the row isn't in the DOM yet when this first runs.
    const frame = window.requestAnimationFrame(() => {
      el = document.getElementById(`utt-${jumpSeq}`)
      if (!el) return
      // On arrival, focus the line too (for keyboard and screen-reader
      // users); next/previous leave focus on the arrows.
      if (jump?.focus) el.focus({ preventScroll: true })
      el.scrollIntoView({ block: "center", behavior: prefersReducedMotion() ? "auto" : "smooth" })
      // A DOM attribute rather than state: it's a 2s flash React doesn't render.
      el.dataset.highlight = "true"
      const flashed = el
      timer = window.setTimeout(() => delete flashed.dataset.highlight, 2000)
    })
    return () => {
      window.cancelAnimationFrame(frame)
      window.clearTimeout(timer)
      if (el) delete el.dataset.highlight
    }
  }, [jump, jumpSeq, tab, transcript])
  // Show the transcript the analysis's evidence seqs refer to; with no
  // analysis, the best one (the refined v2 when there is one).
  const wantedSource: TranscriptSource =
    analysis?.analysis?.generated?.transcript === "v2"
      ? "v2"
      : analysis?.analysis?.generated?.transcript === "live"
        ? "live"
        : "best"
  // A loaded transcript of the other source is replaced by refetching
  // (async), not cleared here, so the effect never sets state itself.
  const onSourceChanged = useEffectEvent(() => {
    if (transcriptFor === null || transcriptFor === wantedSource) return
    fetchTranscript()
  })
  useEffect(() => onSourceChanged(), [wantedSource])
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
      setRenameError(t("meetingDetail.titleRequired"))
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
      setRenameError(t("meetingDetail.renameFailed", { reason: sentence(errorText(err)) }))
    } finally {
      setRenaming(false)
    }
  }

  const setExportNotice = (tone: "muted" | "error", text: string) => {
    if (exportTimer.current !== null) window.clearTimeout(exportTimer.current)
    setExportStatus({ tone, text })
    if (tone === "muted") {
      exportTimer.current = window.setTimeout(() => setExportStatus(null), 4000)
    }
  }

  const exportLang = () => (i18next.resolvedLanguage === "vi" ? "vi" : "en")

  const copyExport = async (format: "txt" | "md") => {
    // No await before copyText: it must start inside the click (see clipboard.ts).
    await copyText(fetchExportText(meetingId, format, exportLang()))
    setExportNotice("muted", t("meetingDetail.exportCopied"))
  }

  const downloadExport = async (format: ExportFormat) => {
    if (hasWindowControls()) {
      const path = await saveExport(meetingId, format, exportLang())
      if (path) setExportNotice("muted", t("meetingDetail.exportSaved", { path }))
      return
    }
    const a = document.createElement("a")
    a.href = exportUrl(meetingId, format, exportLang())
    a.download = ""
    a.click()
  }

  const runExport = (work: () => Promise<void>) => {
    if (exportTimer.current !== null) window.clearTimeout(exportTimer.current)
    setExportStatus(null)
    setExporting(true)
    work()
      .catch((err) => setExportNotice("error", t("meetingDetail.exportFailed", { error: errorText(err) })))
      .finally(() => setExporting(false))
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
      setDeleteError(t("meetingDetail.deleteFailed", { reason: sentence(errorText(err)) }))
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
      reloadAnalysis()
    } catch (err) {
      // Nothing was sent yet: ask, and retry once the user agrees.
      if (err instanceof RefineError && err.kind === "consent_required") {
        setConsentOpen(true)
      } else {
        setError(refineErrorText(err, setup))
        const notSetUp = err instanceof RefineError && (err.kind === "summary_not_ready" || err.kind === "quota_exceeded")
        setNeedsSetup(notSetUp)
        // The card should now show what's missing, not the old "ready".
        if (notSetUp) refreshSummaryStatus()
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
      // if_ready: a better transcript must not wait on summaries being
      // set up; the server says why when it skipped the summary.
      const m = await refineMeeting(meetingId, { engine: "soniox", summarize: "if_ready" })
      setImproveSkipped(m.summary_skipped ?? null)
      setMeeting(m)
      reloadAnalysis()
      setImproveOpen(false)
    } catch (err) {
      setImproveError(refineErrorText(err, setup))
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
      setCloudError(t("meetingDetail.cloudSwitchFailed", { reason: sentence(errorText(err)) }))
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
      setError(t("meetingDetail.actionUpdateFailed", { item: item.text, reason: sentence(errorText(err)) }))
    })
  }

  if (!meeting && !error) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-8" aria-busy="true">
        <StatusMessage className="sr-only">{t("meetingDetail.loadingMeeting")}</StatusMessage>
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
          <span aria-hidden="true">←</span> {t("meetingDetail.backToMeetings")}
        </Button>
        <StatusMessage tone="error" className="mt-4">
          {t("meetingDetail.loadFailedWith", { error: error ? ` (${error})` : "" })}
        </StatusMessage>
      </div>
    )
  }

  const refined = Boolean(meeting.refined)
  // Offered for an ended meeting transcribed locally (or not at all)
  // that hasn't already had a Soniox pass, when a key is set.
  const showImprove =
    Boolean(meeting.ended_at) &&
    meeting.engine !== "soniox" &&
    meeting.refined_with !== "soniox" &&
    sonioxKeySet === true &&
    isOptionVisible(setup, "transcription", "soniox")
  // Summaries are manual: ending a meeting doesn't create one, so an
  // ended, unsummarized meeting leads with the call to action.
  const showSummaryCta = !refined && Boolean(meeting.ended_at)
  const findHint = meeting.ended_at
    ? t("meetingDetail.findHintEnded")
    : t("meetingDetail.findHintLive")
  const summaryButton = (
    <Button onClick={handleRefine} disabled={refining} variant={refined ? "outline" : "default"}>
      {refining ? (
        <Loader2 aria-hidden="true" className="size-4 motion-safe:animate-spin" />
      ) : (
        !refined && <Sparkles aria-hidden="true" />
      )}
      {refining ? t("meetingDetail.creatingSummary") : refined ? t("meetingDetail.redoSummary") : t("meetingDetail.createSummary")}
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
  const offerCloud =
    (!usingCloud || cloudNeedsLogin || !cloud?.consent) && isOptionVisible(setup, "summaries", "catchm-cloud")
  // The card offers only what the setup shows: Ollama on this computer,
  // or (under Cloud, in its place) the user's own Cloudflare account.
  const offerLocal = isOptionVisible(setup, "summaries", "ollama")
  const offerCloudflare = !offerLocal && isOptionVisible(setup, "summaries", "openai")

  // Evidence for Overview's "Source" links, when the Transcript tab shows
  // the lines the analysis cites.
  const insight = analysis?.analysis
  const canJump = insight ? evidenceMatchesTranscript(insight) : false
  const decisionEvidence = (i: number, text: string) =>
    canJump ? evidenceAt(insight?.decisions, i, text, meeting.decisions?.length ?? 0) : []
  const actionEvidence = (i: number, text: string) =>
    canJump ? evidenceAt(insight?.actions, i, text, meeting.action_items?.length ?? 0) : []
  const sourceLink = (evidence: number[]) =>
    evidence.length > 0 && <SourceLink onClick={() => jumpToEvidence(evidence)} />

  return (
    // The graphs get more room than the reading width text tabs keep.
    <div className={`mx-auto px-6 py-8 ${tab === "mindmap" || tab === "flows" ? "max-w-6xl" : "max-w-3xl"}`}>
      <Button variant="ghost" onClick={onBack} className="mb-4">
        <span aria-hidden="true">←</span> {t("meetingDetail.backToMeetings")}
      </Button>

      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-start gap-2">
            <h1 className="min-w-0 text-balance break-words text-2xl font-semibold">{meeting.title}</h1>
            {refined && (
              <Badge variant="secondary" className="mt-1.5 shrink-0">
                {t("meetingDetail.summaryReadyBadge")}
              </Badge>
            )}
            <Button
              variant="ghost"
              size="icon"
              className="mt-0.5 size-7 shrink-0 text-muted-foreground"
              onClick={openRename}
              aria-label={t("meetingDetail.renameAria")}
            >
              <Pencil aria-hidden="true" className="size-4" />
            </Button>
          </div>
          <p className="text-sm text-muted-foreground">{formatDateRange(meeting.started_at, meeting.ended_at)}</p>
          <MeetingOrganize meeting={meeting} onSaved={setMeeting} />
        </div>
        <div className="flex gap-2">
          {refined && summaryButton}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" disabled={exporting}>
                <Download aria-hidden="true" /> {t("meetingDetail.export")}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => runExport(() => copyExport("txt"))}>
                <Copy aria-hidden="true" /> {t("meetingDetail.exportCopyText")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => runExport(() => copyExport("md"))}>
                <FileText aria-hidden="true" /> {t("meetingDetail.exportCopyMarkdown")}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={() => runExport(() => downloadExport("txt"))}>
                <FileText aria-hidden="true" /> {t("meetingDetail.exportTxt")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => runExport(() => downloadExport("md"))}>
                <FileCode aria-hidden="true" /> {t("meetingDetail.exportMd")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => runExport(() => downloadExport("docx"))}>
                <FileType aria-hidden="true" /> {t("meetingDetail.exportDocx")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Button variant="destructive" onClick={openDelete}>
            <Trash2 aria-hidden="true" /> {t("meetingDetail.delete")}
          </Button>
        </div>
      </div>

      <StatusMessage tone={exportStatus?.tone ?? "muted"} className="mb-4">
        {exportStatus?.text}
      </StatusMessage>

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
            <AudioLines aria-hidden="true" />
            {t("meetingDetail.improveButton")}
          </Button>
          <p id="improve-soniox-desc" className="min-w-0 flex-1 text-sm text-muted-foreground">
            {t("meetingDetail.improveDesc")}
          </p>
        </div>
      )}

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
            {t("statusRow.openSettings")}
          </Button>
        )}
      </div>
      <StatusMessage className="sr-only">{summaryAnnouncement}</StatusMessage>
      {meeting.refined_with === "soniox" && (
        <StatusMessage className="mb-4">
          {improveSkipped
            ? t("meetingDetail.improvedSkipped", { reason: improveSkipped })
            : t("meetingDetail.improved")}
        </StatusMessage>
      )}
      {refined && (
        <StatusMessage className="mb-4">
          {refining ? t("meetingDetail.creatingNewSummary") : null}
        </StatusMessage>
      )}

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className="mb-6 flex w-fit max-w-full overflow-x-auto">
          <TabsTrigger value="overview">{t("meetingDetail.tabOverview")}</TabsTrigger>
          <TabsTrigger value="mindmap">{t("meetingDetail.tabMindmap")}</TabsTrigger>
          <TabsTrigger value="flows">{t("meetingDetail.tabFlows")}</TabsTrigger>
          <TabsTrigger value="transcript">
            {t("meetingDetail.tabTranscript")}
            {meeting.refined_with === "soniox" && (
              <Badge variant="secondary">
                Soniox
              </Badge>
            )}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          {showSummaryCta &&
            (setupNeeded ? (
              <Card className="mb-8">
                <CardHeader>
                  <CardTitle className="text-lg font-semibold">
                    <h2 className="flex items-center gap-2">
                      <Sparkles aria-hidden="true" className="size-4" />
                      {t("meetingDetail.setupTitle")}
                    </h2>
                  </CardTitle>
                  <CardDescription className="text-pretty">
                    {summaryStatus.reason && `${sentence(summaryStatus.reason)} `}
                    {t("meetingDetail.setupDesc")}
                  </CardDescription>
                </CardHeader>
                <CardContent className="grid gap-3">
                  <ul className="grid gap-3 sm:grid-cols-2">
                    {offerLocal && (
                      <li className="grid content-between justify-items-start gap-3 rounded-lg border border-border p-3">
                        <div>
                          <h3 className="flex items-center gap-2 text-sm font-medium">
                            <HardDrive aria-hidden="true" className="size-4" />
                            {t("labels.engine.whisper")}
                          </h3>
                          <p id="summary-setup-local-desc" className="mt-1 text-sm text-muted-foreground">
                            {localGb != null
                              ? t("meetingDetail.localDescGb", { size: formatGb(localGb) })
                              : t("meetingDetail.localDescFewGb")}
                          </p>
                        </div>
                        <Button
                          size="sm"
                          onClick={() => navigate({ name: "settings", section: "summaries" })}
                          aria-describedby="summary-setup-local-desc"
                        >
                          {t("meetingDetail.setupLocalButton")}
                        </Button>
                      </li>
                    )}
                    {offerCloudflare && (
                      <li className="grid content-between justify-items-start gap-3 rounded-lg border border-border p-3">
                        <div>
                          <h3 className="flex items-center gap-2 text-sm font-medium">
                            <Cloud aria-hidden="true" className="size-4" />
                            {t("setup.options.cloudflare")}
                          </h3>
                          <p id="summary-setup-cloudflare-desc" className="mt-1 text-sm text-muted-foreground">
                            {t("setup.options.cloudflareDesc")}
                          </p>
                        </div>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => navigate({ name: "settings", section: "summaries" })}
                          aria-describedby="summary-setup-cloudflare-desc"
                        >
                          {t("getStarted.setUp")}
                        </Button>
                      </li>
                    )}
                    {offerCloud && (
                      <li className="grid content-between justify-items-start gap-3 rounded-lg border border-border p-3">
                        <div>
                          <h3 className="flex items-center gap-2 text-sm font-medium">
                            <Cloud aria-hidden="true" className="size-4" />
                            {t("meetingDetail.catchmCloud")}
                          </h3>
                          <p id="summary-setup-cloud-desc" className="mt-1 text-sm text-muted-foreground">
                            {t("meetingDetail.cloudDesc", { count: cloud?.limit ?? CLOUD_DEFAULT_LIMIT })}
                            {cloudNeedsLogin && t("meetingDetail.cloudLoginHint")}
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
                            {cloudSaving ? t("meetingOrganize.saving") : t("meetingDetail.useCloud")}
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
                    className="h-auto justify-self-start px-0 text-muted-foreground"
                    onClick={() => void handleRefine()}
                  >
                    {t("meetingDetail.tryAnyway")}
                  </Button>
                </CardContent>
              </Card>
            ) : (
              <Card className="mb-8">
                <CardHeader>
                  <CardTitle className="text-lg font-semibold">
                    <h2 className="flex items-center gap-2">
                      <Sparkles aria-hidden="true" className="size-4" />
                      {t("meetingDetail.getCtaTitle")}
                    </h2>
                  </CardTitle>
                  <CardDescription className="text-pretty">{summaryHelp()}</CardDescription>
                </CardHeader>
                <CardContent>
                  {checkingSetup ? <Skeleton aria-hidden="true" className="h-8 w-60" /> : summaryButton}
                </CardContent>
              </Card>
            ))}
          <Section title={t("meetingDetail.sectionDecisions")} icon={<GitBranch aria-hidden="true" className="size-4" />}>
            {meeting.decisions && meeting.decisions.length > 0 ? (
              <ul className="grid gap-2 text-base">
                {meeting.decisions.map((d, i) => (
                  <li key={i} className="break-words">
                    {d.text} {sourceLink(decisionEvidence(i, d.text))}
                    {d.scope && <span className="block text-sm text-muted-foreground">{d.scope}</span>}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">
                {refined ? t("meetingDetail.noDecisionsFound") : `${t("meetingDetail.noDecisionsYet")} ${findHint}`}
              </p>
            )}
          </Section>

          <Section title={t("sidebar.actionItems")} icon={<CheckSquare aria-hidden="true" className="size-4" />}>
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
                        {a.due && <span>{t("meetingDetail.due", { date: formatDay(a.due) })}</span>}
                        {sourceLink(actionEvidence(i, a.text))}
                      </div>
                    </div>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">
                {refined
                  ? t("meetingDetail.noActionsFound")
                  : `${t("meetingDetail.noActionsYet")} ${findHint}`}
              </p>
            )}
          </Section>

          <Section title={t("meetingDetail.sectionSummary")}>
            {meeting.summary ? (
              <p className="break-words text-base">{meeting.summary}</p>
            ) : (
              <p className="text-sm text-muted-foreground">
                {refined ? t("meetingDetail.noSummaryFound") : showSummaryCta
                  ? t("meetingDetail.noSummaryCta")
                  : t("meetingDetail.noSummaryEnd")}
              </p>
            )}
          </Section>

          <Section title={t("meetingDetail.sectionParticipants")}>
            {meeting.participants && meeting.participants.length > 0 ? (
              <p className="break-words text-base">{meeting.participants.join(", ")}</p>
            ) : (
              <p className="text-sm text-muted-foreground">{t("meetingDetail.noParticipants")}</p>
            )}
          </Section>

          <Section title={t("meetingDetail.sectionNotes")}>
            {meeting.notes && meeting.notes.length > 0 ? (
              <ul className="grid gap-1 text-base">
                {meeting.notes.map((n, i) => (
                  <li key={i} className="break-words">
                    {n}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-sm text-muted-foreground">{t("meetingDetail.noNotes")}</p>
            )}
          </Section>
        </TabsContent>

        <TabsContent value="mindmap">
          <MindMapTab
            data={analysis}
            error={analysisError}
            onRetry={reloadAnalysis}
            ended={Boolean(meeting.ended_at)}
            refined={refined}
            summaryAction={summaryButton}
            onJump={jumpToEvidence}
          />
        </TabsContent>

        <TabsContent value="flows">
          <FlowsTab
            data={analysis}
            error={analysisError}
            onRetry={reloadAnalysis}
            ended={Boolean(meeting.ended_at)}
            refined={refined}
            summaryAction={summaryButton}
            finder={flowFinder}
            onJump={jumpToEvidence}
          />
        </TabsContent>

        <TabsContent value="transcript">
          {jump && (
            <EvidenceBar
              index={jump.index}
              total={jump.seqs.length}
              missing={jumpMissing}
              from={jump.from}
              onStep={(delta) =>
                setJump({ ...jump, index: Math.min(jump.seqs.length - 1, Math.max(0, jump.index + delta)), focus: false })
              }
              onBack={() => setTab(jump.from)}
              onClose={() => setJump(null)}
            />
          )}
          {transcriptError ? (
            <div className="grid justify-items-start gap-3">
              <StatusMessage tone="error">
                {t("meetingDetail.transcriptLoadFailed", { error: transcriptError })}
              </StatusMessage>
              <Button size="sm" variant="outline" onClick={retryTranscript}>
                {t("common.tryAgain")}
              </Button>
            </div>
          ) : transcript === null ? (
            <div className="grid gap-2" aria-busy="true">
              <StatusMessage className="sr-only">{t("meetingDetail.loadingTranscript")}</StatusMessage>
              {[0, 1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-4 w-full" />
              ))}
            </div>
          ) : transcript.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("meetingDetail.noTranscript")}</p>
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
                      // utt-<seq>: the target of evidence jumps, flashed by data-highlight.
                      <div
                        key={u.seq}
                        id={`utt-${u.seq}`}
                        tabIndex={-1}
                        className="-mx-2 rounded-md px-2 py-0.5 motion-safe:transition-colors motion-safe:duration-500 data-[highlight=true]:bg-primary/10 data-[highlight=true]:ring-2 data-[highlight=true]:ring-ring/60"
                      >
                        <p className="break-words text-base">{u.text}</p>
                        {u.translated && (
                          <p className="break-words text-base text-muted-foreground">
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
              <DialogTitle>{t("meetingDetail.renameTitle")}</DialogTitle>
              <DialogDescription>{t("meetingDetail.renameDesc")}</DialogDescription>
            </DialogHeader>
            <div className="grid gap-1.5 py-4">
              <Label htmlFor="rename-title">{t("startMeeting.title")}</Label>
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
                {renaming ? t("meetingDetail.renaming") : t("meetingDetail.renameMeeting")}
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
              isSelfHostVisible(setup) ? t("meetingDetail.consentDeclined") : t("meetingDetail.consentDeclinedCloud"),
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
        title={t("meetingDetail.improveConfirmTitle")}
        description={
          <>
            <span className="block">
              {t("meetingDetail.improveConfirmBody")}
            </span>
            <span className="mt-2 block">
              {t("meetingDetail.estimatedCost", { cost: sonioxCost(meeting) })}{" "}
              {sonioxPrivacy()}
            </span>
          </>
        }
        confirmLabel={improving ? t("meetingDetail.improving") : t("meetingDetail.improveButton")}
        pending={improving}
        error={improveError}
        onConfirm={() => void handleImprove()}
      />

      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("meetingDetail.deleteConfirmTitle")}
        description={<>{t("meetingDetail.deleteConfirmBody", { title: meeting.title })}</>}
        confirmLabel={deleting ? t("meetingDetail.deleting") : t("meetingDetail.deleteMeeting")}
        destructive
        pending={deleting}
        error={deleteError}
        onConfirm={() => void handleDelete()}
      />
    </div>
  )
}
