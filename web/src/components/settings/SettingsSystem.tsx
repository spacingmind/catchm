import { useEffect, useState, type ReactNode } from "react"
import { RefreshCw } from "lucide-react"

import { StatusMessage } from "@/components/StatusMessage"
import { SettingRow, SettingsSection, SaveStatus, descId } from "@/components/settings/pieces"
import { useSaveStatus } from "@/components/settings/useConfigSave"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { useUpdateInstall } from "@/components/useUpdateInstall"
import { formatDateTime } from "@/lib/format"
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
/**
 * A checkbox setting with its label beside it, so the box and the text
 * form one hit target (SettingRow puts the control at the far edge,
 * leaving a dead zone between them). `checked` null means the value
 * couldn't be read: show why instead of a made-up default.
 */
function CheckboxRow({
  id,
  label,
  description,
  checked,
  onCheckedChange,
}: {
  id: string
  label: string
  description?: ReactNode
  checked: boolean | null
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <div className="border-b border-border py-4 last:border-b-0">
      {checked === null ? (
        <StatusMessage tone="error">
          Couldn’t read this setting. Restart CatchM, then try again.
        </StatusMessage>
      ) : (
        <div className="flex items-start gap-3">
          <Checkbox
            id={id}
            className="mt-0.5"
            checked={checked}
            onCheckedChange={(c) => onCheckedChange(c === true)}
            aria-describedby={description ? descId(id) : undefined}
          />
          <div className="grid min-w-0 gap-0.5">
            <label htmlFor={id} className="cursor-pointer text-sm font-medium">
              {label}
            </label>
            {description && (
              <p id={descId(id)} className="text-sm text-muted-foreground">
                {description}
              </p>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

export function SettingsSystem() {
  // undefined = still loading; null = the bridge call failed.
  const [version, setVersion] = useState<string | null | undefined>(undefined)
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [lastChecked, setLastChecked] = useState("")
  const [checking, setChecking] = useState(false)

  const [autostart, setAutostartState] = useState<boolean | null | undefined>(undefined)
  const [notifications, setNotificationsState] = useState<boolean | null | undefined>(undefined)
  // One status per section: a failed login-item write must not show up
  // (or be cleared) under Notifications, and vice versa.
  const autostartSave = useSaveStatus()
  const notificationsSave = useSaveStatus()

  const update = useUpdateInstall()

  useEffect(() => {
    let cancelled = false
    // getAppVersion crosses the JS→Go bridge (always a Promise — see
    // its wrapper): resolve it, never render it directly. Settled
    // individually so one failing call doesn't blank the others.
    void Promise.allSettled([getAppVersion(), getAutostart(), getNotificationsEnabled(), getLastChecked()]).then(
      ([v, auto, notif, checked]) => {
        if (cancelled) return
        setVersion(v.status === "fulfilled" ? v.value : null)
        setAutostartState(auto.status === "fulfilled" ? auto.value : null)
        setNotificationsState(notif.status === "fulfilled" ? notif.value : null)
        if (checked.status === "fulfilled") setLastChecked(checked.value)
      },
    )
    return () => {
      cancelled = true
    }
  }, [])

  const handleAutostart = (enabled: boolean) => {
    setAutostartState(enabled)
    void autostartSave
      .run(
        () => setAutostart(enabled),
        "Couldn’t change this setting. Try again, or restart CatchM if it keeps failing.",
      )
      .then((ok) => {
        if (!ok) setAutostartState(!enabled)
      })
  }

  const handleNotifications = (enabled: boolean) => {
    setNotificationsState(enabled)
    void notificationsSave
      .run(
        () => setNotificationsEnabled(enabled),
        "Couldn’t change notifications. Try again, or restart CatchM if it keeps failing.",
      )
      .then((ok) => {
        if (!ok) setNotificationsState(!enabled)
      })
  }

  const handleCheck = async () => {
    setChecking(true)
    try {
      const result = await checkForUpdate()
      setStatus(result)
      setLastChecked(result.checkedAt ?? new Date().toISOString())
    } catch (err) {
      setStatus({
        currentVersion: version ?? "",
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
  const latest = effectiveStatus?.latestVersion

  const checkedLabel = lastChecked ? `Last checked ${formatDateTime(lastChecked)}` : "Not checked yet this session"
  const updateError = update.error || status?.error
  const updateDetail = updateError
    ? updateError
    : update.phase === "downloading"
      ? latest
        ? `Downloading version ${latest}…`
        : "Downloading the update…"
      : update.phase === "cancelling"
        ? "Cancelling the download…"
        : update.phase === "applying"
          ? "Updating — CatchM will restart shortly."
          : effectiveStatus?.updateAvailable
            ? [update.notice, latest ? `Version ${latest} is available.` : "A new version is available."]
                .filter(Boolean)
                .join(" ")
            : status
              ? "You’re up to date."
              : checkedLabel

  const loadingPref = (
<p className="py-4 text-sm text-muted-foreground">Loading…</p>
  )

  return (
    <div>
      <SettingsSection title="Start automatically" description="CatchM starts in the background when you sign in to your computer.">
        {autostart === undefined ? (
          loadingPref
        ) : (
          <CheckboxRow
            id="autostart-toggle"
            label="Start CatchM when you sign in"
            checked={autostart}
            onCheckedChange={handleAutostart}
          />
        )}
        <div className="flex items-center justify-end">
          <SaveStatus state={autostartSave.save.state} error={autostartSave.save.error} />
        </div>
      </SettingsSection>

      <SettingsSection title="Notifications" description="Pop-up notifications while CatchM’s window is hidden.">
        {notifications === undefined ? (
          loadingPref
        ) : (
          <CheckboxRow
            id="notifications-toggle"
            label="Show notifications"
            description="For example, when recording stops working, a meeting ends, or cloud sync needs your attention."
            checked={notifications}
            onCheckedChange={handleNotifications}
          />
        )}
        <div className="flex items-center justify-end">
          <SaveStatus state={notificationsSave.save.state} error={notificationsSave.save.error} />
        </div>
      </SettingsSection>

      <SettingsSection title="Updates" description="CatchM checks for updates on startup and once a day while running.">
        <SettingRow
          label="Current version"
          id="update-status"
          description={
            <>
              <span translate="no" className="block font-medium tabular-nums text-foreground">
                {version === undefined ? "…" : version === null ? "Unknown" : version}
              </span>
              <span
                role="status"
                aria-live="polite"
                aria-atomic="true"
                className={updateError ? "block text-destructive" : "block"}
              >
                {updateDetail}
              </span>
            </>
          }
          control={
            <div className="flex shrink-0 items-center gap-2">
              {effectiveStatus?.updateAvailable && update.phase === "idle" && (
                <Button onClick={() => void update.download()}>Update now</Button>
              )}
              {(update.phase === "downloading" || update.phase === "cancelling") && (
                <Button
                  variant="outline"
                  disabled={update.phase === "cancelling"}
                  onClick={() => void update.cancel()}
                >
                  {update.phase === "cancelling" ? "Cancelling…" : "Cancel download"}
                </Button>
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
                disabled={checking || !(update.phase === "idle" || update.phase === "confirm")}
                aria-describedby={descId("update-status")}
              >
                <RefreshCw aria-hidden="true" className={checking ? "motion-safe:animate-spin" : undefined} />
                {checking ? "Checking…" : "Check for updates"}
              </Button>
            </div>
          }
        />
      </SettingsSection>
    </div>
  )
}
