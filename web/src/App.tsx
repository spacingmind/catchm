import { useCallback, useEffect, useState } from "react"

import { ActionItems } from "@/components/ActionItems"
import { AppSidebar, type View } from "@/components/AppSidebar"
import { LiveMeeting } from "@/components/LiveMeeting"
import { MeetingDetail } from "@/components/MeetingDetail"
import { MeetingList } from "@/components/MeetingList"
import { NewMeetingDialog } from "@/components/NewMeetingDialog"
import { SettingsCloud } from "@/components/SettingsCloud"
import { SettingsConfig } from "@/components/SettingsConfig"
import { SettingsTranslate } from "@/components/SettingsTranslate"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import type { Meeting } from "@/lib/api"

function App() {
  const [view, setView] = useState<View>("list")
  const [selected, setSelected] = useState<Meeting | null>(null)
  // The meeting currently recording (at most one in the UI's model), kept
  // independently of `selected` so browsing past meetings doesn't lose the
  // sidebar's recording indicator.
  const [live, setLive] = useState<Meeting | null>(null)
  const [startOpen, setStartOpen] = useState(false)

  const openMeeting = (meeting: Meeting) => {
    setSelected(meeting)
    setView(meeting.ended_at ? "detail" : "live")
    if (!meeting.ended_at) setLive(meeting)
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
      />

      <main className="min-w-0 flex-1">
        {view === "list" && <MeetingList onOpenMeeting={openMeeting} />}
        {view === "live" && selected && !selected.ended_at && (
          <LiveMeeting
            meeting={selected}
            onEnded={(m) => {
              setLive(null)
              setSelected(m)
              setView("detail")
            }}
          />
        )}
        {view === "detail" && selected && (
          <MeetingDetail meetingId={selected.id} initialMeeting={selected} onBack={goToList} />
        )}
        {view === "action-items" && <ActionItems onOpenMeeting={openMeeting} />}
        {view === "settings" && (
          <Tabs defaultValue="engines">
            <TabsList className="mx-auto mb-6 flex w-fit px-6">
              <TabsTrigger value="engines">Engines</TabsTrigger>
              <TabsTrigger value="translation">Translation</TabsTrigger>
              <TabsTrigger value="cloud">Cloud</TabsTrigger>
            </TabsList>
            <TabsContent value="engines">
              <SettingsConfig />
            </TabsContent>
            <TabsContent value="translation">
              <SettingsTranslate />
            </TabsContent>
            <TabsContent value="cloud">
              <SettingsCloud />
            </TabsContent>
          </Tabs>
        )}
      </main>

      <NewMeetingDialog open={startOpen} onOpenChange={setStartOpen} onStarted={openMeeting} />
    </div>
  )
}

export default App
