import { useEffect, type ComponentType } from "react"
import {
  Cloud,
  Keyboard,
  Languages,
  Mic,
  Monitor,
  Palette,
  Settings2,
  Sparkles,
  Speech,
  type LucideIcon,
} from "lucide-react"
import type { ParseKeys } from "i18next"
import { useTranslation } from "react-i18next"
import { cn } from "cn"

import { SettingsAppearance } from "@/components/settings/SettingsAppearance"
import { SettingsCloudSync } from "@/components/settings/SettingsCloudSync"
import { SettingsShortcuts } from "@/components/settings/SettingsShortcuts"
import { SettingsSummaries } from "@/components/settings/SettingsSummaries"
import { SettingsSystem } from "@/components/settings/SettingsSystem"
import { SettingsTranscription } from "@/components/settings/SettingsTranscription"
import { SettingsTranslation } from "@/components/settings/SettingsTranslation"
import { SettingsRecording } from "@/components/settings/SettingsRecording"
import { Button } from "@/components/ui/button"
import { navigate, useRoute } from "@/lib/route"
import { hasWindowControls } from "@/lib/wailsRuntime"

interface SectionDef {
  /** URL segment: #/settings/{id}. */
  id: string
  labelKey: ParseKeys
  icon: LucideIcon
  Page: ComponentType
  desktopOnly?: boolean
}

// Adding a section is one entry here: the Section type, the nav, the
// route and the page all derive from it. The first shown is the default.
const ALL_SECTIONS = [
  { id: "recording", labelKey: "settings.nav.recording", icon: Mic, Page: SettingsRecording },
  { id: "transcription", labelKey: "settings.nav.transcription", icon: Speech, Page: SettingsTranscription },
  { id: "translation", labelKey: "settings.nav.translation", icon: Languages, Page: SettingsTranslation },
  { id: "summaries", labelKey: "settings.nav.summaries", icon: Sparkles, Page: SettingsSummaries },
  { id: "cloud", labelKey: "settings.nav.cloudSync", icon: Cloud, Page: SettingsCloudSync },
  { id: "appearance", labelKey: "settings.nav.appearance", icon: Palette, Page: SettingsAppearance },
  // M22 §5: global shortcuts/launch-at-login/notifications only mean
  // anything inside the real Wails window — hidden entirely in
  // `catchm serve`, same hasWindowControls() convention M21 set for
  // the compact-mode button.
  { id: "shortcuts", labelKey: "settings.nav.shortcuts", icon: Keyboard, Page: SettingsShortcuts, desktopOnly: true },
  // Launch-at-login/notifications/updates — the desktop-shell toggles
  // that used to make the Shortcuts page taller than the default
  // 1180x760 window. "System": nothing else claims the name, and it
  // covers OS-integration concerns (login item, toasts, updates).
  { id: "system", labelKey: "settings.nav.system", icon: Monitor, Page: SettingsSystem, desktopOnly: true },
] as const satisfies readonly SectionDef[]

export type Section = (typeof ALL_SECTIONS)[number]["id"]

const SECTIONS: readonly SectionDef[] = ALL_SECTIONS.filter((s) => !("desktopOnly" in s) || hasWindowControls())

export function Settings() {
  const { t } = useTranslation()
  const route = useRoute()
  const requested = route.name === "settings" ? route.section : null
  // An unknown or hidden section (e.g. #/settings/shortcuts in a plain
  // browser) shows the default, and the URL is corrected to match.
  const active = SECTIONS.find((s) => s.id === requested) ?? SECTIONS[0]

  useEffect(() => {
    if (route.name === "settings" && requested !== active.id) {
      navigate({ name: "settings", section: active.id }, { replace: true })
    }
  }, [route.name, requested, active.id])

  const { Page } = active

  return (
    <div className="mx-auto flex max-w-4xl gap-8 px-6 py-8">
      <nav aria-label={t("settings.nav.sections")} className="w-52 shrink-0">
        <h1 className="mb-4 flex items-center gap-2 text-2xl font-semibold">
          <Settings2 aria-hidden="true" className="size-6" /> {t("settings.nav.title")}
        </h1>
        <ul className="grid gap-1">
          {SECTIONS.map(({ id, labelKey, icon: Icon }) => (
            <li key={id}>
              <Button
                variant="ghost"
                onClick={() => navigate({ name: "settings", section: id })}
                aria-current={active.id === id ? "page" : undefined}
                className={cn(
                  "h-9 w-full justify-start gap-2.5 px-2.5",
                  active.id === id ? "bg-muted text-foreground" : "font-normal text-muted-foreground",
                )}
              >
                <Icon aria-hidden="true" className="size-4 shrink-0" />
                {t(labelKey)}
              </Button>
            </li>
          ))}
        </ul>
      </nav>

      <div className="min-w-0 flex-1">
        {/* key: each page keeps its own form state; switching sections
            starts the next one fresh, as the old per-section branches did. */}
        <Page key={active.id} />
      </div>
    </div>
  )
}
