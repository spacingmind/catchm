import { lazy, Suspense, type ReactNode } from "react"
import { Network } from "lucide-react"

import { evidenceMatchesTranscript, type JumpToEvidence } from "@/components/insights/evidence"
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
  if (error) return <InsightLoadError what="the mind map" error={error} onRetry={onRetry} />
  if (data === undefined) return <GraphSkeleton label="Loading mind map…" />
  if (data === null) {
    return (
      <InsightEmpty icon={<Network aria-hidden="true" className="size-4" />} title="No mind map yet">
        <p>
          {refined
            ? "This summary was made before CatchM could draw mind maps. Redo the summary to see one."
            : ended
              ? "Create a summary first to see the mind map."
              : "End the meeting, then create a summary to see the mind map."}
        </p>
        {ended && summaryAction}
      </InsightEmpty>
    )
  }
  const { analysis, mindmap } = data
  if (!analysis.topics?.length) {
    return (
      <InsightEmpty icon={<Network aria-hidden="true" className="size-4" />} title="No mind map for this summary">
        <p>
          {analysis.generated?.provider === "catchm-cloud"
            ? "CatchM Cloud doesn’t sort a meeting into topics yet, so it can’t draw a mind map. To get one, switch to “On this computer” in Settings → Summaries, then redo the summary."
            : "This summary has no topics, so there’s nothing to draw. Redo the summary to try again."}
        </p>
      </InsightEmpty>
    )
  }
  return (
    <Suspense fallback={<GraphSkeleton label="Loading mind map…" />}>
      <MindMapView
        analysis={analysis}
        mindmap={mindmap}
        canJump={evidenceMatchesTranscript(analysis)}
        onJump={onJump}
      />
    </Suspense>
  )
}
