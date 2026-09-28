import type { Analysis, AnalysisAction, AnalysisDecision, AnalysisTopic } from "@/lib/api"
import { formatDay } from "@/lib/format"

/**
 * Evidence (docs/plans/active/meeting-insights.md, decision 2): topics,
 * decisions, actions and flow steps carry utterance seqs. Jumping to them
 * opens the Transcript tab on the first seq, with next/previous for the
 * rest.
 */
export type JumpToEvidence = (seqs: number[]) => void

/** Whether an analysis's evidence seqs can be shown: MeetingDetail's
 *  Transcript tab loads the transcript the analysis names
 *  (generated.transcript, "live" or "v2" — GET .../transcript?source=),
 *  so the seqs always point at the lines on screen. An analysis that
 *  doesn't say which transcript it used can't be trusted to. */
export function evidenceMatchesTranscript(a: Analysis): boolean {
  const t = a.generated?.transcript
  return t === "live" || t === "v2"
}

export type RefItem =
  | { kind: "topic"; item: AnalysisTopic }
  | { kind: "decision"; item: AnalysisDecision }
  | { kind: "action"; item: AnalysisAction }

/** Resolves a MindNode ref ("topic:t1", "decision:d1", "action:a1"). */
export function resolveRef(a: Analysis, ref: string | undefined): RefItem | null {
  if (!ref) return null
  const colon = ref.indexOf(":")
  const kind = ref.slice(0, colon)
  const id = ref.slice(colon + 1)
  switch (kind) {
    case "topic": {
      const item = a.topics?.find((t) => t.id === id)
      return item ? { kind, item } : null
    }
    case "decision": {
      const item = a.decisions?.find((d) => d.id === id)
      return item ? { kind, item } : null
    }
    case "action": {
      const item = a.actions?.find((x) => x.id === id)
      return item ? { kind, item } : null
    }
    default:
      return null
  }
}

/** An action's owner or a flow's actor for display: a person key
 *  ("person:long") becomes that person's name; plain text stays as it is. */
export function ownerName(a: Analysis, owner: string | undefined): string | undefined {
  if (!owner) return undefined
  const person = a.people?.find((p) => p.key === owner)
  if (person) return person.name
  return owner.startsWith("person:") ? owner.slice("person:".length) : owner
}

/** "due Fri, Oct 3", or the deadline as it was said ("thứ Sáu tuần sau"). */
export function dueLabel(action: AnalysisAction): string | undefined {
  if (action.due) return `due ${formatDay(action.due)}`
  if (action.due_text) return `due ${action.due_text}`
  return undefined
}

export function prefersReducedMotion(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches
}

/** Small screens get the graphs' list view open by default: a graph is hard to read there. */
export function narrowScreen(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(max-width: 639px)").matches
}
