import { useEffect, useState } from "react"
import { RefreshCw } from "lucide-react"

import { SettingRow, SettingsSection } from "@/components/settings/pieces"
import { Button } from "@/components/ui/button"
import { checkForUpdate, getAppVersion, getLastChecked, type UpdateStatus } from "@/lib/wailsRuntime"

/**
 * M23: current version, manual check, last-checked time. Rendered as a
 * section of the Shortcuts page (matching M20's page layout — no new
 * nav item) since both are desktop-shell concerns. When a check finds
 * a newer release the banner (UpdateBanner.tsx, mounted in App) is the
 * actionable surface; this page just reports status.
 */
export function SettingsAbout() {
  const [version] = useState(getAppVersion)
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [lastChecked, setLastChecked] = useState("")
  const [checking, setChecking] = useState(false)

  useEffect(() => {
    getLastChecked()
      .then(setLastChecked)
      .catch(() => {})
  }, [])

  const handleCheck = async () => {
    setChecking(true)
    try {
      const result = await checkForUpdate()
      setStatus(result)
      setLastChecked(result.checkedAt ?? new Date().toISOString())
    } catch (err) {
      setStatus({
        currentVersion: version,
        updateAvailable: false,
        error: err instanceof Error ? err.message : String(err),
      })
    } finally {
      setChecking(false)
    }
  }

  const checkedLabel = lastChecked
    ? `Last checked ${new Date(lastChecked).toLocaleString()}`
    : "Not checked yet this session"
  const detail = status?.error
    ? status.error
    : status?.updateAvailable
      ? `Version ${status.latestVersion} is available.`
      : status
        ? "You're up to date."
        : checkedLabel

  return (
    <SettingsSection title="Updates" description="CatchM checks for updates on startup and once a day while running.">
      <SettingRow
        label={`Current version: ${version}`}
        description={detail}
        control={
          <Button variant="outline" onClick={() => void handleCheck()} disabled={checking}>
            <RefreshCw className={checking ? "motion-safe:animate-spin" : undefined} />
            {checking ? "Checking…" : "Check for updates"}
          </Button>
        }
      />
    </SettingsSection>
  )
}
