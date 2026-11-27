import { useState } from "react"
import { useTranslation } from "react-i18next"

import { SaveStatus, SettingRow, descId } from "@/components/settings/pieces"
import { useConfigSave } from "@/components/settings/useConfigSave"
import { Input } from "@/components/ui/input"
import type { AppConfig, UpdateConfigRequest } from "@/lib/api"

/** Matches config.MaxProfileName. */
const MAX_NAME = 80

const splitList = (s: string) =>
  s
    .split(/[,;\n]/)
    .map((x) => x.trim().replace(/\s+/g, " "))
    .filter(Boolean)

/**
 * A profile text field saved on blur or Enter; follows a value saved
 * elsewhere (adjusting state while rendering, as React recommends over
 * an effect).
 */
function ProfileField({
  id,
  label,
  description,
  placeholder,
  stored,
  autoComplete,
  type,
  toPatch,
  config,
  onConfig,
}: {
  id: string
  label: string
  description: string
  placeholder: string
  stored: string
  autoComplete?: string
  type?: string
  toPatch: (value: string) => UpdateConfigRequest["profile"]
  config: AppConfig
  onConfig: (c: AppConfig) => void
}) {
  const { save, put } = useConfigSave(config, onConfig)
  const [value, setValue] = useState(stored)
  const [seen, setSeen] = useState(stored)
  if (seen !== stored) {
    setSeen(stored)
    setValue(stored)
  }

  const commit = () => {
    if (value.trim() === stored) return
    void put({ profile: toPatch(value) })
  }

  return (
    <SettingRow
      htmlFor={id}
      label={label}
      description={description}
      control={
        <div className="grid justify-items-end gap-1">
          <Input
            id={id}
            type={type}
            autoComplete={autoComplete}
            maxLength={type === "email" ? 400 : MAX_NAME * 4}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            onBlur={commit}
            onKeyDown={(e) => {
              if (e.key === "Enter") commit()
            }}
            placeholder={placeholder}
            aria-describedby={descId(id)}
            className="w-64"
          />
          <SaveStatus state={save.state} error={save.error} />
        </div>
      }
    />
  )
}

/**
 * "Your name": who the microphone is in a call, used to label the
 * user's own lines (docs/plans/active/speaker-identity.md). With `full`,
 * also the user's emails (to find them in a pasted guest list) and what
 * people call them (so their tasks are theirs).
 */
export function ProfileNameRow({
  id,
  config,
  onConfig,
  full,
}: {
  id: string
  config: AppConfig
  onConfig: (c: AppConfig) => void
  full?: boolean
}) {
  const { t } = useTranslation()
  const profile = config.profile ?? { name: "", emails: [], aliases: [] }
  return (
    <>
      <ProfileField
        id={id}
        label={t("settings.profile.name")}
        description={t("settings.profile.nameDesc")}
        placeholder={t("settings.profile.namePlaceholder")}
        stored={profile.name ?? ""}
        autoComplete="name"
        toPatch={(v) => ({ name: v.trim().replace(/\s+/g, " ") })}
        config={config}
        onConfig={onConfig}
      />
      {full && (
        <>
          <ProfileField
            id={`${id}-aliases`}
            label={t("settings.profile.aliases")}
            description={t("settings.profile.aliasesDesc")}
            placeholder={t("settings.profile.aliasesPlaceholder")}
            stored={(profile.aliases ?? []).join(", ")}
            autoComplete="off"
            toPatch={(v) => ({ aliases: splitList(v) })}
            config={config}
            onConfig={onConfig}
          />
          <ProfileField
            id={`${id}-emails`}
            label={t("settings.profile.emails")}
            description={t("settings.profile.emailsDesc")}
            placeholder="you@work.com, you@gmail.com"
            stored={(profile.emails ?? []).join(", ")}
            autoComplete="email"
            toPatch={(v) => ({ emails: splitList(v).map((e) => e.toLowerCase()) })}
            config={config}
            onConfig={onConfig}
          />
        </>
      )}
    </>
  )
}
