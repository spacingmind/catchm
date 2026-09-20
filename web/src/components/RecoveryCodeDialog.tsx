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
 * click, no open=false, no timer), the code lives only in this
 * component's render (never localStorage, never refetchable — the
 * server returns it exactly once), and unmounting discards it.
 */
export function RecoveryCodeDialog({ code, onAcked }: { code: string | null; onAcked: () => void }) {
  return (
    <Dialog
      open={code !== null}
      onOpenChange={(open) => {
        // The only way out is the explicit acknowledgment button.
        if (!open) return
      }}
    >
      <DialogContent className="sm:max-w-md" onPointerDownOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle>Save your recovery code</DialogTitle>
          <DialogDescription>
            This code is shown only once. If you ever lose your password and have no linked device, it is the only way
            to recover your meetings. Write it down or store it in a password manager now.
          </DialogDescription>
        </DialogHeader>
        <p className="select-all rounded-md bg-muted px-4 py-3 text-center font-mono text-lg tracking-wider break-all">
          {code}
        </p>
        <DialogFooter>
          <Button
            type="button"
            onClick={() => {
              void navigator.clipboard?.writeText(code ?? "")
            }}
            variant="outline"
          >
            Copy
          </Button>
          <Button type="button" onClick={onAcked}>
            I've saved my recovery code
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
