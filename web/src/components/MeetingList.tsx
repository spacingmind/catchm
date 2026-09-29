import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react"
import i18next from "i18next"
import { useTranslation } from "react-i18next"
import { Folder, FolderOpen, ListVideo, Mic, Pencil, Search, X } from "lucide-react"

import logo from "@/assets/logo.png"
import { FilterChip } from "@/components/FilterChip"
import { GetStarted } from "@/components/GetStarted"
import { ProjectBadge, ProjectDot, RenameProjectDialog } from "@/components/Projects"
import { SegmentedControl } from "@/components/settings/pieces"
import { StatusMessage } from "@/components/StatusMessage"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { listMeetings, plainTags, projectKey, projectOf, type Meeting } from "@/lib/api"
import { formatDateRange, formatDateTime, formatDay, formatMonth } from "@/lib/format"
import { noAutofill } from "@/lib/inputProps"
import { findProject, readGroupBy, useProjects, writeGroupBy, type MeetingGroupBy } from "@/lib/projects"
import { MEETINGS, navigate } from "@/lib/route"

interface MeetingListProps {
  /** Show only this project's meetings (from #/meetings?project=…). */
  project: string | null
  /** Open the list filtered to a project. */
  onOpenProject: (name: string) => void
  onOpenMeeting: (meeting: Meeting) => void
  /** Opens the start-meeting popover (the empty state's call to action). */
  onStartMeeting?: () => void
  /** Opens the import-recording dialog (the empty state's link). */
  onImportRecording?: () => void
}

/** Stable empty list, so memo deps don't change on every render. */
const NO_MEETINGS: Meeting[] = []

/** Result of one debounced search, tagged with the query and attempt it
 *  answers so a stale or superseded result is never shown. */
type SearchResult = { q: string; attempt: number; meetings: Meeting[] | null; error: string | null }

const errorText = (err: unknown) => (err instanceof Error ? err.message : String(err))

/** Local midnight of the given time. */
function dayStart(t: Date): number {
  const d = new Date(t)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** Date group heading: Today, Yesterday, Last 7 days, else the month. */
function dateGroup(iso: string, today: number): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return i18next.t("meetingList.earlier")
  const day = dayStart(d)
  const daysAgo = Math.round((today - day) / 86_400_000)
  if (daysAgo <= 0) return i18next.t("meetingList.today")
  if (daysAgo === 1) return i18next.t("meetingList.yesterday")
  if (daysAgo < 7) return i18next.t("meetingList.last7Days")
  return formatMonth(d)
}

interface MeetingGroup {
  key: string
  label: string
  /** Set for a project group (its display name). */
  project?: string
  meetings: Meeting[]
}

/** Groups keep the list's newest-first order; "No project" goes last. */
function groupMeetings(ms: readonly Meeting[], by: MeetingGroupBy, canonical: (name: string) => string): MeetingGroup[] {
  const groups = new Map<string, MeetingGroup>()
  const today = dayStart(new Date())
  for (const m of ms) {
    let key: string
    let label: string
    let project: string | undefined
    if (by === "project") {
      const p = projectOf(m.tags)
      key = p ? `p:${projectKey(p)}` : "none"
      project = p ? canonical(p) : undefined
      label = project ?? i18next.t("meetingList.noProject")
    } else {
      label = dateGroup(m.started_at, today)
      key = `d:${label}`
    }
    const g = groups.get(key) ?? { key, label, project, meetings: [] }
    g.meetings.push(m)
    groups.set(key, g)
  }
  const out = [...groups.values()]
  const none = out.findIndex((g) => g.key === "none")
  if (none >= 0) out.push(...out.splice(none, 1))
  return out
}

export function MeetingList({ project, onOpenProject, onOpenMeeting, onStartMeeting, onImportRecording }: MeetingListProps) {
  const { t, i18n } = useTranslation()
  const [meetings, setMeetings] = useState<Meeting[] | null>(null)
  const [listError, setListError] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [activeTag, setActiveTag] = useState<string | null>(null)
  const [groupBy, setGroupByState] = useState<MeetingGroupBy>(readGroupBy)
  const [renaming, setRenaming] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const projects = useProjects()
  const groupLabelId = useId()
  const groupIdBase = useId()

  const setGroupBy = (v: MeetingGroupBy) => {
    setGroupByState(v)
    writeGroupBy(v)
  }
  // The project's spelling from /api/projects (most-used), else as given.
  const canonical = useCallback((name: string) => findProject(projects, name)?.name ?? name, [projects])
  const projectFilter = project ? canonical(project) : null

  const refresh = useCallback(() => {
    listMeetings()
      .then((ms) => {
        setMeetings(ms)
        setListError(null)
      })
      .catch((err: unknown) => setListError(errorText(err)))
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  // Debounced server-side search: the free-text `q` param hits the API,
  // while tag filtering below stays client-side over the fetched list.
  // "Searching" is derived (no result yet for this query + attempt), so
  // the effect only sets state from its async callbacks.
  const trimmedQuery = query.trim()
  const [searchAttempt, setSearchAttempt] = useState(0)
  const [search, setSearch] = useState<SearchResult | null>(null)
  useEffect(() => {
    if (!trimmedQuery) return
    let cancelled = false
    const timer = window.setTimeout(() => {
      listMeetings(trimmedQuery)
        .then((ms) => {
          if (!cancelled) setSearch({ q: trimmedQuery, attempt: searchAttempt, meetings: ms, error: null })
        })
        .catch((err: unknown) => {
          if (!cancelled) setSearch({ q: trimmedQuery, attempt: searchAttempt, meetings: null, error: errorText(err) })
        })
    }, 300)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [trimmedQuery, searchAttempt])

  const current =
    trimmedQuery && search && search.q === trimmedQuery && search.attempt === searchAttempt ? search : null
  const searching = trimmedQuery !== "" && current === null
  const searchError = current?.error ?? null

  const tagUniverse = useMemo(() => {
    const tags = new Set<string>()
    for (const m of meetings ?? []) for (const tg of plainTags(m.tags)) tags.add(tg)
    return [...tags].sort()
  }, [meetings])

  const base = trimmedQuery ? (current?.meetings ?? NO_MEETINGS) : (meetings ?? NO_MEETINGS)
  const visibleMeetings = useMemo(() => {
    const key = project ? projectKey(project) : null
    return base.filter(
      (m) =>
        (!activeTag || (m.tags ?? []).includes(activeTag)) && (!key || projectKey(projectOf(m.tags)) === key),
    )
  }, [base, activeTag, project])
  // A project filter shows one project, so it's always by date.
  const effectiveGroupBy: MeetingGroupBy = project ? "date" : groupBy
  // Group headers ("Today", month names) are translated inside
  // groupMeetings, so a language switch must regroup too.
  const language = i18n.language
  const groups = useMemo(
    () => groupMeetings(visibleMeetings, effectiveGroupBy, canonical),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [visibleMeetings, effectiveGroupBy, canonical, language],
  )

  const listEmpty = meetings !== null && meetings.length === 0 && !trimmedQuery
  const filtered = trimmedQuery !== "" || activeTag !== null || project !== null
  const noMatches =
    meetings !== null && !listEmpty && filtered && !searching && !searchError && visibleMeetings.length === 0

  const error = searchError
    ? t("meetingList.searchFailed", { error: searchError })
    : listError
      ? t("meetingList.loadFailed", { error: listError })
      : null
  const retry = () => {
    if (searchError) {
      setSearchAttempt((n) => n + 1)
    } else {
      setListError(null)
      refresh()
    }
  }
  const clearFilters = () => {
    setQuery("")
    setActiveTag(null)
    if (project) navigate(MEETINGS)
  }

  const onRenamed = (to: string, count: number) => {
    setNotice(t("meetingList.renamed", { name: to, count }))
    refresh()
    if (project) navigate({ name: "meetings", project: to }, { replace: true })
  }

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="flex items-center gap-2 text-2xl font-semibold text-balance">
          <ListVideo aria-hidden="true" className="size-6" /> {t("meetingList.title")}
        </h1>
      </div>

      {/* Hiding the card removes the focused Hide button; search is next. */}
      <GetStarted onHide={() => searchRef.current?.focus()} />

      <div className="relative mb-4">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          ref={searchRef}
          type="search"
          name="q"
          {...noAutofill}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={t("meetingList.searchPlaceholder")}
          className="pl-9"
          aria-label={t("meetingList.searchAria")}
        />
      </div>

      {projectFilter && (
        <div className="mb-4 flex flex-wrap items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground">{t("meetingList.projectLabel")}</span>
          <span className="inline-flex h-7 max-w-full items-center gap-1.5 rounded-full border border-primary bg-primary pr-0.5 pl-2.5 text-[0.8rem] font-medium text-primary-foreground">
            <Folder aria-hidden="true" className="size-3.5 shrink-0" />
            <ProjectDot name={projectFilter} className="ring-1 ring-primary-foreground/60" />
            <span className="min-w-0 truncate">{projectFilter}</span>
            <button
              type="button"
              onClick={() => navigate(MEETINGS)}
              aria-label={t("meetingList.clearFilterAria", { project: projectFilter })}
              title={t("meetingList.showAll")}
              className="grid size-5 cursor-pointer place-items-center rounded-full hover:bg-primary-foreground/15 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              <X aria-hidden="true" className="size-3" />
            </button>
          </span>
          <Button size="sm" variant="ghost" onClick={() => setRenaming(projectFilter)}>
            <Pencil aria-hidden="true" /> {t("meetingList.renameProject")}
          </Button>
        </div>
      )}

      {tagUniverse.length > 0 && (
        <div role="group" aria-label={t("meetingList.filterByTag")} className="mb-4 flex flex-wrap items-center gap-2">
          {tagUniverse.map((tag) => (
            <FilterChip
              key={tag}
              pressed={activeTag === tag}
              onPressedChange={(on) => setActiveTag(on ? tag : null)}
            >
              {tag}
            </FilterChip>
          ))}
        </div>
      )}

      {!project && meetings !== null && meetings.length > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-2">
          <span id={groupLabelId} className="text-xs font-medium text-muted-foreground">
            {t("meetingList.groupByLabel")}
          </span>
          <SegmentedControl
            labelledBy={groupLabelId}
            value={groupBy}
            onChange={setGroupBy}
            options={[
              { value: "date", label: t("meetingList.groupByDate") },
              { value: "project", label: t("meetingList.groupByProject") },
            ]}
          />
        </div>
      )}

      <StatusMessage tone="success" className="mb-4">
        {notice}
      </StatusMessage>

      <div className={error ? "mb-4 flex flex-wrap items-center gap-3" : undefined}>
        <StatusMessage tone="error" className="min-w-0 flex-1 break-words">
          {error}
        </StatusMessage>
        {error && (
          <Button size="sm" variant="outline" onClick={retry}>
            {t("common.tryAgain")}
          </Button>
        )}
      </div>

      {meetings === null ? (
        listError ? null : (
          <div className="grid gap-3" aria-busy="true">
            <p className="sr-only" role="status">
              {t("meetingList.loading")}
            </p>
            {[0, 1, 2].map((i) => (
              <Card key={i}>
                <CardHeader>
                  <div className="flex items-start justify-between gap-2">
                    <Skeleton className="h-5 w-2/5" />
                    <Skeleton className="h-5 w-12 rounded-full" />
                  </div>
                </CardHeader>
                <CardContent>
                  <Skeleton className="h-4 w-1/3" />
                </CardContent>
              </Card>
            ))}
          </div>
        )
      ) : listEmpty ? (
        <div className="rounded-xl border border-dashed border-border px-6 py-16 text-center">
          <img src={logo} alt="" width={64} height={64} className="mx-auto mb-4 size-16 rounded-xl" />
          <p className="text-lg font-medium">{t("meetingList.emptyTitle")}</p>
          <p className="mt-1 text-sm text-pretty text-muted-foreground">{t("meetingList.emptyBody")}</p>
          {onStartMeeting && (
            <Button className="mt-6" onClick={onStartMeeting}>
              <Mic /> {t("sidebar.startMeeting")}
            </Button>
          )}
          {onImportRecording && (
            <div className="mt-3">
              <Button variant="link" className="text-muted-foreground" onClick={onImportRecording}>
                {t("meetingList.importRecording")}
              </Button>
            </div>
          )}
        </div>
      ) : searching ? (
        <p role="status" className="py-8 text-center text-sm text-muted-foreground">
          {t("meetingList.searching")}
        </p>
      ) : noMatches ? (
        <div className="grid justify-items-center gap-3 py-8 text-center">
          <p role="status" className="text-sm text-muted-foreground">
            {trimmedQuery
              ? t("meetingList.noMatchesSearch")
              : activeTag
                ? t("meetingList.noMatchesTag")
                : t("meetingList.noMatchesProject")}
          </p>
          <Button size="sm" variant="outline" onClick={clearFilters}>
            {t("meetingList.clearFilters")}
          </Button>
        </div>
      ) : (
        <div className="grid gap-6">
          {groups.map((g, i) => (
            <section key={g.key} aria-labelledby={`${groupIdBase}-${i}`} className="grid gap-2">
              <h2
                id={`${groupIdBase}-${i}`}
                className="flex min-w-0 items-center gap-2 text-sm font-medium text-muted-foreground"
              >
                {g.project ? (
                  <>
                    <ProjectDot name={g.project} />
                    <button
                      type="button"
                      onClick={() => onOpenProject(g.project ?? "")}
                      title={t("meetingList.showOnly", { name: g.project })}
                      className="min-w-0 cursor-pointer truncate rounded-sm underline-offset-2 hover:text-foreground hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    >
                      {g.project}
                    </button>
                  </>
                ) : (
                  <span className="min-w-0 truncate">{g.label}</span>
                )}
                <span className="shrink-0 font-normal">
                  <span className="sr-only">, </span>
                  <span aria-hidden="true">· </span>
                  {t("common.meetingCount", { count: g.meetings.length })}
                </span>
              </h2>
              <ul className="grid gap-3">
                {g.meetings.map((m) => (
                  <MeetingRow key={m.id} meeting={m} onOpen={onOpenMeeting} canonical={canonical} />
                ))}
              </ul>
            </section>
          ))}
        </div>
      )}

      <RenameProjectDialog
        project={renaming}
        projects={projects}
        onOpenChange={(open) => {
          if (!open) setRenaming(null)
        }}
        onRenamed={onRenamed}
      />
    </div>
  )
}

/**
 * "All projects": every project with its meeting count and last meeting,
 * a way into each one, and rename.
 */
export function AllProjects({ onOpenProject }: { onOpenProject: (name: string) => void }) {
  const { t } = useTranslation()
  const projects = useProjects()
  const [renaming, setRenaming] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <h1 className="mb-2 flex items-center gap-2 text-2xl font-semibold text-balance">
        <FolderOpen aria-hidden="true" className="size-6" /> {t("meetingList.projectsTitle")}
      </h1>
      <p className="mb-6 text-sm text-pretty text-muted-foreground">{t("meetingList.projectsIntro")}</p>

      <StatusMessage tone="success" className="mb-4">
        {notice}
      </StatusMessage>

      {projects === null ? (
        <div className="grid gap-3" aria-busy="true">
          <p className="sr-only" role="status">
            {t("meetingList.loadingProjects")}
          </p>
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-16 w-full rounded-xl" />
          ))}
        </div>
      ) : projects.length === 0 ? (
        <div className="rounded-xl border border-dashed border-border px-6 py-16 text-center">
          <p className="text-lg font-medium">{t("meetingList.noProjectsTitle")}</p>
          <p className="mt-1 text-sm text-pretty text-muted-foreground">{t("meetingList.noProjectsBody")}</p>
        </div>
      ) : (
        <ul className="grid gap-3">
          {projects.map((p) => (
            <li key={p.name}>
              <Card className="relative transition-colors hover:bg-secondary/40 has-[button.project-open:focus-visible]:ring-2 has-[button.project-open:focus-visible]:ring-ring">
                <CardHeader className="flex flex-row items-center gap-3">
                  <ProjectDot name={p.name} className="size-3" />
                  <div className="min-w-0 flex-1">
                    <CardTitle className="min-w-0">
                      <button
                        type="button"
                        onClick={() => onOpenProject(p.name)}
                        className="project-open block w-full cursor-pointer truncate text-left after:absolute after:inset-0 after:rounded-xl focus-visible:outline-none"
                      >
                        {p.name}
                      </button>
                    </CardTitle>
                    <CardDescription>
                      {t("meetingList.projectMeta", { count: p.meetings, date: formatDay(p.last_meeting_at) })}
                    </CardDescription>
                  </div>
                  {/* Above the stretched title link, so it stays its own target. */}
                  <Button
                    variant="ghost"
                    size="sm"
                    className="relative z-10"
                    onClick={() => setRenaming(p.name)}
                    aria-label={t("meetingList.renameAria", { name: p.name })}
                  >
                    <Pencil aria-hidden="true" /> {t("meetingList.rename")}
                  </Button>
                </CardHeader>
              </Card>
            </li>
          ))}
        </ul>
      )}

      <RenameProjectDialog
        project={renaming}
        projects={projects}
        onOpenChange={(open) => {
          if (!open) setRenaming(null)
        }}
        onRenamed={(to, count) => setNotice(t("meetingList.renamed", { name: to, count }))}
      />
    </div>
  )
}

/**
 * One meeting card. The title is the only interactive element; its ::after
 * stretches over the whole card so the card is still one click target,
 * without putting block content inside a <button> or folding the whole
 * card's text into the button's accessible name.
 */
function MeetingRow({
  meeting: m,
  onOpen,
  canonical,
}: {
  meeting: Meeting
  onOpen: (m: Meeting) => void
  canonical: (name: string) => string
}) {
  const { t } = useTranslation()
  const preview = previewLine(m)
  const project = projectOf(m.tags)
  return (
    <li>
      {/* content-visibility on the Card, not the <li>: its paint
          containment would clip the Card's ring (a box-shadow). */}
      <Card className="relative [contain-intrinsic-size:auto_7.5rem] [content-visibility:auto] transition-colors hover:bg-secondary/40 has-[button:focus-visible]:ring-2 has-[button:focus-visible]:ring-ring">
        <CardHeader>
          <div className="flex min-w-0 items-center justify-between gap-2">
            <CardTitle className="min-w-0">
              <button
                type="button"
                onClick={() => onOpen(m)}
                className="block w-full cursor-pointer truncate text-left after:absolute after:inset-0 after:rounded-xl focus-visible:outline-none"
              >
                {m.title}
              </button>
            </CardTitle>
            {!m.ended_at && (
              <Badge className="gap-1.5">
                <span aria-hidden="true" className="size-1.5 motion-safe:animate-pulse rounded-full bg-current" />
                {t("meetingList.live")}
              </Badge>
            )}
          </div>
          <CardDescription>
            {m.ended_at ? formatDateRange(m.started_at, m.ended_at) : t("meetingList.startedAt", { date: formatDateTime(m.started_at) })}
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-1.5">
          {preview && <p className="truncate text-sm text-muted-foreground">{preview}</p>}
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-sm text-muted-foreground">
            {m.ended_at && !m.refined ? (
              <span>{t("meetingList.noSummary")}</span>
            ) : (
              <>
                <span>{t("meetingList.decisionCount", { count: m.decisions?.length ?? 0 })}</span>
                <span>{t("meetingList.actionItemCount", { count: m.action_items?.length ?? 0 })}</span>
              </>
            )}
            {project && <ProjectBadge name={canonical(project)} />}
            {plainTags(m.tags).map((tg) => (
              <Badge key={tg} variant="secondary">
                {tg}
              </Badge>
            ))}
          </div>
        </CardContent>
      </Card>
    </li>
  )
}

/** One-line preview: first decision if any, else the summary. */
function previewLine(m: Meeting): string {
  const first = m.decisions?.[0]?.text?.trim()
  if (first) return first
  return m.summary?.trim() ?? ""
}
