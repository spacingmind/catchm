import { useId, useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"
import { Pencil, UserRound } from "lucide-react"

import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"

/** Matches meeting.MaxSpeakerName. */
const MAX_NAME = 80

/**
 * A speaker's name over their lines in the transcript. Clicking it opens
 * a small form to name (or rename) that speaker everywhere: transcript,
 * summary, export and MCP (docs/plans/active/speaker-identity.md).
 */
export function SpeakerNamePopover({
  label,
  named,
  current,
  suggestions,
  onSave,
  me,
  onSetMe,
}: {
  /** What the speaker reads as now. */
  label: string
  /** The user confirmed this name (else it's a generic label). */
  named: boolean
  /** The confirmed name, "" when none. */
  current: string
  /** Names to offer: guests first, then the user, then the summary's people. */
  suggestions: readonly string[]
  /** Saves the name ("" removes it); rejects with a message. */
  onSave: (name: string) => Promise<void>
  /** This speaker is the user: "yes" (they said so), "auto" (the
   *  microphone's guess) or "mic" (unlabelled microphone lines: always). */
  me: "no" | "auto" | "yes" | "mic"
  /** Marks this speaker as the user (true) or not (false). */
  onSetMe: (isMe: boolean) => Promise<void>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState(current)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const id = useId()
  const offered = suggestions.filter((s) => s.toLowerCase() !== draft.trim().toLowerCase()).slice(0, 8)

  const run = async (work: () => Promise<void>) => {
    setBusy(true)
    setError(null)
    try {
      await work()
      setOpen(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }
  const save = (name: string) => run(() => onSave(name.trim().replace(/\s+/g, " ")))

  const submit = (e: FormEvent) => {
    e.preventDefault()
    void save(draft)
  }

  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (o) {
          setDraft(current)
          setError(null)
        }
      }}
    >
      <PopoverTrigger asChild>
        <button
          type="button"
          className="group mb-1 inline-flex max-w-full items-center gap-1 rounded-md text-xs font-medium text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          aria-label={t("speakers.renameAria", { name: label })}
        >
          <span className={named || me === "yes" || me === "mic" ? "min-w-0 truncate text-foreground" : "min-w-0 truncate"}>{label}</span>
          {me === "auto" && <span className="font-normal">{t("speakers.autoMe")}</span>}
          <Pencil className="size-3 opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100" aria-hidden />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72">
        <form onSubmit={submit} className="grid gap-4">
          <div className="grid gap-1.5">
            {me === "mic" ? (
              <p className="text-sm">{t("speakers.isYouMic")}</p>
            ) : me === "yes" ? (
              <div className="flex flex-wrap items-center gap-3">
                <p className="min-w-0 flex-1 text-sm">{t("speakers.isYou")}</p>
                <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void run(() => onSetMe(false))}>
                  {t("speakers.notMe")}
                </Button>
              </div>
            ) : (
              <>
                <Button type="button" size="sm" variant={me === "auto" ? "default" : "outline"} disabled={busy} onClick={() => void run(() => onSetMe(true))}>
                  <UserRound aria-hidden />
                  {t("speakers.thisIsMe")}
                </Button>
                {me === "auto" && <p className="text-xs text-muted-foreground">{t("speakers.autoMeHint")}</p>}
              </>
            )}
          </div>
          <div className="grid gap-1.5">
            <label htmlFor={`${id}-name`} className="text-sm font-medium">
              {t("speakers.nameLabel")}
            </label>
            <Input
              id={`${id}-name`}
              value={draft}
              maxLength={MAX_NAME}
              onChange={(e) => setDraft(e.target.value)}
              placeholder={t("speakers.namePlaceholder")}
              autoComplete="off"
              aria-describedby={`${id}-hint`}
              autoFocus
            />
            <p id={`${id}-hint`} className="text-xs text-muted-foreground">
              {t("speakers.nameHint")}
            </p>
          </div>
          {offered.length > 0 && (
            <div className="grid gap-1.5">
              <p className="text-xs font-medium text-muted-foreground">{t("speakers.suggestions")}</p>
              <div className="flex flex-wrap items-center gap-2">
                {offered.map((s) => (
                  <Button key={s} type="button" size="sm" variant="outline" className="rounded-full" onClick={() => setDraft(s)}>
                    {s}
                  </Button>
                ))}
              </div>
            </div>
          )}
          <StatusMessage tone="error" className="text-xs">
            {error}
          </StatusMessage>
          <div className="flex items-center justify-end gap-2">
            {named && (
              <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => void save("")}>
                {t("speakers.clear")}
              </Button>
            )}
            <Button type="submit" size="sm" disabled={busy || !draft.trim() || draft.trim() === current}>
              {t("speakers.save")}
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  )
}
