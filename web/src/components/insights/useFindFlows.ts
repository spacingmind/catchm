import { useState } from "react"

import { ApiError, findFlows, RefineError, type MeetingAnalysis } from "@/lib/api"
import { formatDateTime } from "@/lib/format"

const errorText = (err: unknown) => (err instanceof Error ? err.message : String(err))
/** Ends with exactly one period, so a next step can follow. */
const sentence = (s: string) => `${s.replace(/[.\s]+$/, "")}.`

/** A flows-pass failure in plain words, by kind like the summary errors
 *  (MeetingDetail's refineErrorText), worded for this pass: the server's
 *  summary_not_ready message already says why (e.g. CatchM Cloud can't
 *  find flows yet), so it isn't prefixed with "Summaries aren't set up". */
function findFlowsErrorText(err: unknown): string {
  const raw = errorText(err)
  if (err instanceof RefineError) {
    switch (err.kind) {
      case "summary_not_ready":
        return `${sentence(raw)} You can change this in Settings → Summaries.`
      case "quota_exceeded": {
        const after = err.reset_at ? formatDateTime(err.reset_at) : "tomorrow"
        return `Today’s free AI requests are used up. Try again after ${after}, or switch to “On this computer” in Settings → Summaries.`
      }
      case "summary_failed":
        return "CatchM couldn’t look for processes this time. Try again in a moment."
      case "consent_required":
        return sentence(raw)
    }
  }
  if (err instanceof ApiError && err.status === 404) return "This meeting no longer exists. Go back to your meetings."
  if (err instanceof ApiError && err.status === 409) return sentence(raw)
  return `Couldn’t look for processes: ${sentence(raw)} Try again.`
}

export interface FindFlowsState {
  pending: boolean
  error: string | null
  /** The last failure points to Settings → Summaries. */
  needsSetup: boolean
  /** Live-region text for the last finished run ("" until one finishes). */
  announcement: string
  run: () => void
}

/**
 * The on-demand flows pass (POST /api/meetings/{id}/flows). Lives in
 * MeetingDetail rather than the Flows tab, so a run keeps going, and its
 * result lands, when the user switches tabs meanwhile.
 */
export function useFindFlows(meetingId: string, onResult: (r: MeetingAnalysis) => void): FindFlowsState {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [needsSetup, setNeedsSetup] = useState(false)
  const [announcement, setAnnouncement] = useState("")

  const run = () => {
    if (pending) return
    setPending(true)
    setError(null)
    setNeedsSetup(false)
    setAnnouncement("")
    findFlows(meetingId)
      .then((r) => {
        onResult(r)
        const n = r.analysis.flows?.length ?? 0
        setAnnouncement(
          n === 0
            ? "No step-by-step process was found in this meeting."
            : n === 1
              ? "Found 1 process."
              : `Found ${n} processes.`,
        )
      })
      .catch((err: unknown) => {
        setError(findFlowsErrorText(err))
        setNeedsSetup(err instanceof RefineError && (err.kind === "summary_not_ready" || err.kind === "quota_exceeded"))
      })
      .finally(() => setPending(false))
  }

  return { pending, error, needsSetup, announcement, run }
}
