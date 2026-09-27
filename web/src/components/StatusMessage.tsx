import type { ReactNode } from "react"
import { cn } from "cn"

export type StatusTone = "muted" | "error" | "success" | "warning"

const TONE_CLASS: Record<StatusTone, string> = {
  muted: "text-muted-foreground",
  error: "text-destructive",
  success: "text-muted-foreground",
  warning: "text-warning",
}

/**
 * A polite live region for status/error text. Keep it mounted and change
 * only its children: screen readers announce updates to a region that
 * already exists, but often miss one that mounts with its text. When
 * empty it becomes sr-only (out of flow, so no height, margin or grid
 * gap) rather than display:none, which would drop it from the
 * accessibility tree and defeat the announcement.
 */
export function StatusMessage({
  tone = "muted",
  id,
  className,
  children,
}: {
  tone?: StatusTone
  id?: string
  className?: string
  children?: ReactNode
}) {
  const empty = isEmpty(children)
  return (
    <p
      id={id}
      role="status"
      aria-live="polite"
      aria-atomic="true"
      className={empty ? "sr-only" : cn("text-sm", TONE_CLASS[tone], className)}
    >
      {empty ? null : children}
    </p>
  )
}

/** True when children would render nothing (null, false, "", or an
 *  array of those — e.g. several `{cond && "…"}` siblings all false). */
function isEmpty(children: ReactNode): boolean {
  if (Array.isArray(children)) return children.every(isEmpty)
  return children == null || children === false || children === true || children === ""
}
