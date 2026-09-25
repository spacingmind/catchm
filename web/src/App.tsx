import { useCallback, useEffect, useRef, useState } from "react"

import { ActionItems } from "@/components/ActionItems"
import { AppSidebar, type View } from "@/components/AppSidebar"
import { LiveMeeting } from "@/components/LiveMeeting"
import { MeetingDetail } from "@/components/MeetingDetail"
import { MeetingList } from "@/components/MeetingList"
import { StartMeetingPopover } from "@/components/StartMeetingPopover"
import { UpdateBanner } from "@/components/UpdateBanner"
import { Settings, type Section as SettingsSection } from "@/components/settings/Settings"
import { listMeetings, type CaptureErrorKind, type Meeting, type StartMeetingResponse } from "@/lib/api"
import { getMeeting } from "@/lib/api"
import { hasWindowControls, notifyReadyForEvents, windowEventsOn } from "@/lib/wailsRuntime"

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

function App() {
  const [view, setView] = useState<View>("list")
  const [selected, setSelected] = useState<Meeting | null>(null)
  // The meeting currently recording (at most one in the UI's model), kept
  // independently of `selected` so browsing past meetings doesn't lose the
  // sidebar's recording indicator.
  const [live, setLive] = useState<Meeting | null>(null)
  const [startOpen, setStartOpen] = useState(false)
  // Which Settings section to land on next time it opens (Ctrl/⌘, and the
  // sidebar item leave this unset, so Settings falls back to its own
  // default — only the capture_error banner's "Open settings" sets it).
  const [settingsSection, setSettingsSection] = useState<SettingsSection | undefined>(undefined)
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

  // catchm://meeting/<id> protocol activation (a "Meeting ended" toast
  // click): the Go side (cmd/catchm-desktop/activate.go) emits
  // app:open-meeting with the id — cold start (the toast launched the
  // app) and already-running (OnSecondInstanceLaunch) alike. The
  // meeting is fetched fresh rather than trusted from stale state: a
  // toast can sit in Action Center long past the meeting's lifetime,
  // and ended_at decides live vs detail view.
  const openMeetingByID = useCallback((id: string) => {
    getMeeting(id)
      .then((m) => {
        setSelected(m)
        setView(m.ended_at ? "detail" : "live")
        if (!m.ended_at) setLive(m)
      })
      .catch(() => {
        // Unknown/deleted meeting: still land somewhere sensible.
        setView("list")
      })
  }, [])

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

  const openMeeting = (meeting: Meeting) => {
    setSelected(meeting)
    setView(meeting.ended_at ? "detail" : "live")
    if (!meeting.ended_at) setLive(meeting)
  }

  const handleStarted = (meeting: StartMeetingResponse) => {
    setCaptureError(meeting.capture_error ?? null)
    setCaptureErrorKind(meeting.capture_error_kind ?? null)
    openMeeting(meeting)
  }

  const goToList = useCallback(() => {
    setSelected(null)
    setView("list")
  }, [])

  const openLive = useCallback(() => {
    if (!live) return
    setSelected(live)
    setView("live")
  }, [live])

  const startMeeting = useCallback(() => setStartOpen(true), [])
  const openSettings = useCallback((section?: SettingsSection) => {
    setSettingsSection(section)
    setView("settings")
  }, [])

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
  const showCompactShell = liveCompact && view === "live" && selected && !selected.ended_at

  return (
    <div className={showCompactShell ? "bg-background text-foreground" : "flex min-h-svh text-foreground"}>
      {/* M23: update banner (self-mounted, no-op until "update:available"
          fires). Hidden in compact mode — a 480x260 overlay has no room. */}
      {!showCompactShell && <UpdateBanner />}

      {/* Compact mode always stays opaque (a small floating reading window,
          unaffected by M22 — Mica is out of its scope). The normal shell
          drops its own bg-background so html.desktop-mica's transparent
          body (index.css) can show Mica through here and the sidebar; a
          plain browser still gets an opaque page from body's own rule. */}
      {!showCompactShell && (
        <AppSidebar
          view={view}
          onNavigate={(v) => (v === "live" ? openLive() : setView(v))}
          liveTitle={live?.title ?? null}
          onStartMeeting={startMeeting}
          startButtonRef={startBtnRef}
        />
      )}

      {/* main: always opaque — content (meeting list, transcript, notes,
          settings) stays readable over Mica regardless of the shell. */}
      <main className={showCompactShell ? "" : "min-w-0 flex-1 bg-background"}>
        {!showCompactShell && view === "list" && <MeetingList onOpenMeeting={openMeeting} />}
        {view === "live" && selected && !selected.ended_at && (
          <LiveMeeting
            meeting={selected}
            captureError={captureError}
            onOpenSettings={
              captureError
                ? () => openSettings(captureErrorSection(captureErrorKind ?? "engine"))
                : undefined
            }
            onCompactChange={setLiveCompact}
            onEnded={(m) => {
              setLive(null)
              setCaptureError(null)
              setCaptureErrorKind(null)
              setSelected(m)
              setView("detail")
            }}
          />
        )}
        {!showCompactShell && view === "detail" && selected && (
          <MeetingDetail key={selected.id} meetingId={selected.id} initialMeeting={selected} onBack={goToList} />
        )}
        {!showCompactShell && view === "action-items" && <ActionItems onOpenMeeting={openMeeting} />}
        {!showCompactShell && view === "settings" && <Settings initialSection={settingsSection} />}
      </main>

      {!showCompactShell && (
        <StartMeetingPopover open={startOpen} onOpenChange={setStartOpen} onStarted={handleStarted} anchor={startBtnRef} />
      )}
    </div>
  )
}

export default App
