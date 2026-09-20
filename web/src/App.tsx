import { useState } from "react"
import { CheckSquare, Settings2 } from "lucide-react"

import logo from "@/assets/logo.png"
import { ActionItems } from "@/components/ActionItems"
import { Button } from "@/components/ui/button"
import { LiveMeeting } from "@/components/LiveMeeting"
import { MeetingDetail } from "@/components/MeetingDetail"
import { MeetingList } from "@/components/MeetingList"
import { SettingsCloud } from "@/components/SettingsCloud"
import { SettingsConfig } from "@/components/SettingsConfig"
import { SettingsTranslate } from "@/components/SettingsTranslate"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import type { Meeting } from "@/lib/api"

type View = "list" | "live" | "detail" | "action-items" | "settings"

function App() {
  const [view, setView] = useState<View>("list")
  const [selected, setSelected] = useState<Meeting | null>(null)

  const openMeeting = (meeting: Meeting) => {
    setSelected(meeting)
    setView(meeting.ended_at ? "detail" : "live")
  }

  const goToList = () => {
    setSelected(null)
    setView("list")
  }

  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="flex items-center justify-between border-b border-border px-6 py-3">
        <button type="button" onClick={goToList} className="flex items-center gap-2 text-lg font-semibold">
          <img src={logo} alt="CatchM" className="size-6 rounded-md" /> CatchM
        </button>
        <div className="flex items-center gap-1">
          <Button variant="ghost" size="sm" onClick={() => setView("action-items")}>
            <CheckSquare /> Action items
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setView("settings")}>
            <Settings2 /> Settings
          </Button>
        </div>
      </header>

      <main>
        {view === "list" && <MeetingList onOpenMeeting={openMeeting} />}
        {view === "live" && selected && (
          <LiveMeeting
            meeting={selected}
            onEnded={(m) => {
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
    </div>
  )
}

export default App
