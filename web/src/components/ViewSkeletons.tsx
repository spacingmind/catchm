import { useTranslation } from "react-i18next"

import { StatusMessage } from "@/components/StatusMessage"
import { Skeleton } from "@/components/ui/skeleton"

/*
 * Placeholders for the lazily loaded views (App.tsx's Suspense
 * fallbacks, plus the meeting fetch behind a deep link). They live in
 * the main chunk, so they mirror each view's own loading look without
 * importing the view.
 */

function CardLines() {
  return (
    <div className="rounded-xl border border-border bg-card">
      <div className="p-6 pb-2">
        <Skeleton className="h-4 w-24" />
      </div>
      <div className="grid gap-2 p-6 pt-0">
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-4/5" />
      </div>
    </div>
  )
}

/** Same shape as MeetingDetail's own loading state. */
export function MeetingDetailSkeleton() {
  const { t } = useTranslation()
  return (
    <div className="mx-auto max-w-3xl px-6 py-8" aria-busy="true">
      <StatusMessage className="sr-only">{t("viewSkeletons.loadingMeeting")}</StatusMessage>
      <Skeleton className="mb-4 h-9 w-32" />
      <Skeleton className="mb-6 h-8 w-3/5" />
      <Skeleton className="mb-2 h-4 w-2/5" />
      <div className="grid gap-4">
        {[0, 1, 2].map((i) => (
          <CardLines key={i} />
        ))}
      </div>
    </div>
  )
}

/** Same shape as ActionItems' own loading state. */
export function ActionItemsSkeleton() {
  const { t } = useTranslation()
  return (
    <div className="mx-auto max-w-3xl px-6 py-8" aria-busy="true">
      <StatusMessage className="sr-only">{t("viewSkeletons.loadingActionItems")}</StatusMessage>
      <Skeleton className="mb-6 h-8 w-48" />
      <div className="grid gap-3">
        {[0, 1, 2].map((i) => (
          <div key={i} className="rounded-xl border border-border bg-card p-6">
            <div className="flex items-start gap-3">
              <Skeleton className="mt-0.5 size-4 shrink-0" />
              <div className="grid min-w-0 flex-1 gap-1">
                <Skeleton className="h-5 w-3/4" />
                <Skeleton className="h-3.5 w-1/3" />
              </div>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

/** Settings' two-column layout: section nav on the left, a form card. */
export function SettingsSkeleton() {
  const { t } = useTranslation()
  return (
    <div className="mx-auto flex max-w-4xl gap-8 px-6 py-8" aria-busy="true">
      <StatusMessage className="sr-only">{t("viewSkeletons.loadingSettings")}</StatusMessage>
      <div className="grid w-52 shrink-0 content-start gap-1">
        <Skeleton className="mb-4 h-8 w-32" />
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </div>
      <div className="grid min-w-0 flex-1 content-start gap-4">
        <CardLines />
        <CardLines />
      </div>
    </div>
  )
}
