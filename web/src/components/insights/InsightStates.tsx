import type { ReactNode } from "react"

import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
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

/** A tab with nothing to draw yet: what's missing and what to do. */
export function InsightEmpty({ icon, title, children }: { icon: ReactNode; title: string; children: ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-lg">
          <h2 className="flex items-center gap-2">
            {icon}
            {title}
          </h2>
        </CardTitle>
      </CardHeader>
      <CardContent className="grid justify-items-start gap-3 text-sm text-muted-foreground text-pretty">
        {children}
      </CardContent>
    </Card>
  )
}

export function InsightLoadError({ what, error, onRetry }: { what: string; error: string; onRetry: () => void }) {
  return (
    <div className="grid justify-items-start gap-3">
      <StatusMessage tone="error">
        Couldn’t load {what} ({error}). Try again.
      </StatusMessage>
      <Button variant="outline" onClick={onRetry}>
        Try again
      </Button>
    </div>
  )
}
