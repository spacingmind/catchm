import { ChevronLeft, ChevronRight, Quote, X } from "lucide-react"

import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"

/** A small "Source" link next to a decision or action: opens the
 *  transcript at the line it came from. */
export function SourceLink({ onClick }: { onClick: () => void }) {
  return (
    <Button
      variant="link"
      size="xs"
      className="-my-1 px-1 text-muted-foreground hover:text-foreground"
      aria-label="Source: show where this was said in the transcript"
      onClick={onClick}
    >
      <Quote aria-hidden="true" />
      Source
    </Button>
  )
}

const TAB_NAMES: Record<string, string> = {
  overview: "Overview",
  mindmap: "Mind map",
  flows: "Flows",
}

/**
 * Shown above the transcript after a jump: which source line this is
 * ("1 of 3", with previous/next when there are several), a way back to
 * where the jump started, and a note when the line can't be found.
 */
export function EvidenceBar({
  index,
  total,
  missing,
  from,
  onStep,
  onBack,
  onClose,
}: {
  index: number
  total: number
  missing: boolean
  from: string
  onStep: (delta: number) => void
  onBack: () => void
  onClose: () => void
}) {
  const back = TAB_NAMES[from]
  return (
    <div className="sticky top-0 z-10 mb-4 grid gap-2 rounded-xl border border-border bg-card/95 px-4 py-3 backdrop-blur">
      <div className="flex flex-wrap items-center gap-2">
        <p className="text-sm font-medium">{total > 1 ? `Source ${index + 1} of ${total}` : "Source"}</p>
        {total > 1 && (
          <div className="flex items-center gap-1">
            <Button
              variant="outline"
              size="icon-sm"
              aria-label="Previous source"
              disabled={index === 0}
              onClick={() => onStep(-1)}
            >
              <ChevronLeft aria-hidden="true" />
            </Button>
            <Button
              variant="outline"
              size="icon-sm"
              aria-label="Next source"
              disabled={index === total - 1}
              onClick={() => onStep(1)}
            >
              <ChevronRight aria-hidden="true" />
            </Button>
          </div>
        )}
        <div className="ml-auto flex items-center gap-1">
          {back && (
            <Button variant="ghost" size="sm" onClick={onBack}>
              <span aria-hidden="true">←</span> Back to {back}
            </Button>
          )}
          <Button variant="ghost" size="icon-sm" aria-label="Close source" onClick={onClose}>
            <X aria-hidden="true" />
          </Button>
        </div>
      </div>
      <StatusMessage tone="warning">
        {missing ? "This line isn’t in the transcript anymore. It may have changed after the summary was made." : null}
      </StatusMessage>
    </div>
  )
}
