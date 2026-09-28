import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Mic, Search } from "lucide-react"

import logo from "@/assets/logo.png"
import { FilterChip } from "@/components/FilterChip"
import { GetStarted } from "@/components/GetStarted"
import { StatusMessage } from "@/components/StatusMessage"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { listMeetings, type Meeting } from "@/lib/api"
import { formatDateRange, formatDateTime } from "@/lib/format"
import { noAutofill } from "@/lib/inputProps"
import { pluralize } from "@/lib/utils"

interface MeetingListProps {
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

export function MeetingList({ onOpenMeeting, onStartMeeting, onImportRecording }: MeetingListProps) {
  const [meetings, setMeetings] = useState<Meeting[] | null>(null)
  const [listError, setListError] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [activeTag, setActiveTag] = useState<string | null>(null)
  const searchRef = useRef<HTMLInputElement>(null)

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
    for (const m of meetings ?? []) for (const t of m.tags ?? []) tags.add(t)
    return [...tags].sort()
  }, [meetings])

  const base = trimmedQuery ? (current?.meetings ?? NO_MEETINGS) : (meetings ?? NO_MEETINGS)
  const visibleMeetings = useMemo(
    () => (activeTag ? base.filter((m) => (m.tags ?? []).includes(activeTag)) : base),
    [base, activeTag],
  )

  const listEmpty = meetings !== null && meetings.length === 0 && !trimmedQuery
  const filtered = trimmedQuery !== "" || activeTag !== null
  const noMatches =
    meetings !== null && !listEmpty && filtered && !searching && !searchError && visibleMeetings.length === 0

  const error = searchError
    ? `Search failed: ${searchError}. Try again, or restart CatchM if it keeps failing.`
    : listError
      ? `Couldn’t load meetings: ${listError}. Try again, or restart CatchM if it keeps failing.`
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
  }

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-semibold text-balance">Meetings</h1>
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
          placeholder="Search meetings…"
          className="pl-9"
          aria-label="Search meetings"
        />
      </div>

      {tagUniverse.length > 0 && (
        <div role="group" aria-label="Filter by tag" className="mb-4 flex flex-wrap items-center gap-2">
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

      <div className={error ? "mb-4 flex flex-wrap items-center gap-3" : undefined}>
        <StatusMessage tone="error" className="min-w-0 flex-1 break-words">
          {error}
        </StatusMessage>
        {error && (
          <Button size="sm" variant="outline" onClick={retry}>
            Try again
          </Button>
        )}
      </div>

      {meetings === null ? (
        listError ? null : (
          <div className="grid gap-3" aria-busy="true">
            <p className="sr-only" role="status">
              Loading meetings…
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
          <p className="text-lg font-medium">No meetings yet.</p>
          <p className="mt-1 text-sm text-pretty text-muted-foreground">
            Start a meeting to capture the conversation, decisions, and follow-ups in one place.
          </p>
          {onStartMeeting && (
            <Button className="mt-6" onClick={onStartMeeting}>
              <Mic /> Start meeting
            </Button>
          )}
          {onImportRecording && (
            <div className="mt-3">
              <Button variant="link" className="text-muted-foreground" onClick={onImportRecording}>
                Import a recording you already have
              </Button>
            </div>
          )}
        </div>
      ) : searching ? (
        <p role="status" className="py-8 text-center text-sm text-muted-foreground">
          Searching…
        </p>
      ) : noMatches ? (
        <div className="grid justify-items-center gap-3 py-8 text-center">
          <p role="status" className="text-sm text-muted-foreground">
            {trimmedQuery ? "No meetings match your search." : "No meetings have this tag."}
          </p>
          <Button size="sm" variant="outline" onClick={clearFilters}>
            Clear filters
          </Button>
        </div>
      ) : (
        <ul className="grid gap-3">
          {visibleMeetings.map((m) => (
            <MeetingRow key={m.id} meeting={m} onOpen={onOpenMeeting} />
          ))}
        </ul>
      )}
    </div>
  )
}

/**
 * One meeting card. The title is the only interactive element; its ::after
 * stretches over the whole card so the card is still one click target,
 * without putting block content inside a <button> or folding the whole
 * card's text into the button's accessible name.
 */
function MeetingRow({ meeting: m, onOpen }: { meeting: Meeting; onOpen: (m: Meeting) => void }) {
  const preview = previewLine(m)
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
                Live
              </Badge>
            )}
          </div>
          <CardDescription>
            {m.ended_at ? formatDateRange(m.started_at, m.ended_at) : `Started ${formatDateTime(m.started_at)}`}
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-1.5">
          {preview && <p className="truncate text-sm text-muted-foreground">{preview}</p>}
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-sm text-muted-foreground">
            {m.ended_at && !m.refined ? (
              <span>No summary yet</span>
            ) : (
              <>
                <span>{pluralize(m.decisions?.length ?? 0, "decision")}</span>
                <span>{pluralize(m.action_items?.length ?? 0, "action item")}</span>
              </>
            )}
            {(m.tags ?? []).map((t) => (
              <Badge key={t} variant="secondary">
                {t}
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
