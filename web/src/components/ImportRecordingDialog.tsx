import { useEffect, useRef, useState, type ChangeEvent, type FormEvent } from "react"
import { Loader2, Upload } from "lucide-react"
import { useTranslation } from "react-i18next"

import { ProjectSelectField } from "@/components/Projects"
import { SegmentedControl } from "@/components/settings/pieces"
import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
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
import { Progress } from "@/components/ui/progress"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Toggle } from "@/components/ui/toggle"
import { ArrowLeftRight } from "lucide-react"
import { ApiError, getConfig, importRecording, type Meeting } from "@/lib/api"
import { noAutofill } from "@/lib/inputProps"
import type { TFunction } from "i18next"
import { engineLabel } from "@/lib/labels"
import { LANGUAGES, languageLabel } from "@/lib/languages"
import { choiceProject, readLastProject, refreshProjects, useProjects, writeLastProject, type ProjectChoice } from "@/lib/projects"

/** Radix Select can't hold an item with value ""; stands in for "unset". */
const UNSET_LANG = "__unset__"

const ACCEPT = ".wav,.mp3,.m4a,.mp4,.ogg,.webm,.flac,.aac"

const SONIOX_RATE_PER_HOUR = 0.1

const errorText = (err: unknown) => (err instanceof Error ? err.message : String(err))

const titleFromFile = (name: string) => name.replace(/\.[^.]+$/, "")

const isWav = (file: File) => /\.wav$/i.test(file.name) || file.type === "audio/wav"

/** "about $0.05" from seconds at Soniox's rate. */
function estimateCost(t: TFunction, seconds: number): string {
  const dollars = (seconds / 3600) * SONIOX_RATE_PER_HOUR
  return t("importRecording.costLine", { cost: dollars.toFixed(2) })
}

/** "12 min 34 s" style, rounded. */
function formatDuration(t: TFunction, seconds: number): string {
  const m = Math.round(seconds / 60)
  if (m < 60) return t("importRecording.durationMin", { minutes: m })
  const h = Math.floor(m / 60)
  return t("importRecording.durationHourMin", { hours: h, minutes: m % 60 })
}

/** Reads an audio file's duration in the browser; resolves null when the
 *  browser can't decode the metadata (unsupported container, etc.). */
function readDuration(file: File): Promise<number | null> {
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file)
    const audio = new Audio()
    const done = (v: number | null) => {
      URL.revokeObjectURL(url)
      resolve(v)
    }
    audio.preload = "metadata"
    audio.onloadedmetadata = () =>
      done(Number.isFinite(audio.duration) ? audio.duration : null)
    audio.onerror = () => done(null)
    audio.src = url
  })
}

interface ImportRecordingDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** On success: the parent navigates to the new meeting. */
  onImported: (meeting: Meeting) => void
  /** Open a meeting by id (the "saved but not transcribed" case). */
  onOpenMeeting: (id: string) => void
  /** Jump to a Settings section (the Soniox key link). */
  onOpenSettings?: () => void
}

export function ImportRecordingDialog({ open, onOpenChange, onImported, onOpenMeeting, onOpenSettings }: ImportRecordingDialogProps) {
  const { t } = useTranslation()
  const [file, setFile] = useState<File | null>(null)
  const [title, setTitle] = useState("")
  const [lang, setLang] = useState("")
  const [translateTo, setTranslateTo] = useState("")
  const [twoWay, setTwoWay] = useState(false)
  const [engine, setEngine] = useState<"soniox" | "whisper">("soniox")
  const [sonioxKeySet, setSonioxKeySet] = useState<boolean | null>(null)
  const [duration, setDuration] = useState<number | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [progress, setProgress] = useState<number | null>(null)
  const [uploadDone, setUploadDone] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [failedMeetingId, setFailedMeetingId] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const projects = useProjects()
  // Defaults to the last used project (if it still exists).
  const [projectChoice, setProjectChoice] = useState<ProjectChoice>(() => ({ name: readLastProject(), isNew: false }))
  const [projectError, setProjectError] = useState<string | null>(null)

  // Key state and a fresh form each time the dialog opens.
  useEffect(() => {
    if (!open) return
    let cancelled = false
    void refreshProjects()
    getConfig()
      .then((c) => {
        if (!cancelled) setSonioxKeySet(c.soniox_api_key_set)
      })
      .catch(() => {
        if (!cancelled) setSonioxKeySet(null)
      })
    return () => {
      cancelled = true
    }
  }, [open])

  // Fresh form on open (the same event that opens the dialog), not in
  // an effect: closing always passes through here too, so the dialog is
  // blank the next time it opens.
  const resetForm = () => {
    setFile(null)
    if (fileInputRef.current) fileInputRef.current.value = ""
    setTitle("")
    setLang("")
    setTranslateTo("")
    setTwoWay(false)
    setDuration(null)
    setError(null)
    setFailedMeetingId(null)
    setProgress(null)
    setUploadDone(false)
    setProjectChoice({ name: readLastProject(), isNew: false })
    setProjectError(null)
  }

  useEffect(() => {
    if (!open) return
    const id = window.setTimeout(() => fileInputRef.current?.focus(), 50)
    return () => window.clearTimeout(id)
  }, [open])

  const pickFile = (e: ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0] ?? null
    setError(null)
    setFailedMeetingId(null)
    setFile(f)
    setDuration(null)
    if (f) {
      setTitle((t) => t || titleFromFile(f.name))
      void readDuration(f).then(setDuration)
    }
  }

  const whisperDisabled = file !== null && !isWav(file)
  const sonioxDisabled = sonioxKeySet === false
  // Keep the selection usable when a file's format disables it.
  // Fall back to whichever engine is usable, so the form never sits on
  // a disabled choice (and never shows Soniox's cost/privacy line while
  // the file would really be transcribed on this computer).
  const activeEngine: "soniox" | "whisper" =
    engine === "whisper" && whisperDisabled ? "soniox" : engine === "soniox" && sonioxDisabled && !whisperDisabled ? "whisper" : engine
  const showCost = activeEngine === "soniox" && duration !== null

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (submitting || !file) return
    const project = choiceProject(projectChoice, projects)
    if (projectChoice.isNew && !project) {
      setProjectError(t("startMeeting.projectError"))
      document.getElementById("import-project-name")?.focus()
      return
    }
    setProjectError(null)
    setSubmitting(true)
    setError(null)
    setFailedMeetingId(null)
    setProgress(null)
    setUploadDone(false)
    try {
      const m = await importRecording(
        file,
        {
          title: title.trim() || undefined,
          lang: lang || undefined,
          translate_to: translateTo || undefined,
          two_way: twoWay || undefined,
          engine: activeEngine,
          project: project || undefined,
        },
        (fraction) => {
          setProgress(Math.round(fraction * 100))
          if (fraction >= 1) setUploadDone(true)
        },
      )
      writeLastProject(project)
      void refreshProjects()
      onImported(m)
      onOpenChange(false)
    } catch (err) {
      const meetingId =
        err instanceof ApiError && typeof err.body?.meeting_id === "string" ? err.body.meeting_id : null
      if (meetingId) {
        setFailedMeetingId(meetingId)
        setError(t("importRecording.savedNotTranscribed", { error: errorText(err) }))
      } else {
        setError(t("importRecording.genericError", { error: errorText(err) }))
      }
    } finally {
      setSubmitting(false)
      setProgress(null)
    }
  }

  const status = submitting
    ? uploadDone || progress === null
      ? t("importRecording.transcribing")
      : t("importRecording.uploading", { progress })
    : null

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && submitting) return
        // Reset as the dialog closes (parent sets open too), so it's
        // blank the next time it opens.
        if (!next) resetForm()
        onOpenChange(next)
      }}
    >
      <DialogContent>
        <form onSubmit={submit} noValidate className="grid gap-4" aria-busy={submitting || undefined}>
          <DialogHeader>
            <DialogTitle>{t("importRecording.title")}</DialogTitle>
            <DialogDescription>{t("importRecording.description")}</DialogDescription>
          </DialogHeader>

          <div className="grid gap-1.5">
            <Label htmlFor="import-file">{t("importRecording.file")}</Label>
            <Input
              ref={fileInputRef}
              id="import-file"
              name="file"
              type="file"
              accept={ACCEPT}
              onChange={pickFile}
              aria-describedby="import-file-hint"
              className="w-full"
            />
            <p id="import-file-hint" className="text-xs text-muted-foreground">
              {t("importRecording.fileHint")}
              {file && duration !== null && ` ${t("importRecording.lengthAbout", { duration: formatDuration(t, duration) })}`}
            </p>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="import-title">{t("startMeeting.title")}</Label>
            <Input
              id="import-title"
              name="title"
              {...noAutofill}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={file ? titleFromFile(file.name) : t("importRecording.titlePlaceholder")}
            />
          </div>

          <ProjectSelectField
            id="import-project"
            value={projectChoice}
            onChange={(c) => {
              setProjectChoice(c)
              setProjectError(null)
            }}
            projects={projects}
            error={projectError}
          />

          <div role="group" aria-labelledby="import-translation-label" className="grid gap-1.5">
            <div className="flex items-center justify-between">
              <span id="import-translation-label" className="text-sm font-medium">
                {t("startMeeting.translation")}
              </span>
              <Toggle
                size="sm"
                variant="outline"
                pressed={twoWay}
                onPressedChange={setTwoWay}
                aria-describedby="import-two-way-hint"
              >
                <ArrowLeftRight aria-hidden="true" />
                {t("startMeeting.twoWayLabel")}
              </Toggle>
            </div>
            <div className="flex items-center gap-2">
              <Select value={lang || (twoWay ? undefined : UNSET_LANG)} onValueChange={(v) => setLang(v === UNSET_LANG ? "" : v)}>
                <SelectTrigger
                  id="import-lang"
                  className="w-40"
                  aria-label={twoWay ? t("startMeeting.firstLanguage") : t("startMeeting.spokenLanguage")}
                >
                  <SelectValue placeholder={t("startMeeting.firstLanguage")} />
                </SelectTrigger>
                <SelectContent>
                  {!twoWay && <SelectItem value={UNSET_LANG}>{t("startMeeting.detectAuto")}</SelectItem>}
                  {LANGUAGES.map((l) => (
                    <SelectItem key={l.code} value={l.code}>
                      {languageLabel(l.code)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <span aria-hidden="true" className="text-sm text-muted-foreground">
                {twoWay ? "↔" : "→"}
              </span>
              <Select value={translateTo || (twoWay ? undefined : UNSET_LANG)} onValueChange={(v) => setTranslateTo(v === UNSET_LANG ? "" : v)}>
                <SelectTrigger
                  id="import-translate-to"
                  className="w-40"
                  aria-label={twoWay ? t("startMeeting.secondLanguage") : t("startMeeting.translateInto")}
                >
                  <SelectValue placeholder={t("startMeeting.secondLanguage")} />
                </SelectTrigger>
                <SelectContent>
                  {!twoWay && <SelectItem value={UNSET_LANG}>{t("startMeeting.noTranslation")}</SelectItem>}
                  {LANGUAGES.map((l) => (
                    <SelectItem key={l.code} value={l.code}>
                      {languageLabel(l.code)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <p id="import-two-way-hint" className="text-xs text-muted-foreground">
              {twoWay ? t("startMeeting.twoWayHint") : t("startMeeting.twoWayOffHint")}
            </p>
          </div>

          <div className="grid gap-1.5">
            <span id="import-engine-label" className="text-sm font-medium">
              {t("startMeeting.transcribeWith")}
            </span>
            <SegmentedControl
              id="import-engine"
              labelledBy="import-engine-label"
              describedBy="import-engine-reason"
              value={activeEngine}
              onChange={setEngine}
              options={[
                {
                  value: "soniox",
                  label: engineLabel("soniox"),
                  disabled: sonioxDisabled,
                  reason: sonioxDisabled ? t("importRecording.sonioxNoKey") : undefined,
                },
                {
                  value: "whisper",
                  label: engineLabel("whisper"),
                  disabled: whisperDisabled,
                  reason: whisperDisabled ? t("importRecording.whisperFormat") : undefined,
                },
              ]}
            />
            <p id="import-engine-reason" className="text-xs text-muted-foreground">
              {activeEngine === "soniox" ? t("importRecording.sonioxHintShort") : t("importRecording.freePrivate")}
            </p>
            {sonioxDisabled && onOpenSettings && !submitting && (
              <div>
                <Button type="button" size="sm" variant="outline" onClick={onOpenSettings}>
                  {t("importRecording.openTranscriptionSettings")}
                </Button>
              </div>
            )}
          </div>

          {activeEngine === "soniox" && (
            <p className="text-xs text-muted-foreground">
              {showCost && `${estimateCost(t, duration ?? 0)} `}
              {t("meetingDetail.sonioxPrivacy")}
            </p>
          )}

          {submitting && progress !== null && !uploadDone && (
            <Progress value={progress} aria-label={t("importRecording.uploadProgress")} />
          )}
          <StatusMessage id="import-status">{status}</StatusMessage>
          <StatusMessage id="import-error" tone="error" className="break-words">
            {error}
          </StatusMessage>
          {failedMeetingId && !submitting && (
            <div>
              <Button type="button" size="sm" variant="outline" onClick={() => onOpenMeeting(failedMeetingId)}>
                {t("importRecording.openMeeting")}
              </Button>
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" disabled={submitting} onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={submitting || !file} aria-busy={submitting || undefined}>
              {submitting ? <Loader2 aria-hidden="true" className="motion-safe:animate-spin" /> : <Upload aria-hidden="true" />}
              {submitting ? t("importRecording.importing") : t("importRecording.importRecording")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
