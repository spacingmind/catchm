import { useEffect, useState } from "react"
import { CheckSquare } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Skeleton } from "@/components/ui/skeleton"
import { getMeeting, listActionItems, updateActionItemStatus, type ActionItemRef, type Meeting } from "@/lib/api"

const done = (status: string | undefined) => status === "done"

interface ActionItemsProps {
  onOpenMeeting: (meeting: Meeting) => void
}

export function ActionItems({ onOpenMeeting }: ActionItemsProps) {
  const [items, setItems] = useState<ActionItemRef[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    listActionItems()
      .then((is) => {
        setItems(is)
        setError(null)
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  // Optimistic toggle: flip the row immediately, PATCH, revert on error.
  const toggle = (item: ActionItemRef) => {
    if (items === null) return
    const prev = items
    const status = done(item.status) ? "" : "done"
    setItems(
      prev.map((it) =>
        it.meeting_id === item.meeting_id && it.index === item.index ? { ...it, status } : it,
      ),
    )
    updateActionItemStatus(item.meeting_id, item.index, status).catch((err: unknown) => {
      setItems(prev)
      setError(err instanceof Error ? err.message : String(err))
    })
  }

  const openMeeting = (item: ActionItemRef) => {
    getMeeting(item.meeting_id)
      .then(onOpenMeeting)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
  }

  if (items === null) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-8">
        <h1 className="mb-6 flex items-center gap-2 text-2xl font-semibold">
          <Skeleton className="size-6" /> <Skeleton className="h-7 w-40" />
        </h1>
        <div className="grid gap-3" aria-busy="true">
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

  const open = items.filter((it) => !done(it.status))
  const closed = items.filter((it) => done(it.status))

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <h1 className="mb-6 flex items-center gap-2 text-2xl font-semibold">
        <CheckSquare className="size-6" /> Action items
      </h1>

      {error && <p className="mb-4 text-sm text-destructive">{error}</p>}

      {items.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border py-16 text-center">
          <p className="text-lg font-medium">Nothing to catch up on.</p>
          <p className="mt-1 text-sm text-muted-foreground">
            Action items appear when a meeting ends and the agent extracts follow-ups from the transcript.
          </p>
        </div>
      ) : (
        <div className="grid gap-3">
          {open.map((item) => (
            <Card key={`${item.meeting_id}:${item.index}`}>
              <CardContent className="flex items-start gap-3">
                <Checkbox
                  id={`item-${item.meeting_id}-${item.index}`}
                  checked={false}
                  onCheckedChange={() => toggle(item)}
                  aria-label={`Mark "${item.text}" done`}
                  className="mt-0.5"
                />
                <div className="grid min-w-0 flex-1 gap-1">
                  <label htmlFor={`item-${item.meeting_id}-${item.index}`} className="cursor-pointer break-words">
                    {item.text}
                  </label>
                  <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <button
                      type="button"
                      onClick={() => openMeeting(item)}
                      className="rounded-sm font-medium text-muted-foreground underline-offset-2 hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    >
                      {item.meeting_title}
                    </button>
                    {item.owner && <Badge variant="outline">{item.owner}</Badge>}
                    {item.due && <span>due {item.due}</span>}
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
          {closed.length > 0 && open.length > 0 && (
            <p className="pt-2 text-xs font-medium uppercase tracking-wide text-muted-foreground">
              Done
            </p>
          )}
          {closed.map((item) => (
            <Card key={`${item.meeting_id}:${item.index}`} className="opacity-60">
              <CardContent className="flex items-start gap-3">
                <Checkbox
                  id={`item-${item.meeting_id}-${item.index}`}
                  checked
                  onCheckedChange={() => toggle(item)}
                  aria-label={`Reopen "${item.text}"`}
                  className="mt-0.5"
                />
                <div className="grid min-w-0 flex-1 gap-1">
                  <label htmlFor={`item-${item.meeting_id}-${item.index}`} className="cursor-pointer break-words line-through">
                    {item.text}
                  </label>
                  <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <button
                      type="button"
                      onClick={() => openMeeting(item)}
                      className="rounded-sm font-medium text-muted-foreground underline-offset-2 hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    >
                      {item.meeting_title}
                    </button>
                    {item.owner && <Badge variant="outline">{item.owner}</Badge>}
                    {item.due && <span>due {item.due}</span>}
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

    </div>
  )
}
