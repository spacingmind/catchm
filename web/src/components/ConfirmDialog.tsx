import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { Loader2 } from "lucide-react"

import { StatusMessage } from "@/components/StatusMessage"
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"

/**
 * Confirm step for destructive or hard-to-undo actions. The caller owns
 * the async work: onConfirm starts it, `pending`/`error` reflect it, and
 * the caller closes the dialog (onOpenChange(false) / open=false) on
 * success. The confirm button is a plain Button, not AlertDialogAction,
 * so clicking it doesn't auto-close — a failure stays visible inline.
 * While pending, Escape/Cancel can't dismiss it.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  cancelLabel,
  destructive = false,
  pending = false,
  error,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description: ReactNode
  confirmLabel: string
  cancelLabel?: string
  destructive?: boolean
  pending?: boolean
  error?: string | null
  onConfirm: () => void
}) {
  const { t } = useTranslation()
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next && pending) return
        onOpenChange(next)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <StatusMessage tone="error">{error}</StatusMessage>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{cancelLabel ?? t("common.cancel")}</AlertDialogCancel>
          <Button
            variant={destructive ? "destructive" : "default"}
            disabled={pending}
            aria-busy={pending || undefined}
            onClick={onConfirm}
          >
            {pending && <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />}
            {confirmLabel}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
