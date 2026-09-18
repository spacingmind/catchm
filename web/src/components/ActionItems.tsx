import { useEffect, useState } from "react"
import { CheckSquare } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Card, CardContent } from "@/components/ui/card"
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
    return <div className="mx-auto max-w-3xl px-6 py-8 text-sm text-muted-foreground">Loading…</div>
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
        <p className="py-8 text-center text-sm text-muted-foreground">
          No action items recorded yet.
        </p>
      ) : (
        <div className="grid gap-3">
          {open.map((item) => (
            <Card key={`${item.meeting_id}:${item.index}`}>
              <CardContent className="flex items-start gap-3">
                <input
                  type="checkbox"
                  checked={false}
                  onChange={() => toggle(item)}
                  className="mt-0.5 size-4 shrink-0 accent-primary"
                  aria-label={`Mark "${item.text}" done`}
                />
                <div className="grid flex-1 gap-1">
                  <span>{item.text}</span>
                  <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <button
                      type="button"
                      onClick={() => openMeeting(item)}
                      className="font-medium text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
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
                <input
                  type="checkbox"
                  checked
                  onChange={() => toggle(item)}
                  className="mt-0.5 size-4 shrink-0 accent-primary"
                  aria-label={`Reopen "${item.text}"`}
                />
                <div className="grid flex-1 gap-1">
                  <span className="line-through">{item.text}</span>
                  <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <button
                      type="button"
                      onClick={() => openMeeting(item)}
                      className="font-medium text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
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
