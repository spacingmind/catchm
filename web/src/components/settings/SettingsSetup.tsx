import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { StatusMessage } from "@/components/StatusMessage"
import { SettingsSection } from "@/components/settings/pieces"
import { SetupBuilder } from "@/components/settings/SetupBuilder"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { getConfig, type AppConfig } from "@/lib/api"

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

/** Settings → Setup: the preset and the provider for each module. */
export function SettingsSetup() {
  const { t } = useTranslation()
  const [config, setConfig] = useState<AppConfig | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  const load = useCallback(() => {
    getConfig()
      .then(setConfig)
      .catch((err: unknown) => setLoadError(errorMessage(err)))
  }, [])

  useEffect(load, [load])

  if (loadError) {
    return (
      <SettingsSection title={t("settings.nav.setup")} description={t("setup.pageDesc")}>
        <div className="grid justify-items-start gap-3">
          <StatusMessage tone="error">{loadError}</StatusMessage>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setLoadError(null)
              load()
            }}
          >
            {t("common.tryAgain")}
          </Button>
        </div>
      </SettingsSection>
    )
  }

  if (!config) {
    return (
      <div aria-busy="true" className="grid gap-4">
        <span className="sr-only">{t("getStarted.checking")}</span>
        <Skeleton className="h-7 w-40" />
        <Skeleton className="h-16 w-full rounded-xl" />
        <Skeleton className="h-28 w-full rounded-xl" />
        <Skeleton className="h-28 w-full rounded-xl" />
      </div>
    )
  }

  return (
    <SettingsSection title={t("settings.nav.setup")} description={t("setup.pageDesc")}>
      <SetupBuilder config={config} onSaved={setConfig} />
    </SettingsSection>
  )
}
