import { lazy, Suspense, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { Loader2, Route as RouteIcon, Search } from "lucide-react"

import { evidenceMatchesTranscript, type JumpToEvidence } from "@/components/insights/evidenceData"
import { GraphSkeleton, InsightEmpty, InsightLoadError } from "@/components/insights/InsightStates"
import type { FindFlowsState } from "@/components/insights/useFindFlows"
import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
import type { MeetingAnalysis } from "@/lib/api"
import { navigate } from "@/lib/route"

// React Flow and dagre load with this tab, not with the app.
const FlowsView = lazy(() => import("@/components/insights/FlowsView").then((m) => ({ default: m.FlowsView })))

/**
 * The Flows tab: the on-demand flows pass ("Find flows") and the processes
 * it found. The pass itself runs in MeetingDetail (useFindFlows), so it
 * survives a tab switch.
 */
export function FlowsTab({
  data,
  error,
  onRetry,
  ended,
  refined,
  summaryAction,
  finder,
  onJump,
}: {
  /** undefined while loading, null when the meeting has no analysis yet. */
  data: MeetingAnalysis | null | undefined
  error: string | null
  onRetry: () => void
  ended: boolean
  /** Has a summary; with no analysis, it predates analysis.json, and redoing it makes one. */
  refined: boolean
  summaryAction: ReactNode
  finder: FindFlowsState
  onJump: JumpToEvidence
}) {
  const { t } = useTranslation()
  if (error) return <InsightLoadError what={t("insights.whatFlows")} error={error} onRetry={onRetry} />
  if (data === undefined) return <GraphSkeleton label={t("insights.loadingFlows")} />
  if (data === null) {
    return (
      <InsightEmpty icon={<RouteIcon aria-hidden="true" className="size-4" />} title={t("insights.noFlowsYet")}>
        <p>
          {refined
            ? t("insights.flowsPredates")
            : ended
              ? t("insights.flowsNeedsSummary")
            : t("insights.flowsNeedsEnd")}
        </p>
        {ended && summaryAction}
      </InsightEmpty>
    )
  }

  const flows = data.analysis.flows ?? []
  const found = flows.length > 0
  const canJump = evidenceMatchesTranscript(data.analysis)
  // Only after a run in this visit: an empty list may also mean "never looked".
  const noneFound = !found && !finder.pending && finder.announcement !== "" && !finder.error
  return (
    <div className="grid gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <p className="min-w-0 flex-1 text-sm text-muted-foreground text-pretty">
          {found
            ? canJump
              ? t("insights.flowsFoundHintJump")
              : t("insights.flowsFoundHint")
            : t("insights.flowsHelp")}
        </p>
        <Button
          variant={found ? "outline" : "default"}
          onClick={finder.run}
          disabled={finder.pending}
          aria-busy={finder.pending || undefined}
        >
          {finder.pending ? (
            <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />
          ) : (
            <Search aria-hidden="true" />
          )}
          {finder.pending ? t("insights.finding") : found ? t("insights.findAgain") : t("insights.findFlows")}
        </Button>
      </div>

      <StatusMessage>{finder.pending ? t("insights.findingLong") : null}</StatusMessage>
      <div className={finder.needsSetup && finder.error ? "flex flex-wrap items-center gap-3" : undefined}>
        <StatusMessage tone="error" className={finder.needsSetup ? "min-w-0 flex-1" : undefined}>
          {finder.error}
        </StatusMessage>
        {finder.needsSetup && finder.error && (
          <Button size="sm" variant="outline" onClick={() => navigate({ name: "settings", section: "summaries" })}>
            {t("statusRow.openSettings")}
          </Button>
        )}
      </div>
      {/* The result, spoken once; the visible text below says the same. */}
      <StatusMessage className="sr-only">{finder.announcement}</StatusMessage>

      {noneFound && <p className="text-sm text-muted-foreground">{t("insights.flowsNoneFound")}</p>}
      {found && (
        <Suspense fallback={<GraphSkeleton label={t("insights.loadingFlows")} />}>
          <FlowsView analysis={data.analysis} canJump={canJump} onJump={onJump} />
        </Suspense>
      )}
    </div>
  )
}
