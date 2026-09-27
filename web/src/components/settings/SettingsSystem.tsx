import { useEffect, useRef, useState } from "react"
import { RefreshCw } from "lucide-react"

import { SettingRow, SettingsSection, SaveStatus, type SaveState } from "@/components/settings/pieces"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { useUpdateInstall } from "@/components/useUpdateInstall"
import {
  checkForUpdate,
  getAutostart,
  getAppVersion,
  getLastChecked,
  getNotificationsEnabled,
  setAutostart,
  setNotificationsEnabled,
  type UpdateStatus,
} from "@/lib/wailsRuntime"

/**
 * M22 §5 + M23: launch-at-login, notifications, and updates — the
 * desktop-shell concerns that don't fit the Shortcuts page anymore at
 * the default 1180x760 window. Desktop-only: Settings.tsx hides the
 * nav item entirely in `catchm serve`, matching the hasWindowControls()
 * convention (this component assumes it's already gated and doesn't
 * re-check). When a check finds a newer release this page offers the
 * same download → confirm → apply flow as the banner (useUpdateInstall
 * — one shared store: both surfaces read the same phase/error/status,
 * and download/apply are no-ops while one is in flight), via the
 * "Update now" button.
 */
export function SettingsSystem() {
  const [version, setVersion] = useState("")
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [lastChecked, setLastChecked] = useState("")
  const [checking, setChecking] = useState(false)

  const [autostart, setAutostartState] = useState(false)
  const [notifications, setNotificationsState] = useState(true)
  const [saveState, setSaveState] = useState<SaveState>("idle")
  const saveTimer = useRef<number | null>(null)

  const update = useUpdateInstall()

  useEffect(() => {
    let cancelled = false
    // getAppVersion crosses the JS→Go bridge (always a Promise — see
    // its wrapper): resolve it, never render it directly.
    Promise.all([getAppVersion(), getAutostart(), getNotificationsEnabled(), getLastChecked()])
      .then(([v, auto, notif, checked]) => {
        if (cancelled) return
        setVersion(v)
        setAutostartState(auto)
        setNotificationsState(notif)
        setLastChecked(checked)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(
    () => () => {
      if (saveTimer.current !== null) window.clearTimeout(saveTimer.current)
    },
    [],
  )

  const flashSaved = () => {
    setSaveState("saved")
    if (saveTimer.current !== null) window.clearTimeout(saveTimer.current)
    saveTimer.current = window.setTimeout(() => setSaveState("idle"), 2000)
  }

  const handleAutostart = async (enabled: boolean) => {
    setAutostartState(enabled)
    try {
      await setAutostart(enabled)
      flashSaved()
    } catch {
      setSaveState("error")
      setAutostartState(!enabled)
    }
  }

  const handleNotifications = async (enabled: boolean) => {
    setNotificationsState(enabled)
    try {
      await setNotificationsEnabled(enabled)
      flashSaved()
    } catch {
      setSaveState("error")
      setNotificationsState(!enabled)
    }
  }

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

  // The manual check's result is fresher than the shared store's
  // event-derived status; when no check has run here yet, fall back to
  // the shared one so this page agrees with the banner (a background
  // check may already have found an update).
  const effectiveStatus = status ?? update.status

  const checkedLabel = lastChecked
    ? `Last checked ${new Date(lastChecked).toLocaleString()}`
    : "Not checked yet this session"
  const updateDetail = update.error
    ? update.error
    : status?.error
      ? status.error
      : update.phase === "downloading"
        ? `Downloading version ${effectiveStatus?.latestVersion}…`
        : update.phase === "applying"
          ? "Updating — CatchM will restart shortly."
          : effectiveStatus?.updateAvailable
            ? `Version ${effectiveStatus.latestVersion} is available.`
            : status
              ? "You're up to date."
              : checkedLabel

  return (
    <div>
      <SettingsSection title="Launch at login" description="Opens hidden to the tray on sign-in.">
        <SettingRow
          label="Launch CatchM at login"
          htmlFor="autostart-toggle"
          control={
            <Checkbox
              id="autostart-toggle"
              checked={autostart}
              onCheckedChange={(checked) => void handleAutostart(checked === true)}
            />
          }
        />
        <div className="flex items-center justify-end">
          <SaveStatus state={saveState} error={saveState === "error" ? "Couldn't save." : null} />
        </div>
      </SettingsSection>

      <SettingsSection title="Notifications" description="Toasts for events while CatchM's window is hidden.">
        <SettingRow
          label="Show notifications"
          htmlFor="notifications-toggle"
          description="Capture failing, a meeting ending, or a cloud sync conflict."
          control={
            <Checkbox
              id="notifications-toggle"
              checked={notifications}
              onCheckedChange={(checked) => void handleNotifications(checked === true)}
            />
          }
        />
      </SettingsSection>

      <SettingsSection title="Updates" description="CatchM checks for updates on startup and once a day while running.">
        <SettingRow
          label={`Current version: ${version || "…"}`}
          description={updateDetail}
          control={
            <div className="flex shrink-0 items-center gap-2">
              {effectiveStatus?.updateAvailable && update.phase === "idle" && (
                <Button onClick={() => void update.download()}>Update now</Button>
              )}
              {effectiveStatus?.updateAvailable && update.phase === "confirm" && (
                <>
                  <Button variant="outline" onClick={() => update.reset()}>
                    Not now
                  </Button>
                  <Button onClick={() => void update.apply()}>Install update</Button>
                </>
              )}
              <Button
                variant="outline"
                onClick={() => void handleCheck()}
                disabled={checking || update.phase === "downloading" || update.phase === "applying"}
              >
                <RefreshCw className={checking ? "motion-safe:animate-spin" : undefined} />
                {checking ? "Checking…" : "Check for updates"}
              </Button>
            </div>
          }
        />
      </SettingsSection>
    </div>
  )
}
