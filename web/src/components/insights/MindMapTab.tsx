import { lazy, Suspense, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { Network } from "lucide-react"

import { evidenceMatchesTranscript, type JumpToEvidence } from "@/components/insights/evidenceData"
import { GraphSkeleton, InsightEmpty, InsightLoadError } from "@/components/insights/InsightStates"
import type { MeetingAnalysis } from "@/lib/api"

// React Flow and d3-hierarchy load with this tab, not with the app.
const MindMapView = lazy(() => import("@/components/insights/MindMapView").then((m) => ({ default: m.MindMapView })))

/**
 * The Mind map tab: loading, "no summary yet", "no topics" (CatchM Cloud's
 * summaries have none yet), or the map itself.
 */
export function MindMapTab({
  data,
  error,
  onRetry,
  ended,
  refined,
  summaryAction,
  onJump,
}: {
  /** undefined while loading, null when the meeting has no analysis yet. */
  data: MeetingAnalysis | null | undefined
  error: string | null
  onRetry: () => void
  ended: boolean
  /** Has a summary; with no analysis, it predates analysis.json, and redoing it makes one. */
  refined: boolean
  /** The page's "Create summary" button. */
  summaryAction: ReactNode
  onJump: JumpToEvidence
}) {
  const { t } = useTranslation()
  if (error) return <InsightLoadError what={t("insights.whatMindmap")} error={error} onRetry={onRetry} />
  if (data === undefined) return <GraphSkeleton label={t("insights.loadingMindmap")} />
  if (data === null) {
    return (
      <InsightEmpty icon={<Network aria-hidden="true" className="size-4" />} title={t("insights.noMindmapYet")}>
        <p>
          {refined
            ? t("insights.mindmapPredates")
            : ended
              ? t("insights.mindmapNeedsSummary")
              : t("insights.mindmapNeedsEnd")}
        </p>
        {ended && summaryAction}
      </InsightEmpty>
    )
  }
  const { analysis, mindmap } = data
  if (!analysis.topics?.length) {
    return (
      <InsightEmpty icon={<Network aria-hidden="true" className="size-4" />} title={t("insights.noMindmapForSummary")}>
        <p>
          {analysis.generated?.provider === "catchm-cloud"
            ? t("insights.cloudNoTopics")
            : t("insights.noTopicsRedo")}
        </p>
      </InsightEmpty>
    )
  }
  return (
    <Suspense fallback={<GraphSkeleton label={t("insights.loadingMindmap")} />}>
      <MindMapView
        analysis={analysis}
        mindmap={mindmap}
        canJump={evidenceMatchesTranscript(analysis)}
        onJump={onJump}
      />
    </Suspense>
  )
}
