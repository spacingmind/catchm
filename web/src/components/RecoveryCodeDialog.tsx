import { useState } from "react"

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
export function RecoveryCodeDialog({ code, onAcked }: { code: string | null; onAcked: () => void }) {
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
          <DialogTitle>Save your recovery code</DialogTitle>
          <DialogDescription>
            This code is shown only once. Without it or a trusted device, encrypted synced meetings may not be
            recoverable — because CatchM cannot read them either. Write it down or store it in a password manager
            now.
          </DialogDescription>
        </DialogHeader>
        <p
          translate="no"
          className="select-all rounded-md bg-muted px-4 py-3 text-center font-mono text-lg tracking-wider break-all"
        >
          {code}
        </p>
        <StatusMessage tone={copyResult?.ok === false ? "error" : "success"}>
          {copyResult
            ? copyResult.ok
              ? "Copied to clipboard."
              : "Couldn’t copy. Select the code above and copy it manually."
            : null}
        </StatusMessage>
        <DialogFooter>
          <Button type="button" onClick={copyCode} variant="outline">
            Copy code
          </Button>
          <Button type="button" onClick={onAcked}>
            I’ve saved my recovery code
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
