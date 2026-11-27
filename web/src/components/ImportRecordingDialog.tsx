import { useEffect, useRef, useState, type ChangeEvent, type FormEvent } from "react"
import { Loader2, Upload } from "lucide-react"
import { useTranslation } from "react-i18next"

import { ProjectSelectField } from "@/components/Projects"
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
import { ApiError, getConfig, importRecording, type AppConfig, type CloudSttEngine, type Meeting, type SttEngine } from "@/lib/api"
import { noAutofill } from "@/lib/inputProps"
import type { TFunction } from "i18next"
import { engineHint, engineLabel } from "@/lib/labels"
import { languageLabel, sortedLanguageCodes } from "@/lib/languages"
import {
  CLOUD_ENGINES,
  engineKeySet,
  engineOptionName,
  cloudPrivacy,
  fileCost,
  isCloudEngine,
  toEngine,
  visibleEngines,
} from "@/lib/setup"
import { choiceProject, readLastProject, refreshProjects, useProjects, writeLastProject, type ProjectChoice } from "@/lib/projects"

/** Radix Select can't hold an item with value ""; stands in for "unset". */
const UNSET_LANG = "__unset__"

const ACCEPT = ".wav,.mp3,.m4a,.mp4,.ogg,.webm,.flac,.aac"

const errorText = (err: unknown) => (err instanceof Error ? err.message : String(err))

const titleFromFile = (name: string) => name.replace(/\.[^.]+$/, "")

const isWav = (file: File) => /\.wav$/i.test(file.name) || file.type === "audio/wav"

/** "about $0.05" from seconds at the cloud engine's file rate. */
function estimateCost(t: TFunction, engine: CloudSttEngine, seconds: number): string {
  return t("importRecording.costLine", {
    cost: fileCost(engine, seconds),
    rate: CLOUD_ENGINES[engine].fileRatePerHour.toFixed(2),
  })
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
  /** Jump to a Settings section (the API key link). */
  onOpenSettings?: () => void
}

export function ImportRecordingDialog({ open, onOpenChange, onImported, onOpenMeeting, onOpenSettings }: ImportRecordingDialogProps) {
  const { t } = useTranslation()
  const [file, setFile] = useState<File | null>(null)
  const [title, setTitle] = useState("")
  const [lang, setLang] = useState("")
  const [translateTo, setTranslateTo] = useState("")
  const [twoWay, setTwoWay] = useState(false)
  const [engine, setEngine] = useState<SttEngine>("whisper")
  // Which engines the setup shows (Cloud hides Whisper, Self-host the
  // cloud engines) and which keys are set; null until it loads.
  const [config, setConfig] = useState<AppConfig | null>(null)
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
        if (cancelled) return
        setConfig(c)
        // Start from the engine meetings use.
        setEngine(toEngine(c.engine))
      })
      .catch(() => {
        if (!cancelled) setConfig(null)
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

  // Before the config loads every engine is shown and none is blocked
  // (the server still rejects a missing key).
  const shownEngines = visibleEngines(config?.setup ?? "")
  const noKey = (e: SttEngine) => isCloudEngine(e) && config !== null && !engineKeySet(config, e)
  const otherFormatsEngine = shownEngines.find((e) => e !== "whisper" && !noKey(e)) ?? shownEngines.find((e) => e !== "whisper")
  // Why an engine can't take this import, or undefined.
  const disabledReason = (e: SttEngine): string | undefined => {
    if (noKey(e)) return t("importRecording.noKey", { engine: engineLabel(e), provider: engineOptionName(e) })
    if (e === "whisper" && file !== null && !isWav(file)) {
      // "Use …" only when there's another engine to use.
      return otherFormatsEngine
        ? t("importRecording.whisperFormat", { engine: engineLabel(otherFormatsEngine) })
        : t("importRecording.whisperWavConvert")
    }
    return undefined
  }
  // Keep the selection usable when a file's format or a missing key
  // disables it: fall back to the first usable engine, so the form never
  // sits on a disabled choice (and never shows a cloud cost/privacy line
  // while the file would really be transcribed on this computer). When
  // none is usable, the choice stays and Import is blocked.
  const usable = shownEngines.filter((e) => !disabledReason(e))
  const activeEngine: SttEngine = usable.includes(engine) ? engine : (usable[0] ?? (shownEngines.includes(engine) ? engine : shownEngines[0]))
  const activeReason = disabledReason(activeEngine)
  const engineBlocked = activeReason !== undefined
  const missingKey = shownEngines.some(noKey) && (engineBlocked || !shownEngines.some((e) => isCloudEngine(e) && !noKey(e)))
  const cloudActive = isCloudEngine(activeEngine) ? activeEngine : null
  // Why the other choices are greyed out (the Select can't say it).
  const otherReasons = shownEngines
    .filter((e) => e !== activeEngine)
    .map(disabledReason)
    .filter((r): r is string => r !== undefined)

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
                  {sortedLanguageCodes().map((code) => (
                    <SelectItem key={code} value={code}>
                      {languageLabel(code)}
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
                  {sortedLanguageCodes().map((code) => (
                    <SelectItem key={code} value={code}>
                      {languageLabel(code)}
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
            {shownEngines.length > 1 ? (
              <>
                <Label htmlFor="import-engine">{t("startMeeting.transcribeWith")}</Label>
                <Select value={activeEngine} onValueChange={(v) => setEngine(v as SttEngine)}>
                  <SelectTrigger id="import-engine" className="w-full" aria-describedby="import-engine-reason">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {shownEngines.map((e) => (
                      <SelectItem key={e} value={e} disabled={e !== activeEngine && disabledReason(e) !== undefined}>
                        {engineLabel(e)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </>
            ) : (
              <p className="text-sm">
                <span className="font-medium">{t("startMeeting.transcribeWith")}</span> {engineLabel(activeEngine)}
              </p>
            )}
            <p id="import-engine-reason" className="text-xs text-muted-foreground">
              {activeReason ?? (otherReasons.length > 0 ? otherReasons.join(" ") : engineHint(activeEngine))}
            </p>
            {missingKey && onOpenSettings && !submitting && (
              <div>
                <Button type="button" size="sm" variant="outline" onClick={onOpenSettings}>
                  {t("importRecording.openTranscriptionSettings")}
                </Button>
              </div>
            )}
          </div>

          {cloudActive && (
            <p className="text-xs text-muted-foreground">
              {duration !== null && `${estimateCost(t, cloudActive, duration)} `}
              {cloudPrivacy(cloudActive)}
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
            <Button type="submit" disabled={submitting || !file || engineBlocked} aria-busy={submitting || undefined}>
              {submitting ? <Loader2 aria-hidden="true" className="motion-safe:animate-spin" /> : <Upload aria-hidden="true" />}
              {submitting ? t("importRecording.importing") : t("importRecording.importRecording")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
