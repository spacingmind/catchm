import { useState } from "react"
import { useTranslation } from "react-i18next"

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

/**
 * The one-time recovery-code dialog, shared by signup (M13d) and the
 * recover flow (M15). Three invariants, enforced here and nowhere else:
 * dismissal ONLY via the explicit acknowledgment button (no outside
 * click, no Escape, no close X, no open=false, no timer), the code lives
 * only in this component's render (never localStorage, never
 * refetchable — the server returns it exactly once), and unmounting
 * discards it.
 */
export function RecoveryCodeDialog({
  code,
  onAcked,
  notice,
}: {
  code: string | null
  onAcked: () => void
  /** An extra line under the description, e.g. that recovering turned 2FA off. */
  notice?: string | null
}) {
  const { t } = useTranslation()
  // Keyed on the code it was for, so a later code never inherits a stale
  // "Copied" from the previous one (no effect needed to reset it).
  const [copy, setCopy] = useState<{ code: string; ok: boolean } | null>(null)
  const copyResult = copy && copy.code === code ? copy : null

  const copyCode = () => {
    if (!code) return
    const write = navigator.clipboard?.writeText(code)
    if (!write) {
      setCopy({ code, ok: false })
      return
    }
    write.then(
      () => setCopy({ code, ok: true }),
      () => setCopy({ code, ok: false }),
    )
  }

  return (
    <Dialog
      open={code !== null}
      onOpenChange={(open) => {
        // The only way out is the explicit acknowledgment button.
        if (!open) return
      }}
    >
      <DialogContent
        className="sm:max-w-md"
        // No close X: it would be a dead control (closing is ignored
        // above) and would otherwise take initial focus.
        showCloseButton={false}
        onPointerDownOutside={(e) => e.preventDefault()}
        onEscapeKeyDown={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle>{t("recoveryCode.title")}</DialogTitle>
          <DialogDescription>{t("recoveryCode.description")}</DialogDescription>
        </DialogHeader>
        {notice && <p className="text-sm text-warning">{notice}</p>}
        <p
          translate="no"
          className="select-all rounded-md bg-muted px-4 py-3 text-center font-mono text-lg tracking-wider break-all"
        >
          {code}
        </p>
        <StatusMessage tone={copyResult?.ok === false ? "error" : "success"}>
          {copyResult ? (copyResult.ok ? t("recoveryCode.copied") : t("recoveryCode.copyFailed")) : null}
        </StatusMessage>
        <DialogFooter>
          <Button type="button" onClick={copyCode} variant="outline">
            {t("recoveryCode.copyCode")}
          </Button>
          <Button type="button" onClick={onAcked}>
            {t("recoveryCode.acked")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
