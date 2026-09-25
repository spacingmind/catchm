import { useCallback, useEffect, useRef, useState } from "react"

import { ActionItems } from "@/components/ActionItems"
import { AppSidebar, type View } from "@/components/AppSidebar"
import { LiveMeeting } from "@/components/LiveMeeting"
import { MeetingDetail } from "@/components/MeetingDetail"
import { MeetingList } from "@/components/MeetingList"
import { StartMeetingPopover } from "@/components/StartMeetingPopover"
import { Settings } from "@/components/settings/Settings"
import { listMeetings, type Meeting, type StartMeetingResponse } from "@/lib/api"

/** How often the live state is re-derived from the server. */
const LIVE_POLL_MS = 3000

function App() {
  const [view, setView] = useState<View>("list")
  const [selected, setSelected] = useState<Meeting | null>(null)
  // The meeting currently recording (at most one in the UI's model), kept
  // independently of `selected` so browsing past meetings doesn't lose the
  // sidebar's recording indicator.
  const [live, setLive] = useState<Meeting | null>(null)
  const [startOpen, setStartOpen] = useState(false)
  // Sidebar Start button — the popover anchors above it.
  const startBtnRef = useRef<HTMLButtonElement | null>(null)
  // capture_error from the start response (M19): the meeting exists but
  // recording didn't start. Shown as a live-view banner until ended.
  const [captureError, setCaptureError] = useState<string | null>(null)

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
  const openSettings = useCallback(() => setView("settings"), [])

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

  return (
    <div className="flex min-h-svh bg-background text-foreground">
      <AppSidebar
        view={view}
        onNavigate={(v) => (v === "live" ? openLive() : setView(v))}
        liveTitle={live?.title ?? null}
        onStartMeeting={startMeeting}
        startButtonRef={startBtnRef}
      />

      <main className="min-w-0 flex-1">
        {view === "list" && <MeetingList onOpenMeeting={openMeeting} />}
        {view === "live" && selected && !selected.ended_at && (
          <LiveMeeting
            meeting={selected}
            captureError={captureError}
            onOpenSettings={captureError ? openSettings : undefined}
            onEnded={(m) => {
              setLive(null)
              setCaptureError(null)
              setSelected(m)
              setView("detail")
            }}
          />
        )}
        {view === "detail" && selected && (
          <MeetingDetail key={selected.id} meetingId={selected.id} initialMeeting={selected} onBack={goToList} />
        )}
        {view === "action-items" && <ActionItems onOpenMeeting={openMeeting} />}
        {view === "settings" && <Settings />}
      </main>

      <StartMeetingPopover
        open={startOpen}
        onOpenChange={setStartOpen}
        onStarted={handleStarted}
        anchor={startBtnRef}
      />
    </div>
  )
}

export default App
