import { useState, type FormEvent } from "react"
import { useTranslation } from "react-i18next"
import { ClipboardPaste, Loader2, X } from "lucide-react"

import { StatusMessage } from "@/components/StatusMessage"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { parseGuests, type Guest } from "@/lib/api"

const guestKey = (g: Guest) => (g.email || g.name || "").toLowerCase()

/** Adds b's guests to a, skipping ones already there (same email, or same name without one). */
function mergeGuests(a: readonly Guest[], b: readonly Guest[]): Guest[] {
  const out = [...a]
  for (const g of b) {
    const i = out.findIndex(
      (x) => (g.email && x.email === g.email) || (!g.email && !x.email && x.name?.toLowerCase() === g.name?.toLowerCase()),
    )
    if (i < 0) out.push(g)
    else if (!out[i].name && g.name) out[i] = { ...out[i], name: g.name }
  }
  return out
}

/**
 * The meeting's guest list, pasted from a calendar invite: names and
 * emails. Guests are offered first when naming speakers; only their
 * names ever reach a summary (docs/plans/active/speaker-identity.md).
 */
export function GuestsSection({
  guests,
  onChange,
}: {
  guests: readonly Guest[]
  /** Saves the full new list; rejects with a message. */
  onChange: (guests: Guest[]) => Promise<void>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [text, setText] = useState("")
  const [preview, setPreview] = useState<Guest[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [removeError, setRemoveError] = useState<string | null>(null)

  const reset = () => {
    setText("")
    setPreview(null)
    setError(null)
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      if (preview === null) {
        const parsed = await parseGuests(text)
        if (parsed.length === 0) setError(t("guests.noneFound"))
        else setPreview(parsed)
      } else {
        await onChange(mergeGuests(guests, preview))
        setOpen(false)
        reset()
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const remove = async (g: Guest) => {
    setRemoveError(null)
    try {
      await onChange(guests.filter((x) => guestKey(x) !== guestKey(g)))
    } catch (err) {
      setRemoveError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="grid gap-2">
      {guests.length > 0 ? (
        <ul className="flex flex-wrap items-center gap-2" aria-label={t("guests.listAria")}>
          {guests.map((g) => (
            <li key={guestKey(g)}>
              <Badge variant="secondary" className="h-6 max-w-full gap-1 pr-0.5" title={g.email}>
                <span className="min-w-0 truncate">{g.name || g.email}</span>
                {g.name && g.email && <span className="hidden min-w-0 truncate text-muted-foreground sm:inline">{g.email}</span>}
                <button
                  type="button"
                  onClick={() => void remove(g)}
                  aria-label={t("guests.removeAria", { name: g.name || g.email || "" })}
                  className="grid size-5 cursor-pointer place-items-center rounded-full hover:bg-foreground/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                >
                  <X className="size-3" aria-hidden />
                </button>
              </Badge>
            </li>
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted-foreground">{t("guests.empty")}</p>
      )}
      <StatusMessage tone="error">{removeError}</StatusMessage>
      <div>
        <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
          <ClipboardPaste aria-hidden />
          {t("guests.paste")}
        </Button>
      </div>

      <Dialog
        open={open}
        onOpenChange={(o) => {
          if (!o && busy) return
          setOpen(o)
          if (!o) reset()
        }}
      >
        <DialogContent>
          <form onSubmit={submit} noValidate>
            <DialogHeader>
              <DialogTitle>{t("guests.dialogTitle")}</DialogTitle>
              <DialogDescription>{t("guests.dialogDesc")}</DialogDescription>
            </DialogHeader>
            <div className="grid gap-1.5 py-4">
              {preview === null ? (
                <>
                  <Label htmlFor="guests-text">{t("guests.textLabel")}</Label>
                  <Textarea
                    id="guests-text"
                    value={text}
                    onChange={(e) => {
                      setText(e.target.value)
                      if (error) setError(null)
                    }}
                    rows={6}
                    placeholder={"An Nguyen <an@example.com>, bao@example.com"}
                    autoFocus
                    aria-describedby="guests-error"
                  />
                </>
              ) : (
                <>
                  <p className="text-sm font-medium">{t("guests.previewTitle", { count: preview.length })}</p>
                  <ul className="grid max-h-60 gap-1 overflow-y-auto text-sm">
                    {preview.map((g) => (
                      <li key={guestKey(g)} className="flex min-w-0 items-center gap-2">
                        <span className="min-w-0 truncate">{g.name || "—"}</span>
                        {g.email && <span className="min-w-0 truncate text-muted-foreground">{g.email}</span>}
                      </li>
                    ))}
                  </ul>
                  <p className="text-xs text-muted-foreground">{t("guests.previewHint")}</p>
                </>
              )}
              <StatusMessage id="guests-error" tone="error">
                {error}
              </StatusMessage>
            </div>
            <DialogFooter>
              {preview !== null && (
                <Button type="button" variant="outline" disabled={busy} onClick={() => setPreview(null)}>
                  {t("guests.back")}
                </Button>
              )}
              <Button type="submit" disabled={busy || !text.trim()} aria-busy={busy || undefined}>
                {busy && <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />}
                {preview === null ? t("guests.read") : t("guests.add", { count: preview.length })}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
