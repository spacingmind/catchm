import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Search } from "lucide-react"

import logo from "@/assets/logo.png"
import { Badge } from "@/components/ui/badge"
import { CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { listMeetings, type Meeting } from "@/lib/api"

interface MeetingListProps {
  onOpenMeeting: (meeting: Meeting) => void
}

export function MeetingList({ onOpenMeeting }: MeetingListProps) {
  const [meetings, setMeetings] = useState<Meeting[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [activeTag, setActiveTag] = useState<string | null>(null)

  const refresh = useCallback(() => {
    listMeetings()
      .then((ms) => {
        setMeetings(ms)
        setError(null)
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  // Debounced server-side search: the free-text `q` param hits the API,
  // while tag filtering below stays client-side over the fetched list.
  const searchTimer = useRef<number | null>(null)
  const [searchResults, setSearchResults] = useState<Meeting[] | null>(null)
  const [searching, setSearching] = useState(false)
  useEffect(() => {
    if (searchTimer.current !== null) window.clearTimeout(searchTimer.current)
    const q = query.trim()
    if (!q) {
      setSearchResults(null)
      setSearching(false)
      return
    }
    setSearching(true)
    searchTimer.current = window.setTimeout(() => {
      listMeetings(q)
        .then((ms) => {
          setSearchResults(ms)
          setError(null)
        })
        .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)))
        .finally(() => setSearching(false))
    }, 300)
    return () => {
      if (searchTimer.current !== null) window.clearTimeout(searchTimer.current)
    }
  }, [query])

  const tagUniverse = useMemo(() => {
    const tags = new Set<string>()
    for (const m of meetings ?? []) for (const t of m.tags ?? []) tags.add(t)
    return [...tags].sort()
  }, [meetings])

  const trimmedQuery = query.trim()
  const base = trimmedQuery ? (searchResults ?? []) : (meetings ?? [])
  const visibleMeetings = useMemo(
    () => (activeTag ? base.filter((m) => (m.tags ?? []).includes(activeTag)) : base),
    [base, activeTag],
  )

  const listEmpty = meetings !== null && meetings.length === 0 && !trimmedQuery
  const noMatches =
    meetings !== null && meetings.length > 0 && trimmedQuery !== "" && !searching && visibleMeetings.length === 0

  return (
    <div className="mx-auto max-w-3xl px-6 py-8">
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Meetings</h1>
      </div>

      <div className="relative mb-4">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search meetings…"
          className="pl-9"
          aria-label="Search meetings"
        />
      </div>

      {tagUniverse.length > 0 && (
        <div className="mb-4 flex flex-wrap items-center gap-2">
          {tagUniverse.map((tag) => (
            <button
              key={tag}
              type="button"
              onClick={() => setActiveTag(activeTag === tag ? null : tag)}
              className="rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
              aria-pressed={activeTag === tag}
            >
              <Badge variant={activeTag === tag ? "default" : "outline"}>{tag}</Badge>
            </button>
          ))}
        </div>
      )}

      {error && <p className="mb-4 text-sm text-destructive">{error}</p>}

      {meetings === null ? (
        <div className="grid gap-3" aria-busy="true">
          {[0, 1, 2].map((i) => (
            <div key={i} className="rounded-xl border border-border bg-card">
              <div className="flex items-start justify-between gap-2 p-6 pb-2">
                <Skeleton className="h-5 w-2/5" />
                <Skeleton className="h-5 w-12 rounded-full" />
              </div>
              <div className="p-6 pt-0">
                <Skeleton className="h-4 w-1/3" />
              </div>
            </div>
          ))}
        </div>
      ) : listEmpty ? (
        <div className="rounded-xl border border-dashed border-border py-16 text-center">
          <img src={logo} alt="" className="mx-auto mb-4 size-16 rounded-xl" />
          <p className="text-lg font-medium">Your burrow is quiet.</p>
          <p className="mt-1 text-sm text-muted-foreground">
            Start a meeting to capture the conversation, decisions, and follow-ups in one place.
          </p>
        </div>
      ) : noMatches ? (
        <p className="py-8 text-center text-sm text-muted-foreground">No matches in the stash.</p>
      ) : (
        <div className="grid gap-3">
          {visibleMeetings.map((m) => (
            <button
              key={m.id}
              type="button"
              onClick={() => onOpenMeeting(m)}
              className="cursor-pointer rounded-xl border border-border bg-card text-left transition-colors hover:bg-secondary/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              <CardHeader>
                <div className="flex min-w-0 items-center justify-between gap-2">
                  <CardTitle className="truncate">{m.title}</CardTitle>
                  {!m.ended_at && (
                    <Badge className="gap-1.5">
                      <span className="size-1.5 motion-safe:animate-pulse rounded-full bg-current" />
                      Live
                    </Badge>
                  )}
                </div>
                <CardDescription>
                  {formatMeetingSpan(m)}
                  {m.engine ? ` · ${m.engine}` : ""}
                </CardDescription>
              </CardHeader>
              <CardContent className="grid gap-1.5">
                {previewLine(m) && (
                  <p className="truncate text-sm text-muted-foreground">{previewLine(m)}</p>
                )}
                <div className="flex flex-wrap items-center gap-4 text-sm text-muted-foreground">
                  <span>{m.decisions?.length ?? 0} decisions</span>
                  <span>{m.action_items?.length ?? 0} action items</span>
                  {(m.tags ?? []).map((t) => (
                    <Badge key={t} variant="secondary">
                      {t}
                    </Badge>
                  ))}
                </div>
              </CardContent>
            </button>
          ))}
        </div>
      )}

    </div>
  )
}

/** One-line preview: first decision if any, else the summary. */
function previewLine(m: Meeting): string {
  const first = m.decisions?.[0]?.text?.trim()
  if (first) return first
  return m.summary?.trim() ?? ""
}

function formatMeetingSpan(m: Meeting): string {
  const start = new Date(m.started_at)
  if (!m.ended_at) return `Started ${start.toLocaleString()}`

  const end = new Date(m.ended_at)
  if (start.toDateString() === end.toDateString()) {
    return `${start.toLocaleDateString()}, ${start.toLocaleTimeString()} – ${end.toLocaleTimeString()}`
  }
  return `${start.toLocaleString()} – ${end.toLocaleString()}`
}
