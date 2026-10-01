import { useEffect, useId, useMemo, useState } from "react"
import { CheckSquare } from "lucide-react"
import i18next from "i18next"
import { useTranslation } from "react-i18next"
import { cn } from "cn"

import { FilterChip } from "@/components/FilterChip"
import { ProjectBadge, ProjectDot } from "@/components/Projects"
import { StatusMessage } from "@/components/StatusMessage"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Skeleton } from "@/components/ui/skeleton"
import { SegmentedControl } from "@/components/settings/pieces"
import { getMeeting, listActionItems, projectKey, updateActionItemStatus, type ActionItemRef, type Meeting } from "@/lib/api"
import { formatDay, parseLocalDate } from "@/lib/format"
import { findProject, useProjects } from "@/lib/projects"

const done = (status: string | undefined) => status === "done"

const itemKey = (item: ActionItemRef) => `${item.meeting_id}:${item.index}`

const errorText = (err: unknown) => (err instanceof Error ? err.message : String(err))

/** Local midnight today: a due day is overdue only once it has passed. */
function startOfToday(): number {
  const d = new Date()
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** Due date as ms, or +Infinity when missing/unparseable (free-text dues
 *  like "next week" sort last and never mark overdue). "YYYY-MM-DD" is
 *  read as local midnight; anything else the LLM wrote (ISO timestamps,
 *  "Sep 1, 2026") falls back to Date.parse. */
function dueTime(item: ActionItemRef): number {
  const local = parseLocalDate(item.due)
  if (local) return local.getTime()
  const t = item.due ? Date.parse(item.due) : NaN
  return Number.isNaN(t) ? Number.POSITIVE_INFINITY : t
}

/** An item is overdue when its due day is before today and it isn't
 *  done. Date-only dues are parsed as local dates: Date.parse would read
 *  them as UTC midnight and flag them overdue hours early east of UTC. */
function isOverdue(item: ActionItemRef): boolean {
  if (done(item.status)) return false
  const t = dueTime(item)
  return Number.isFinite(t) && t < startOfToday()
}

/** project is a matching key (projectKey), not a display name. */
type FilterState = { owner: string | null; meeting: string | null; project: string | null }

const NO_FILTER: FilterState = { owner: null, meeting: null, project: null }

interface ActionItemsProps {
  onOpenMeeting: (meeting: Meeting) => void
}

export function ActionItems({ onOpenMeeting }: ActionItemsProps) {
  const { t } = useTranslation()
  const [items, setItems] = useState<ActionItemRef[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loadAttempt, setLoadAttempt] = useState(0)
  const [view, setView] = useState<"grouped" | "by-due">("grouped")
  const [filter, setFilter] = useState<FilterState>(NO_FILTER)
  // Per-row toggle failures, keyed by itemKey, shown next to that row.
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({})
  const ownerLabelId = useId()
  const meetingLabelId = useId()
  const projectLabelId = useId()
  // Show each project with its most-used spelling, as elsewhere.
  const knownProjects = useProjects()
  const projectName = (name: string) => findProject(knownProjects, name)?.name ?? name

  useEffect(() => {
    listActionItems()
      .then((is) => {
        setItems(is)
        setError(null)
      })
      .catch((err: unknown) =>
        setError(i18next.t("actionItems.loadError", { error: errorText(err) })),
      )
  }, [loadAttempt])

  const retryLoad = () => {
    setError(null)
    setLoadAttempt((n) => n + 1)
  }

  const setRowError = (key: string, message: string | null) =>
    setRowErrors((errs) => {
      if (message === null) {
        if (!(key in errs)) return errs
        const next = { ...errs }
        delete next[key]
        return next
      }
      return { ...errs, [key]: message }
    })

  // Optimistic toggle: flip the row immediately, PATCH, revert on error
  // (reported next to the row, not at the top of the page).
  const toggle = (item: ActionItemRef) => {
    if (items === null) return
    const key = itemKey(item)
    const status = done(item.status) ? "" : "done"
    const flip = (to: string) =>
      setItems((prev) =>
        prev?.map((it) => (it.meeting_id === item.meeting_id && it.index === item.index ? { ...it, status: to } : it)) ??
        prev,
      )
    setRowError(key, null)
    flip(status)
    updateActionItemStatus(item.meeting_id, item.index, status).catch((err: unknown) => {
      flip(item.status ?? "")
      setRowError(
        key,
        status === "done"
          ? t("actionItems.toggleDoneError", { error: errorText(err) })
          : t("actionItems.toggleReopenError", { error: errorText(err) }),
      )
    })
  }

  const openMeeting = (item: ActionItemRef) => {
    getMeeting(item.meeting_id)
      .then(onOpenMeeting)
      .catch((err: unknown) =>
        setError(t("actionItems.openError", { title: item.meeting_title, error: errorText(err) })),
      )
  }

  const owners = useMemo(() => {
    const s = new Set<string>()
    for (const it of items ?? []) if (it.owner) s.add(it.owner)
    return [...s].sort()
  }, [items])

  const meetings = useMemo(() => {
    const s = new Set<string>()
    for (const it of items ?? []) s.add(it.meeting_title)
    return [...s].sort()
  }, [items])

  // One entry per project (case-insensitive), first spelling seen.
  const projects = useMemo(() => {
    const m = new Map<string, string>()
    for (const it of items ?? []) {
      if (it.meeting_project && !m.has(projectKey(it.meeting_project))) {
        m.set(projectKey(it.meeting_project), it.meeting_project)
      }
    }
    return [...m.entries()].sort((a, b) => a[1].localeCompare(b[1]))
  }, [items])

  const { byMeeting, byDue, closed, openCount, visibleCount } = useMemo(() => {
    const visible = (items ?? []).filter(
      (it) =>
        (!filter.owner || it.owner === filter.owner) &&
        (!filter.meeting || it.meeting_title === filter.meeting) &&
        (!filter.project || projectKey(it.meeting_project ?? "") === filter.project),
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
      const ta = dueTime(a)
      const tb = dueTime(b)
      // Infinity - Infinity is NaN; keep undated items in their order.
      return ta === tb ? 0 : ta - tb
    })
    return { byMeeting: [...map.entries()], byDue, closed, openCount: open.length, visibleCount: visible.length }
  }, [items, filter])

  const pageError = (
    <div className={error ? "mb-4 flex flex-wrap items-center gap-3" : undefined}>
      <StatusMessage tone="error" className="min-w-0 flex-1 break-words">
        {error}
      </StatusMessage>
      {error && items === null && (
        <Button size="sm" variant="outline" onClick={retryLoad}>
          {t("common.tryAgain")}
        </Button>
      )}
    </div>
  )

  const heading = (
    <h1 className="mb-6 flex items-center gap-2 text-2xl font-semibold text-balance">
      <CheckSquare className="size-6" /> {t("sidebar.actionItems")}
    </h1>
  )

  if (items === null) {
    return (
      <div className="mx-auto max-w-3xl px-6 py-8">
        {heading}
        {pageError}
        {!error && (
          <div className="grid gap-3" aria-busy="true">
            <p className="sr-only" role="status">
              {t("viewSkeletons.loadingActionItems")}
            </p>
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
        )}
      </div>
    )
  }

  const renderItem = (item: ActionItemRef) => {
    const key = itemKey(item)
    const id = `item-${item.meeting_id}-${item.index}`
    const overdue = isOverdue(item)
    return (
      <Card key={key}>
        <CardContent className="grid gap-1">
          {/* One hit target: the label wraps the checkbox and its text, so
              the checkbox is named by the item text (no aria-label). */}
          <label htmlFor={id} className="flex cursor-pointer items-start gap-3">
            <Checkbox id={id} checked={done(item.status)} onCheckedChange={() => toggle(item)} className="mt-0.5" />
            <span className={cn("min-w-0 flex-1 break-words", done(item.status) && "line-through")}>{item.text}</span>
          </label>
          <div className="flex flex-wrap items-center gap-2 pl-7 text-xs text-muted-foreground">
            <button
              type="button"
              onClick={() => openMeeting(item)}
              className="min-w-0 cursor-pointer rounded-sm text-left font-medium break-words text-muted-foreground underline-offset-2 hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              {item.meeting_title}
            </button>
            {item.meeting_project && <ProjectBadge name={projectName(item.meeting_project)} />}
            {item.owner && <Badge variant="outline">{item.owner}</Badge>}
            {item.due && (
              <span className={overdue ? "text-destructive" : undefined}>
                {t("meetingDetail.due", { date: formatDay(item.due) })}
              </span>
            )}
            {overdue && <Badge variant="destructive">{t("actionItems.overdue")}</Badge>}
          </div>
          <StatusMessage tone="error" className="pl-7 text-xs break-words">
            {rowErrors[key]}
          </StatusMessage>
        </CardContent>
      </Card>
    )
  }

  const filtersActive = filter.owner !== null || filter.meeting !== null || filter.project !== null
  const clearFilters = () => setFilter(NO_FILTER)

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      {heading}

      {pageError}

      {items.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border px-6 py-16 text-center">
          <p className="text-lg font-medium">{t("meetingDetail.noActionsYet")}</p>
          <p className="mt-1 text-sm text-pretty text-muted-foreground">{t("actionItems.emptyBody")}</p>
        </div>
      ) : (
        <div className="grid gap-3">
          <div className="grid gap-2">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <SegmentedControl
                label={t("actionItems.grouping")}
                value={view}
                onChange={setView}
                options={[
                  { value: "grouped", label: t("actionItems.byMeeting") },
                  { value: "by-due", label: t("actionItems.byDueDate") },
                ]}
              />
              {filtersActive && (
                <Button size="sm" variant="ghost" onClick={clearFilters}>
                  {t("meetingList.clearFilters")}
                </Button>
              )}
            </div>
            {projects.length > 0 && (
              <div role="group" aria-labelledby={projectLabelId} className="flex flex-wrap items-center gap-2">
                <span id={projectLabelId} className="text-xs font-medium text-muted-foreground">
                  {t("meetingList.projectLabel")}
                </span>
                {projects.map(([key, spelling]) => {
                  const name = projectName(spelling)
                  return (
                    <FilterChip
                      key={key}
                      pressed={filter.project === key}
                      onPressedChange={(on) => setFilter((f) => ({ ...f, project: on ? key : null }))}
                      className="max-w-64 sm:max-w-80"
                      title={name}
                    >
                      <ProjectDot
                        name={name}
                        className={filter.project === key ? "ring-1 ring-primary-foreground/60" : undefined}
                      />
                      <span className="min-w-0 truncate">{name}</span>
                    </FilterChip>
                  )
                })}
              </div>
            )}
            {owners.length > 0 && (
              <div role="group" aria-labelledby={ownerLabelId} className="flex flex-wrap items-center gap-2">
                <span id={ownerLabelId} className="text-xs font-medium text-muted-foreground">
                  {t("actionItems.owner")}
                </span>
                {owners.map((o) => (
                  <FilterChip
                    key={o}
                    pressed={filter.owner === o}
                    onPressedChange={(on) => setFilter((f) => ({ ...f, owner: on ? o : null }))}
                  >
                    {o}
                  </FilterChip>
                ))}
              </div>
            )}
            {meetings.length > 0 && (
              <div role="group" aria-labelledby={meetingLabelId} className="flex flex-wrap items-center gap-2">
                <span id={meetingLabelId} className="text-xs font-medium text-muted-foreground">
                  {t("actionItems.meeting")}
                </span>
                {meetings.map((m) => (
                  <FilterChip
                    key={m}
                    pressed={filter.meeting === m}
                    onPressedChange={(on) => setFilter((f) => ({ ...f, meeting: on ? m : null }))}
                    // Long titles clip to one line. The span needs min-w-0
                    // to shrink inside the inline-flex chip; the full title
                    // stays in the DOM (accessible name) and the tooltip.
                    className="max-w-64 sm:max-w-80"
                    title={m}
                  >
                    <span className="min-w-0 truncate">{m}</span>
                  </FilterChip>
                ))}
              </div>
            )}
          </div>

          {visibleCount === 0 ? (
            <div className="grid justify-items-center gap-3 rounded-xl border border-dashed border-border px-6 py-10 text-center">
              <p className="text-sm text-muted-foreground">{t("actionItems.noMatches")}</p>
              <Button size="sm" variant="outline" onClick={clearFilters}>
                {t("meetingList.clearFilters")}
              </Button>
            </div>
          ) : view === "grouped" ? (
            byMeeting.map(([title, list]) => (
              <section key={title} className="grid gap-2">
                <h2 className="text-sm font-medium break-words text-muted-foreground">{title}</h2>
                {list.map(renderItem)}
              </section>
            ))
          ) : (
            <div className="grid gap-2">{byDue.map(renderItem)}</div>
          )}

          {closed.length > 0 && openCount === 0 && (
            <p className="pt-2 text-sm font-medium text-muted-foreground">{t("actionItems.allCaughtUp")}</p>
          )}
          {closed.length > 0 && (
            <section className="grid gap-2">
              <h2 className="pt-2 text-sm font-medium text-muted-foreground">{t("actionItems.done")}</h2>
              {closed.map(renderItem)}
            </section>
          )}
        </div>
      )}
    </div>
  )
}
