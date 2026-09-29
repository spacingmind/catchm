import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowDown, Loader2, Minus, Plus, Radio } from "lucide-react"
import { cn } from "cn"

import { ConfirmDialog } from "@/components/ConfirmDialog"
import { SaveStatus, SegmentedControl, type SaveState } from "@/components/settings/pieces"
import { StatusMessage } from "@/components/StatusMessage"
import { CompactOverlay } from "@/components/live/CompactOverlay"
import { StatusRow } from "@/components/live/StatusRow"
import { useCompactMode } from "@/components/live/useCompactMode"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import { useStickToBottom } from "@/hooks/useStickToBottom"
import type { Section as SettingsSection } from "@/components/settings/Settings"
import { addNote, endMeeting, getTranscript, type AudioSource, type Meeting, type Utterance } from "@/lib/api"
import { FONT_SIZES, readFontSize, readViewMode, writeFontSize, writeViewMode, type TranscriptViewMode } from "@/lib/livePrefs"
import { readStartedDevice, writeStartedDevice } from "@/lib/recordPrefs"

/** How often the live transcript is polled for new utterances. */
const POLL_INTERVAL_MS = 1500

/** How long the note's "Saved" confirmation stays before clearing. */
const NOTE_SAVED_MS = 2000

/** Stable empty list for a meeting whose transcript hasn't loaded yet
 *  (a fresh [] each render would re-run the memo and scroll effects). */
const EMPTY_UTTERANCES: Utterance[] = []

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

/** Consecutive utterances from the same speaker render under one label
 *  (M21 §3), instead of repeating it on every line. */
function groupBySpeaker(utterances: Utterance[]): Utterance[][] {
  const groups: Utterance[][] = []
  for (const u of utterances) {
    const last = groups[groups.length - 1]
    if (last && last[0].speaker === u.speaker) {
      last.push(u)
    } else {
      groups.push([u])
    }
  }
  return groups
}

const UtteranceRow = memo(function UtteranceRow({
  u,
  viewMode,
  fontSize,
}: {
  u: Utterance
  viewMode: TranscriptViewMode
  fontSize: number
}) {
  return (
    <div className={cn(viewMode === "side-by-side" && "grid grid-cols-2 gap-4")}>
      {viewMode !== "translation-only" && (
        <p className="min-w-0 max-w-[72ch] break-words leading-[1.6]" style={{ fontSize }}>
          {u.text}
        </p>
      )}
      {viewMode === "translation-only" && (
        <p className="max-w-[72ch] break-words leading-[1.6]" style={{ fontSize }}>
          {u.translated || u.text}
        </p>
      )}
      {viewMode !== "translation-only" && u.translated && (
        <p className="min-w-0 max-w-[72ch] break-words leading-[1.6] text-muted-foreground" style={{ fontSize }}>
          {u.translated}
        </p>
      )}
    </div>
  )
})

/** One speaker's run of lines. The transcript only ever grows at the
 *  end, so a group is unchanged while its first line and length are —
 *  only the last group re-renders when a line arrives. content-visibility
 *  lets the browser skip laying out groups scrolled far off-screen. */
const SpeakerGroup = memo(
  function SpeakerGroup({
    group,
    viewMode,
    fontSize,
    last,
  }: {
    group: Utterance[]
    viewMode: TranscriptViewMode
    fontSize: number
    /** The newest group is laid out eagerly so stick-to-bottom scrolls
     *  to its real height, not the 4rem placeholder. */
    last: boolean
  }) {
    return (
      <div className={last ? undefined : "[contain-intrinsic-size:auto_4rem] [content-visibility:auto]"}>
        {group[0].speaker && <p className="text-[13px] font-medium text-muted-foreground">{group[0].speaker}</p>}
        <div className="grid gap-2">
          {group.map((u) => (
            <UtteranceRow key={u.seq} u={u} viewMode={viewMode} fontSize={fontSize} />
          ))}
        </div>
      </div>
    )
  },
  (a, b) =>
    a.group[0] === b.group[0] &&
    a.group.length === b.group.length &&
    a.viewMode === b.viewMode &&
    a.fontSize === b.fontSize &&
    a.last === b.last,
)

/** The input a meeting is recording from right now. Starts from the
 *  meeting doc (source) and what the start flow asked for (device, which
 *  the doc doesn't store); StatusRow updates it on each switch. */
interface CaptureInput {
  meetingId: string
  source: AudioSource
  device: string
}

function initialInput(meeting: Meeting): CaptureInput {
  return { meetingId: meeting.id, source: meeting.audio_source ?? "mic", device: readStartedDevice(meeting.id) }
}

interface LiveMeetingProps {
  meeting: Meeting
  onEnded: (meeting: Meeting) => void
  /** M19 capture_error from the start response: the meeting exists but
   *  nothing is being recorded. Must stay visible, never a silent no-op. */
  captureError?: string | null
  /** Where the capture_error banner's "Open settings" goes (App picks
   *  it from the error kind). */
  captureErrorSettings?: SettingsSection
  /** Opens settings at a section so the user can fix the cause (e.g. a
   *  missing API key); used by the capture_error banner and the live
   *  engine error. */
  onOpenSettings?: (section?: SettingsSection) => void
  /** M21 §4: reports compact-overlay mode up to App, which hides the
   *  sidebar/shell while it's active — a 480x260 window has no room
   *  for both. */
  onCompactChange?: (compact: boolean) => void
}

export function LiveMeeting({
  meeting,
  onEnded,
  captureError,
  captureErrorSettings,
  onOpenSettings,
  onCompactChange,
}: LiveMeetingProps) {
  const { t } = useTranslation()
  // Tagged with the meeting it belongs to, so switching meetings shows
  // an empty transcript at once (derived below) without resetting state
  // from the poll effect.
  const [transcript, setTranscript] = useState<{ meetingId: string; utterances: Utterance[] }>(() => ({
    meetingId: meeting.id,
    utterances: [],
  }))
  const utterances = transcript.meetingId === meeting.id ? transcript.utterances : EMPTY_UTTERANCES
  const cursorRef = useRef(-1)
  const [pollError, setPollError] = useState<string | null>(null)

  // Lifted out of StatusRow: it unmounts in compact mode, and the
  // compact strip shows the current source too.
  const [input, setInput] = useState<CaptureInput>(() => initialInput(meeting))
  if (input.meetingId !== meeting.id) setInput(initialInput(meeting))

  const [viewMode, setViewMode] = useState<TranscriptViewMode>(readViewMode)
  const [fontSize, setFontSize] = useState<number>(readFontSize)

  const [note, setNote] = useState("")
  const [noteState, setNoteState] = useState<SaveState>("idle")
  const [noteError, setNoteError] = useState<string | null>(null)
  const noteTimerRef = useRef<number | undefined>(undefined)

  const [confirmEnd, setConfirmEnd] = useState(false)
  const [ending, setEnding] = useState(false)
  const [endError, setEndError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    cursorRef.current = -1

    const poll = () => {
      getTranscript(meeting.id, cursorRef.current)
        .then(({ utterances: batch, cursor }) => {
          if (cancelled) return
          cursorRef.current = cursor
          setPollError(null)
          if (batch.length > 0) {
            setTranscript((prev) =>
              prev.meetingId === meeting.id
                ? { meetingId: meeting.id, utterances: [...prev.utterances, ...batch] }
                : { meetingId: meeting.id, utterances: batch },
            )
          }
        })
        .catch((err: unknown) => {
          if (!cancelled) setPollError(errorText(err))
        })
    }

    poll()
    const id = setInterval(poll, POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [meeting.id])

  useEffect(() => () => window.clearTimeout(noteTimerRef.current), [])

  const groups = useMemo(() => groupBySpeaker(utterances), [utterances])
  // A callback ref on the sentinel below the last row: it re-attaches
  // to the new ScrollArea after a compact-mode round-trip.
  const { ref: bottomRef, hasNew, scrollToBottom } = useStickToBottom(utterances.length)

  const handleAddNote = async () => {
    const text = note.trim()
    if (!text || noteState === "saving") return
    window.clearTimeout(noteTimerRef.current)
    setNoteState("saving")
    setNoteError(null)
    try {
      await addNote(meeting.id, text)
      setNote("")
      setNoteState("saved")
      noteTimerRef.current = window.setTimeout(() => setNoteState("idle"), NOTE_SAVED_MS)
    } catch (err) {
      setNoteState("error")
      setNoteError(t("liveMeeting.noteError", { error: errorText(err) }))
    }
  }

  const handleEnd = async () => {
    setEnding(true)
    setEndError(null)
    try {
      await endMeeting(meeting.id)
      onEnded({ ...meeting, ended_at: new Date().toISOString() })
    } catch (err) {
      setEndError(t("liveMeeting.endError", { error: errorText(err) }))
      setEnding(false)
    }
  }

  const changeFontSize = (delta: 1 | -1) => {
    const i = FONT_SIZES.indexOf(fontSize as (typeof FONT_SIZES)[number])
    const next = FONT_SIZES[Math.min(FONT_SIZES.length - 1, Math.max(0, (i < 0 ? 1 : i) + delta))]
    setFontSize(next)
    writeFontSize(next)
  }

  const changeViewMode = (mode: TranscriptViewMode) => {
    setViewMode(mode)
    writeViewMode(mode)
  }

  const handleSourceChange = useCallback(
    (source: AudioSource, device: string) => {
      setInput({ meetingId: meeting.id, source, device })
      // Remember a mid-meeting mic switch too, so returning to Live (or a
      // later source change) starts from the device actually recording.
      writeStartedDevice(meeting.id, device)
    },
    [meeting.id],
  )

  const translationOn = Boolean(meeting.translate_to) || Boolean(meeting.two_way)
  const pollErrorText = pollError ? t("liveMeeting.pollError", { error: pollError }) : null

  const { compact, available: compactAvailable, toggle: toggleCompact } = useCompactMode()

  // Report the flag up to App, which hides the sidebar/shell while
  // compact mode is active — a 480x260 window has no room for both.
  useEffect(() => {
    onCompactChange?.(compact)
    return () => onCompactChange?.(false)
  }, [compact, onCompactChange])

  if (compact) {
    return (
      <CompactOverlay
        meetingId={meeting.id}
        audioSource={input.source}
        utterances={utterances}
        viewMode={viewMode}
        fontSize={fontSize}
        error={pollError}
        captureError={captureError}
        onExit={toggleCompact}
      />
    )
  }

  const unsavedNote = note.trim() !== ""
  const viewModeOptions: { value: TranscriptViewMode; label: string }[] = [
    { value: "stacked", label: t("liveMeeting.viewStacked") },
    { value: "side-by-side", label: t("liveMeeting.viewSideBySide") },
    { value: "translation-only", label: t("liveMeeting.viewTranslationOnly") },
  ]

  return (
    <div className="mx-auto flex min-h-0 w-full max-w-4xl flex-1 flex-col px-6 py-8">
      <div className="mb-4 flex items-center justify-between gap-4">
        <div className="min-w-0">
          <h1 className="flex items-start gap-2 text-2xl font-semibold text-balance">
            <span aria-hidden="true" className="relative mt-1.5 inline-flex shrink-0">
              <Radio className="size-5 text-primary" />
              <span className="absolute -top-0.5 -right-0.5 size-1.5 motion-safe:animate-pulse rounded-full bg-primary" />
            </span>
            <span className="min-w-0 break-words">{meeting.title}</span>
          </h1>
          <p className="text-sm text-muted-foreground">{t("liveMeeting.subtitle")}</p>
        </div>
        <Button
          variant="destructive"
          className="shrink-0"
          onClick={() => {
            setEndError(null)
            setConfirmEnd(true)
          }}
          disabled={ending}
        >
          {ending ? t("liveMeeting.ending") : t("liveMeeting.endMeeting")}
        </Button>
      </div>

      <ConfirmDialog
        open={confirmEnd}
        onOpenChange={setConfirmEnd}
        title={t("liveMeeting.confirmTitle")}
        description={
          unsavedNote ? t("liveMeeting.confirmDescNote") : t("liveMeeting.confirmDescPlain")
        }
        confirmLabel={t("liveMeeting.endMeeting")}
        destructive
        pending={ending}
        error={endError}
        onConfirm={() => void handleEnd()}
      />

      <StatusMessage tone="error" className="mb-4 break-words">
        {pollErrorText}
      </StatusMessage>

      {captureError && (
        <div role="alert" className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/10 px-4 py-3">
          <p className="min-w-0 break-words text-sm text-destructive">
            <span className="font-medium">{t("liveMeeting.notRecordingLabel")}</span> {captureError}
          </p>
          {onOpenSettings && (
            <Button size="sm" variant="outline" onClick={() => onOpenSettings(captureErrorSettings)}>
              {t("statusRow.openSettings")}
            </Button>
          )}
        </div>
      )}

      <StatusRow
        meetingId={meeting.id}
        startedAt={meeting.started_at}
        engine={meeting.engine}
        source={input.source}
        device={input.device}
        onSourceChange={handleSourceChange}
        onOpenTranscriptionSettings={onOpenSettings && (() => onOpenSettings("transcription"))}
        compactAvailable={compactAvailable}
        onToggleCompact={toggleCompact}
      />

      <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
        {translationOn ? (
          <SegmentedControl label={t("liveMeeting.transcriptView")} value={viewMode} onChange={changeViewMode} options={viewModeOptions} />
        ) : (
          <span />
        )}
        <div role="group" aria-label={t("liveMeeting.textSizeGroup")} className="flex items-center gap-1">
          <Button
            size="icon-sm"
            variant="outline"
            aria-label={t("liveMeeting.textSizeDecrease")}
            disabled={fontSize <= FONT_SIZES[0]}
            onClick={() => changeFontSize(-1)}
          >
            <Minus aria-hidden="true" />
          </Button>
          <Button
            size="icon-sm"
            variant="outline"
            aria-label={t("liveMeeting.textSizeIncrease")}
            disabled={fontSize >= FONT_SIZES[FONT_SIZES.length - 1]}
            onClick={() => changeFontSize(1)}
          >
            <Plus aria-hidden="true" />
          </Button>
        </div>
      </div>

      <div className="relative mb-4 min-h-0 flex-1">
        <ScrollArea className="h-full rounded-xl border border-border bg-card">
          <div role="log" aria-label={t("liveMeeting.transcriptLog")} className="grid gap-3 p-4">
            {groups.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("liveMeeting.waiting")}</p>
            ) : (
              groups.map((group, i) => (
                <SpeakerGroup
                  key={group[0].seq}
                  group={group}
                  viewMode={viewMode}
                  fontSize={fontSize}
                  last={i === groups.length - 1}
                />
              ))
            )}
          </div>
          <div ref={bottomRef} />
        </ScrollArea>
        {hasNew && (
          <Button
            size="sm"
            variant="secondary"
            className="absolute bottom-3 left-1/2 -translate-x-1/2 shadow-md"
            onClick={scrollToBottom}
          >
            <ArrowDown aria-hidden="true" /> {t("liveMeeting.newLines")}
          </Button>
        )}
      </div>

      <form
        className="grid shrink-0 gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          void handleAddNote()
        }}
      >
        <Label htmlFor="live-note">{t("liveMeeting.noteLabel")}</Label>
        <Textarea
          id="live-note"
          name="note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder={t("liveMeeting.notePlaceholder")}
          rows={3}
          aria-invalid={noteState === "error" || undefined}
          aria-describedby="live-note-status"
        />
        <div className="flex items-center gap-3">
          <Button type="submit" disabled={!note.trim() || noteState === "saving"} aria-busy={noteState === "saving" || undefined}>
            {noteState === "saving" && <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />}
            {noteState === "saving" ? t("liveMeeting.adding") : t("liveMeeting.addNote")}
          </Button>
          <SaveStatus id="live-note-status" className="min-w-0 break-words" state={noteState === "saving" ? "idle" : noteState} error={noteError} />
        </div>
      </form>
    </div>
  )
}
