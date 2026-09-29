import { useEffect, useState, type ReactNode } from "react"
import { RefreshCw } from "lucide-react"
import { useTranslation } from "react-i18next"

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
  const { t } = useTranslation()
  return (
    <div className="border-b border-border py-4 last:border-b-0">
      {checked === null ? (
        <StatusMessage tone="error">{t("settings.system.cantReadSetting")}</StatusMessage>
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
  const { t } = useTranslation()
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
      .run(() => setAutostart(enabled), t("settings.system.settingSaveError"))
      .then((ok) => {
        if (!ok) setAutostartState(!enabled)
      })
  }

  const handleNotifications = (enabled: boolean) => {
    setNotificationsState(enabled)
    void notificationsSave
      .run(() => setNotificationsEnabled(enabled), t("settings.system.notificationsSaveError"))
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

  const checkedLabel = lastChecked
    ? t("settings.system.lastChecked", { date: formatDateTime(lastChecked) })
    : t("settings.system.notCheckedYet")
  const updateError = update.error || status?.error
  const updateDetail = updateError
    ? updateError
    : update.phase === "downloading"
      ? latest
        ? t("updateBanner.downloading", { version: latest })
        : t("settings.system.downloading")
      : update.phase === "cancelling"
        ? t("updateBanner.cancelling")
        : update.phase === "applying"
          ? t("settings.system.applying")
          : effectiveStatus?.updateAvailable
            ? [update.notice, latest ? t("updateBanner.available", { version: latest }) : t("settings.system.newVersionAvailable")]
                .filter(Boolean)
                .join(" ")
            : status
              ? t("settings.system.upToDate")
              : checkedLabel

  const loadingPref = <p className="py-4 text-sm text-muted-foreground">{t("settings.system.loading")}</p>

  return (
    <div>
      <SettingsSection title={t("settings.system.autostartTitle")} description={t("settings.system.autostartDesc")}>
        {autostart === undefined ? (
          loadingPref
        ) : (
          <CheckboxRow
            id="autostart-toggle"
            label={t("settings.system.autostartLabel")}
            checked={autostart}
            onCheckedChange={handleAutostart}
          />
        )}
        <div className="flex items-center justify-end">
          <SaveStatus state={autostartSave.save.state} error={autostartSave.save.error} />
        </div>
      </SettingsSection>

      <SettingsSection title={t("settings.system.notificationsTitle")} description={t("settings.system.notificationsDesc")}>
        {notifications === undefined ? (
          loadingPref
        ) : (
          <CheckboxRow
            id="notifications-toggle"
            label={t("settings.system.notificationsLabel")}
            description={t("settings.system.notificationsExamples")}
            checked={notifications}
            onCheckedChange={handleNotifications}
          />
        )}
        <div className="flex items-center justify-end">
          <SaveStatus state={notificationsSave.save.state} error={notificationsSave.save.error} />
        </div>
      </SettingsSection>

      <SettingsSection title={t("settings.system.updatesTitle")} description={t("settings.system.updatesDesc")}>
        <SettingRow
          label={t("settings.system.currentVersion")}
          id="update-status"
          description={
            <>
              <span translate="no" className="block font-medium tabular-nums text-foreground">
                {version === undefined ? "…" : version === null ? t("settings.system.unknownVersion") : version}
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
                <Button onClick={() => void update.download()}>{t("settings.system.updateNow")}</Button>
              )}
              {(update.phase === "downloading" || update.phase === "cancelling") && (
                <Button
                  variant="outline"
                  disabled={update.phase === "cancelling"}
                  onClick={() => void update.cancel()}
                >
                  {update.phase === "cancelling" ? t("updateBanner.cancellingShort") : t("updateBanner.cancelDownload")}
                </Button>
              )}
              {effectiveStatus?.updateAvailable && update.phase === "confirm" && (
                <>
                  <Button variant="outline" onClick={() => update.reset()}>
                    {t("updateBanner.notNow")}
                  </Button>
                  <Button onClick={() => void update.apply()}>{t("settings.system.installUpdate")}</Button>
                </>
              )}
              <Button
                variant="outline"
                onClick={() => void handleCheck()}
                disabled={checking || !(update.phase === "idle" || update.phase === "confirm")}
                aria-describedby={descId("update-status")}
              >
                <RefreshCw aria-hidden="true" className={checking ? "motion-safe:animate-spin" : undefined} />
                {checking ? t("settings.system.checking") : t("settings.system.checkForUpdates")}
              </Button>
            </div>
          }
        />
      </SettingsSection>
    </div>
  )
}
