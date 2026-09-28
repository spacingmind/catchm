import { lazy, Suspense, type ReactNode } from "react"
import { Loader2, Route as RouteIcon, Search } from "lucide-react"

import { evidenceMatchesTranscript, type JumpToEvidence } from "@/components/insights/evidence"
import { GraphSkeleton, InsightEmpty, InsightLoadError } from "@/components/insights/InsightStates"
import type { FindFlowsState } from "@/components/insights/useFindFlows"
import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
import type { MeetingAnalysis } from "@/lib/api"
import { navigate } from "@/lib/route"

// React Flow and dagre load with this tab, not with the app.
const FlowsView = lazy(() => import("@/components/insights/FlowsView").then((m) => ({ default: m.FlowsView })))

const FLOWS_HELP =
  "CatchM can look for step-by-step processes described in this meeting, like how a request gets approved. Most meetings don’t have one."

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
  if (error) return <InsightLoadError what="the processes" error={error} onRetry={onRetry} />
  if (data === undefined) return <GraphSkeleton label="Loading processes…" />
  if (data === null) {
    return (
      <InsightEmpty icon={<RouteIcon aria-hidden="true" className="size-4" />} title="No processes yet">
        <p>
          {refined
            ? "This summary was made before CatchM could look for processes. Redo the summary first."
            : ended
              ? "Create a summary first. Then CatchM can look for step-by-step processes in this meeting."
            : "End the meeting and create a summary first. Then CatchM can look for step-by-step processes in this meeting."}
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
              ? "Processes found in this meeting. Click a step to see where it was said."
              : "Processes found in this meeting."
            : FLOWS_HELP}
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
          {finder.pending ? "Looking for processes…" : found ? "Find again" : "Find flows"}
        </Button>
      </div>

      <StatusMessage>{finder.pending ? "Looking for processes… this can take a minute." : null}</StatusMessage>
      <div className={finder.needsSetup && finder.error ? "flex flex-wrap items-center gap-3" : undefined}>
        <StatusMessage tone="error" className={finder.needsSetup ? "min-w-0 flex-1" : undefined}>
          {finder.error}
        </StatusMessage>
        {finder.needsSetup && finder.error && (
          <Button size="sm" variant="outline" onClick={() => navigate({ name: "settings", section: "summaries" })}>
            Open settings
          </Button>
        )}
      </div>
      {/* The result, spoken once; the visible text below says the same. */}
      <StatusMessage className="sr-only">{finder.announcement}</StatusMessage>

      {noneFound && <p className="text-sm text-muted-foreground">No step-by-step process was found in this meeting.</p>}
      {found && (
        <Suspense fallback={<GraphSkeleton label="Loading processes…" />}>
          <FlowsView analysis={data.analysis} canJump={canJump} onJump={onJump} />
        </Suspense>
      )}
    </div>
  )
}
