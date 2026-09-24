import { useEffect, useMemo, useState, type ReactNode } from "react"
import { CheckSquare } from "lucide-react"
import { cn } from "cn"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Skeleton } from "@/components/ui/skeleton"
import { getMeeting, listActionItems, updateActionItemStatus, type ActionItemRef, type Meeting } from "@/lib/api"

const done = (status: string | undefined) => status === "done"

/** An item is overdue when it has a parseable due date in the past and
 *  isn't done. Unparseable dates never mark overdue. */
function isOverdue(item: ActionItemRef): boolean {
  if (done(item.status) || !item.due) return false
  const t = Date.parse(item.due)
  return !Number.isNaN(t) && t < Date.now()
}

type FilterState = { owner: string | null; meeting: string | null }

interface ActionItemsProps {
  onOpenMeeting: (meeting: Meeting) => void
}

export function ActionItems({ onOpenMeeting }: ActionItemsProps) {
  const [items, setItems] = useState<ActionItemRef[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [view, setView] = useState<"grouped" | "by-due">("grouped")
  const [filter, setFilter] = useState<FilterState>({ owner: null, meeting: null })

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

  const owners = useMemo(() => {
    const s = new Set<string>()
    for (const it of items) if (it.owner) s.add(it.owner)
    return [...s].sort()
  }, [items])

  const meetings = useMemo(() => {
    const s = new Set<string>()
    for (const it of items) s.add(it.meeting_title)
    return [...s].sort()
  }, [items])

  const { byMeeting, byDue, closed, openCount } = useMemo(() => {
    const visible = items.filter(
      (it) =>
        (!filter.owner || it.owner === filter.owner) &&
        (!filter.meeting || it.meeting_title === filter.meeting),
    )
    const open = visible.filter((it) => !done(it.status))
    const closed = visible.filter((it) => done(it.status))
    const map = new Map<string, ActionItemRef[]>()
    for (const it of open) {
      const list = map.get(it.meeting_title) ?? []
      list.push(it)
      map.set(it.meeting_title, list)
    }
    const byDue = [...open].sort((a, b) => {
      const ta = a.due ? Date.parse(a.due) : Number.POSITIVE_INFINITY
      const tb = b.due ? Date.parse(b.due) : Number.POSITIVE_INFINITY
      return ta - tb
    })
    return { byMeeting: [...map.entries()], byDue, closed, openCount: open.length }
  }, [items, filter])

  const renderItem = (item: ActionItemRef) => (
    <Card key={`${item.meeting_id}:${item.index}`}>
      <CardContent className="flex items-start gap-3">
        <Checkbox
          id={`item-${item.meeting_id}-${item.index}`}
          checked={done(item.status)}
          onCheckedChange={() => toggle(item)}
          aria-label={done(item.status) ? `Reopen "${item.text}"` : `Mark "${item.text}" done`}
          className="mt-0.5"
        />
        <div className="grid min-w-0 flex-1 gap-1">
          <label
            htmlFor={`item-${item.meeting_id}-${item.index}`}
            className={cn("cursor-pointer break-words", done(item.status) && "line-through")}
          >
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
            {item.due && (
              <span className={isOverdue(item) ? "text-destructive" : undefined}>due {item.due}</span>
            )}
            {isOverdue(item) && <Badge variant="destructive">Overdue</Badge>}
          </div>
        </div>
      </CardContent>
    </Card>
  )

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
          <div className="flex flex-wrap items-center justify-between gap-3">
            <SegmentedControlish
              value={view}
              onChange={setView}
            />
            <div className="flex flex-wrap items-center gap-2">
              {owners.map((o) => (
                <Chip key={o} active={filter.owner === o} onClick={() => setFilter((f) => ({ ...f, owner: f.owner === o ? null : o }))}>
                  {o}
                </Chip>
              ))}
              {meetings.map((m) => (
                <Chip key={m} active={filter.meeting === m} onClick={() => setFilter((f) => ({ ...f, meeting: f.meeting === m ? null : m }))}>
                  {m}
                </Chip>
              ))}
            </div>
          </div>

          {view === "grouped" ? (
            byMeeting.map(([title, list]) => (
              <div key={title} className="grid gap-2">
                <p className="text-sm font-medium text-muted-foreground">{title}</p>
                {list.map(renderItem)}
              </div>
            ))
          ) : (
            <div className="grid gap-2">{byDue.map(renderItem)}</div>
          )}

          {closed.length > 0 && openCount === 0 && (
            <p className="pt-2 text-sm font-medium text-muted-foreground">All caught up.</p>
          )}
          {closed.length > 0 && (
            <div className="grid gap-2">
              <p className="pt-2 text-sm font-medium text-muted-foreground">Done</p>
              {closed.map(renderItem)}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function Chip({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "cursor-pointer rounded-full border border-border px-2.5 py-0.5 text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        active ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted/60",
      )}
    >
      {children}
    </button>
  )
}

function SegmentedControlish({
  value,
  onChange,
}: {
  value: "grouped" | "by-due"
  onChange: (v: "grouped" | "by-due") => void
}) {
  return (
    <div className="inline-flex rounded-lg border border-border bg-muted/40 p-0.5" role="group" aria-label="Grouping">
      <Button
        size="sm"
        variant={value === "grouped" ? "secondary" : "ghost"}
        onClick={() => onChange("grouped")}
        aria-pressed={value === "grouped"}
      >
        By meeting
      </Button>
      <Button
        size="sm"
        variant={value === "by-due" ? "secondary" : "ghost"}
        onClick={() => onChange("by-due")}
        aria-pressed={value === "by-due"}
      >
        By due date
      </Button>
    </div>
  )
}
