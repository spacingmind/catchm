import { useTranslation } from "react-i18next"
import type { ReactNode } from "react"

import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"

/** Stands in for a graph while its data or its lazy chunk loads. Lives
 *  in MeetingDetail's chunk, so it can't import the graph views. */
export function GraphSkeleton({ label }: { label: string }) {
  return (
    <div className="grid gap-3" aria-busy="true">
      <StatusMessage className="sr-only">{label}</StatusMessage>
      <Skeleton className="h-4 w-2/3" />
      <Skeleton className="h-[min(65vh,560px)] min-h-72 w-full rounded-xl" />
    </div>
  )
}

/** A tab with nothing to draw yet: what's missing and what to do. Same
 *  dashed empty state as the Meetings and Action items pages. */
export function InsightEmpty({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) {
  return (
    <div className="rounded-xl border border-dashed border-border px-6 py-16 text-center">
      <h2 className="flex items-center justify-center gap-2 text-lg font-medium">
        {icon}
        {title}
      </h2>
      <div className="mt-1 grid justify-items-center gap-6 text-sm text-pretty text-muted-foreground">{children}</div>
    </div>
  )
}

export function InsightLoadError({ what, error, onRetry }: { what: string; error: string; onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="grid justify-items-start gap-3">
      <StatusMessage tone="error">
        {t("insights.loadError", { what, error })}
      </StatusMessage>
      <Button size="sm" variant="outline" onClick={onRetry}>
        {t("common.tryAgain")}
      </Button>
    </div>
  )
}
