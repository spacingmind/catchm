import { useState } from "react"
import { useTranslation } from "react-i18next"

import { SaveStatus, SettingRow, descId } from "@/components/settings/pieces"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { Input } from "@/components/ui/input"
import type { AppConfig } from "@/lib/api"

/** Matches config.MaxProfileName. */
const MAX_NAME = 80

/**
 * "Your name": who the microphone is in a call. Used to label the
 * user's own lines (docs/plans/active/speaker-identity.md). Saved on
 * blur or Enter.
 */
export function ProfileNameRow({
  id,
  config,
  onConfig,
}: {
  id: string
  config: AppConfig
  onConfig: (c: AppConfig) => void
}) {
  const { t } = useTranslation()
  const { save, put } = useConfigSave(config, onConfig)
  const stored = config.profile?.name ?? ""
  const [name, setName] = useState(stored)
  // Follow a name saved elsewhere (adjusting state while rendering, as
  // React recommends over an effect).
  const [seen, setSeen] = useState(stored)
  if (seen !== stored) {
    setSeen(stored)
    setName(stored)
  }

  const commit = () => {
    const next = name.trim().replace(/\s+/g, " ")
    if (next === stored) return
    void put({ profile: { name: next } })
  }

  return (
    <SettingRow
      htmlFor={id}
      label={t("settings.profile.name")}
      description={t("settings.profile.nameDesc")}
      control={
        <div className="grid justify-items-end gap-1">
          <Input
            id={id}
            name="profile_name"
            autoComplete="name"
            maxLength={MAX_NAME}
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={commit}
            onKeyDown={(e) => {
              if (e.key === "Enter") commit()
            }}
            placeholder={t("settings.profile.namePlaceholder")}
            aria-describedby={descId(id)}
            className="w-64"
          />
          <SaveStatus state={save.state} error={save.error} />
        </div>
      }
    />
  )
}
