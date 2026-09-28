import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type MouseEvent } from "react"

import { AppSidebar, type View } from "@/components/AppSidebar"
import { LiveMeeting } from "@/components/LiveMeeting"
import { MeetingList } from "@/components/MeetingList"
import { StartMeetingPopover } from "@/components/StartMeetingPopover"
import { StatusMessage } from "@/components/StatusMessage"
import { UpdateBanner } from "@/components/UpdateBanner"
import { ActionItemsSkeleton, MeetingDetailSkeleton, SettingsSkeleton } from "@/components/ViewSkeletons"
import { Button } from "@/components/ui/button"
import type { Section as SettingsSection } from "@/components/settings/Settings"
import { getMeeting, listMeetings, type CaptureErrorKind, type Meeting, type StartMeetingResponse } from "@/lib/api"
import { formatRoute, MEETINGS, navigate, parseRoute, useHash, type Route } from "@/lib/route"
import { hasWindowControls, notifyReadyForEvents, windowEventsOn } from "@/lib/wailsRuntime"

// Views off the recording path load on first visit, keeping the main
// chunk under Vite's 500 kB warning. List and Live stay eager: they're
// the first screen and the one a tray/toast click must open instantly.
const MeetingDetail = lazy(() => import("@/components/MeetingDetail").then((m) => ({ default: m.MeetingDetail })))
const ActionItems = lazy(() => import("@/components/ActionItems").then((m) => ({ default: m.ActionItems })))
const Settings = lazy(() => import("@/components/settings/Settings").then((m) => ({ default: m.Settings })))

/** How often the live state is re-derived from the server. */
const LIVE_POLL_MS = 3000

/**
 * Which Settings section fixes a given capture_error_kind: device/input
 * problems point at Recording, everything else (engine construction —
 * missing API key, unreachable server, unknown engine, ...) points at
 * Transcription, where the engine and its key/URLs live. Switches on the
 * structured kind (internal/server's CaptureErrorKind), never the
 * human-readable message text, whose wording the backend must stay free
 * to reword.
 */
function captureErrorSection(kind: CaptureErrorKind): SettingsSection {
  switch (kind) {
    case "device_unavailable":
    case "unsupported":
      return "recording"
    default:
      return "transcription"
  }
}

const MISSING_MEETING = "Couldn’t open that meeting. It may have been deleted; pick one from the list instead."

/** Which shell view a route shows (the sidebar highlights by view). */
function viewOf(route: Route): View {
  switch (route.name) {
    case "meetings":
      return "list"
    case "meeting":
      return "detail"
    default:
      return route.name
  }
}

function App() {
  // The hash route is the source of truth for where the user is (view,
  // meeting, settings section, detail tab), so reload, Back/Forward and
  // deep links all land on the same screen.
  const hash = useHash()
  const route = useMemo(() => parseRoute(hash), [hash])
  const view = viewOf(route)
  const routeId = route.name === "meeting" || route.name === "live" ? route.id : null
  // The routed meeting, once known. Set directly when we already hold a
  // fresh copy (a list row, the start response, a toast fetch), fetched
  // by routeId otherwise (reload, deep link, Back/Forward).
  const [selected, setSelected] = useState<Meeting | null>(null)
  const selectedId = selected?.id ?? null
  const current = selected && selected.id === routeId ? selected : null
  // The meeting currently recording (at most one in the UI's model), kept
  // independently of `selected` so browsing past meetings doesn't lose the
  // sidebar's recording indicator.
  const [live, setLive] = useState<Meeting | null>(null)
  const [startOpen, setStartOpen] = useState(false)
  // Sidebar Start button — the popover anchors above it.
  const startBtnRef = useRef<HTMLButtonElement | null>(null)
  // capture_error from the start response (M19): the meeting exists but
  // recording didn't start. Shown as a live-view banner until ended.
  // capture_error_kind (its structured classification) rides along so
  // "Open settings" routes by kind, not by matching the message text.
  const [captureError, setCaptureError] = useState<string | null>(null)
  const [captureErrorKind, setCaptureErrorKind] = useState<CaptureErrorKind | null>(null)
  // M21 §4: LiveMeeting owns the compact-overlay hook (it needs the
  // transcript state that lives there), but the sidebar/shell has to
  // disappear too — a 480x260 window has no room for both. LiveMeeting
  // reports the flag up through this callback rather than the hook
  // itself moving, so the transcript polling doesn't have to move too.
  const [liveCompact, setLiveCompact] = useState(false)
  // Shell-level notice (a meeting link that points at a meeting that no
  // longer exists). Tagged with the route it was raised on, so the next
  // navigation clears it.
  const [notice, setNotice] = useState<{ text: string; hash: string } | null>(null)
  // Dropped during render (not in an effect) so a stale notice never
  // paints on the new route, nor returns when the user comes back.
  if (notice && notice.hash !== hash) setNotice(null)
  const noticeText = notice?.hash === hash ? notice.text : null

  // Unknown or non-canonical URLs ("", "#/foo", "#/meetings/") become
  // the canonical form of what they parsed to, in place.
  useEffect(() => {
    if (hash !== formatRoute(route)) navigate(route, { replace: true })
  }, [hash, route])

  /** Land on the list and say why the meeting didn't open. */
  const showMissingMeeting = useCallback((replace: boolean) => {
    setSelected(null)
    navigate(MEETINGS, { replace })
    setNotice({ text: MISSING_MEETING, hash: formatRoute(MEETINGS) })
  }, [])

  const openMeeting = useCallback((meeting: Meeting) => {
    setSelected(meeting)
    if (!meeting.ended_at) setLive(meeting)
    navigate(meeting.ended_at ? { name: "meeting", id: meeting.id, tab: "overview" } : { name: "live", id: meeting.id })
  }, [])

  // A routed meeting we don't hold yet (reload, deep link, Back to a
  // meeting since dropped): fetch it.
  useEffect(() => {
    if (!routeId || selectedId === routeId) return
    let cancelled = false
    getMeeting(routeId)
      .then((m) => {
        if (cancelled) return
        setSelected(m)
        if (!m.ended_at) setLive(m)
      })
      .catch(() => {
        // The bad URL shouldn't stay in history, hence replace.
        if (!cancelled) showMissingMeeting(true)
      })
    return () => {
      cancelled = true
    }
  }, [routeId, selectedId, showMissingMeeting])

  // ended_at decides live vs detail, whatever the URL says: a meeting
  // that ended (#/live/{id} from history, a stale link) opens its
  // detail, and one still recording opens live. Replace, so Back
  // doesn't bounce through the redirect.
  useEffect(() => {
    if (!current) return
    if (route.name === "live" && current.ended_at) {
      navigate({ name: "meeting", id: current.id, tab: "overview" }, { replace: true })
    } else if (route.name === "meeting" && !current.ended_at) {
      navigate({ name: "live", id: current.id }, { replace: true })
    }
  }, [route, current])

  // catchm://meeting/<id> protocol activation (a "Meeting ended" toast
  // click): the Go side (cmd/catchm-desktop/activate.go) emits
  // app:open-meeting with the id — cold start (the toast launched the
  // app) and already-running (OnSecondInstanceLaunch) alike. The
  // meeting is fetched fresh rather than trusted from stale state: a
  // toast can sit in Action Center long past the meeting's lifetime,
  // and ended_at decides live vs detail view.
  const openMeetingByID = useCallback(
    (id: string) => {
      getMeeting(id)
        .then(openMeeting)
        .catch(() => showMissingMeeting(false))
    },
    [openMeeting, showMissingMeeting],
  )

  useEffect(() => {
    // Order matters: subscribe FIRST, then announce readiness — a
    // cold-start URI is held by the Go side until ReadyForEvents, so
    // this subscription is guaranteed to exist before the event fires.
    const cancel = windowEventsOn("app:open-meeting", (data) => {
      const id = Array.isArray(data) ? data[0] : data
      if (typeof id === "string") openMeetingByID(id)
    })
    notifyReadyForEvents()
    return cancel
  }, [openMeetingByID])

  // M22 §1: let Mica show through the shell (body + sidebar, via
  // index.css's html.desktop-mica rules) only inside the real Wails
  // window. hasWindowControls() never changes after mount, so this runs
  // once; a plain browser (catchm serve) never adds the class and keeps
  // the normal opaque page.
  useEffect(() => {
    if (hasWindowControls()) {
      document.documentElement.classList.add("desktop-mica")
    }
  }, [])

  // Server is the source of truth for "is anything recording": refresh on
  // mount and every few seconds so a reload/restart or a meeting started
  // elsewhere (MCP tools, another window) still shows the indicator. Same
  // rule as the M11 tray tooltip: the meeting with no ended_at. Optimistic
  // updates in openMeeting/onEnded win until the next poll corrects them.
  useEffect(() => {
    let cancelled = false
    const sync = () => {
      listMeetings()
        .then((ms) => {
          if (!cancelled) setLive(ms.find((m) => !m.ended_at) ?? null)
        })
        .catch(() => {
          // Server unreachable — keep the last known state.
        })
    }
    sync()
    const id = setInterval(sync, LIVE_POLL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [])

  const handleStarted = (meeting: StartMeetingResponse) => {
    setCaptureError(meeting.capture_error ?? null)
    setCaptureErrorKind(meeting.capture_error_kind ?? null)
    openMeeting(meeting)
  }

  const goToList = useCallback(() => {
    // Drop the cached meeting: Back to it (e.g. after a delete) refetches
    // and reports a missing one instead of showing the stale copy.
    setSelected(null)
    navigate(MEETINGS)
  }, [])

  const afterDelete = useCallback(() => {
    setSelected(null)
    navigate(MEETINGS, { replace: true })
  }, [])

  const openLive = useCallback(() => {
    if (!live) return
    setSelected(live)
    navigate({ name: "live", id: live.id })
  }, [live])

  const startMeeting = useCallback(() => setStartOpen(true), [])
  // No section (Ctrl/⌘, and the sidebar item): Settings picks its
  // default, or stays on the section already open. Live's "Open
  // settings" buttons (capture_error banner, engine error) name one.
  const openSettings = useCallback((section?: SettingsSection) => {
    if (!section && parseRoute(window.location.hash).name === "settings") return
    navigate({ name: "settings", section: section ?? null })
  }, [])

  const skipToContent = (e: MouseEvent<HTMLAnchorElement>) => {
    // A real #main jump would replace the route with "#main"; move focus
    // (and scroll) by hand instead.
    e.preventDefault()
    const main = document.getElementById("main")
    main?.focus()
    main?.scrollIntoView()
  }

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey || e.shiftKey) return
      const key = e.key.toLowerCase()
      if (key === "n") {
        e.preventDefault()
        startMeeting()
      } else if (key === ",") {
        e.preventDefault()
        openSettings()
      }
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [startMeeting, openSettings])

  // M21 §4: a 480x260 compact window has no room for the sidebar or
  // anything else. LiveMeeting stays mounted at the same position in
  // the tree either way (only the wrapper/siblings around it change) so
  // toggling compact mode never remounts it — losing the transcript
  // poll's cursor or an in-progress note draft on every toggle would be
  // a bad trade for a CSS-only problem.
  const showLive = view === "live" && current !== null && !current.ended_at
  const showCompactShell = liveCompact && showLive

  return (
    <div className={showCompactShell ? "bg-background text-foreground" : "flex min-h-svh text-foreground"}>
      {/* Skip link: first tab stop, visible only while focused. */}
      {!showCompactShell && (
        <a
          href="#main"
          onClick={skipToContent}
          className="fixed top-3 left-3 z-50 -translate-y-[200%] rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground shadow-md focus:translate-y-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 motion-safe:transition-transform"
        >
          Skip to content
        </a>
      )}

      {/* Compact mode always stays opaque (a small floating reading window,
          unaffected by M22 — Mica is out of its scope). The normal shell
          drops its own bg-background so html.desktop-mica's transparent
          body (index.css) can show Mica through here and the sidebar; a
          plain browser still gets an opaque page from body's own rule. */}
      {!showCompactShell && (
        <AppSidebar
          view={view}
          onNavigate={(v) => {
            if (v === "live") openLive()
            else if (v === "settings") openSettings()
            else if (v === "action-items") navigate({ name: "action-items" })
            else navigate(MEETINGS)
          }}
          liveTitle={live?.title ?? null}
          onStartMeeting={startMeeting}
          startButtonRef={startBtnRef}
        />
      )}

      {/* main: always opaque — content (meeting list, transcript, notes,
          settings) stays readable over Mica regardless of the shell. */}
      {/* tabIndex -1: the skip link moves focus here, not just scroll. */}
      <main
        id="main"
        tabIndex={-1}
        className={
          showCompactShell
            ? ""
            : view === "live"
              ? // Live pins the transcript to the window: main is the
                // viewport-tall column and LiveMeeting fills whatever
                // the update banner/notice above it leave over.
                "flex h-svh min-w-0 flex-1 flex-col bg-background outline-none"
              : "min-w-0 flex-1 bg-background outline-none"
        }
      >
        {/* M23: update banner (self-mounted, no-op until "update:available"
            fires). In normal flow at the top of main, so it never covers
            the sidebar header. Hidden in compact mode — a 480x260 overlay
            has no room. */}
        {!showCompactShell && <UpdateBanner />}
        {!showCompactShell && (
          <div className={noticeText ? "mx-auto flex max-w-3xl items-start gap-3 px-6 pt-6" : undefined}>
            <StatusMessage tone="error" className="min-w-0 flex-1 pt-1">
              {noticeText}
            </StatusMessage>
            {noticeText && (
              <Button size="sm" variant="ghost" onClick={() => setNotice(null)}>
                Dismiss
              </Button>
            )}
          </div>
        )}
        {!showCompactShell && view === "list" && (
          <MeetingList onOpenMeeting={openMeeting} onStartMeeting={startMeeting} />
        )}
        {/* A routed meeting still being fetched (reload, deep link). Its
            own slot, so LiveMeeting's position below never shifts. */}
        {!showCompactShell && routeId !== null && current === null && <MeetingDetailSkeleton />}
        {showLive && (
          <LiveMeeting
            meeting={current}
            captureError={captureError}
            captureErrorSettings={captureError ? captureErrorSection(captureErrorKind ?? "engine") : undefined}
            onOpenSettings={openSettings}
            onCompactChange={setLiveCompact}
            onEnded={(m) => {
              setLive(null)
              setCaptureError(null)
              setCaptureErrorKind(null)
              setSelected(m)
              // Replace: Back shouldn't return to a live view that's over.
              navigate({ name: "meeting", id: m.id, tab: "overview" }, { replace: true })
            }}
          />
        )}
        {!showCompactShell && view === "detail" && current?.ended_at && (
          <Suspense fallback={<MeetingDetailSkeleton />}>
            <MeetingDetail
              key={current.id}
              meetingId={current.id}
              initialMeeting={current}
              onBack={goToList}
              onDeleted={afterDelete}
            />
          </Suspense>
        )}
        {!showCompactShell && view === "action-items" && (
          <Suspense fallback={<ActionItemsSkeleton />}>
            <ActionItems onOpenMeeting={openMeeting} />
          </Suspense>
        )}
        {!showCompactShell && view === "settings" && (
          <Suspense fallback={<SettingsSkeleton />}>
            <Settings />
          </Suspense>
        )}
      </main>

      {!showCompactShell && (
        <StartMeetingPopover open={startOpen} onOpenChange={setStartOpen} onStarted={handleStarted} anchor={startBtnRef} onOpenSettings={openSettings} />
      )}
    </div>
  )
}

export default App
