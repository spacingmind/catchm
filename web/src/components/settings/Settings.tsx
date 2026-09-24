import { useState, type ComponentType } from "react"
import { Cloud, Languages, Mic, Settings2, Speech } from "lucide-react"
import { cn } from "cn"

import { SettingsCloud } from "@/components/SettingsCloud"
import { SettingsConfig } from "@/components/SettingsConfig"
import { SettingsTranslate } from "@/components/SettingsTranslate"
import { SettingsRecording } from "@/components/settings/SettingsRecording"

type Section = "recording" | "transcription" | "translation" | "cloud"

const SECTIONS: { id: Section; label: string; icon: ComponentType<{ className?: string }> }[] = [
  { id: "recording", label: "Recording", icon: Mic },
  { id: "transcription", label: "Transcription", icon: Speech },
  { id: "translation", label: "Translation", icon: Languages },
  { id: "cloud", label: "Cloud sync", icon: Cloud },
]

export function Settings() {
  const [section, setSection] = useState<Section>("recording")

  return (
    <div className="mx-auto flex max-w-4xl gap-8 px-6 py-8">
      <nav aria-label="Settings sections" className="w-52 shrink-0">
        <h1 className="mb-4 flex items-center gap-2 text-2xl font-semibold">
          <Settings2 className="size-6" /> Settings
        </h1>
        <ul className="grid gap-1">
          {SECTIONS.map(({ id, label, icon: Icon }) => (
            <li key={id}>
              <button
                type="button"
                onClick={() => setSection(id)}
                aria-current={section === id ? "true" : undefined}
                className={cn(
                  "flex h-9 w-full items-center gap-2.5 rounded-md px-2.5 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
                  section === id
                    ? "bg-muted font-medium text-foreground"
                    : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
                )}
              >
                <Icon className="size-4 shrink-0" />
                {label}
              </button>
            </li>
          ))}
        </ul>
      </nav>

      <div className="min-w-0 flex-1">
        {section === "recording" && <SettingsRecording />}
        {section === "transcription" && <SettingsConfig />}
        {section === "translation" && <SettingsTranslate />}
        {section === "cloud" && <SettingsCloud />}
      </div>
    </div>
  )
}
