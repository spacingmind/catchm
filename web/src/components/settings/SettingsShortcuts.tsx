import { useEffect, useRef, useState } from "react"

import { SettingRow, SettingsSection, SaveStatus, type SaveState } from "@/components/settings/pieces"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import {
  getAutostart,
  getNotificationsEnabled,
  getShortcuts,
  setAutostart,
  setNotificationsEnabled,
  setShortcuts,
  type ShortcutsInfo,
} from "@/lib/wailsRuntime"

/** Matches internal/hotkeys.Action string values exactly. */
const ACTIONS: { id: string; label: string; description: string }[] = [
  { id: "show_hide", label: "Show/hide CatchM", description: "Works even when another app is focused." },
  { id: "start_stop_meeting", label: "Start/stop meeting", description: "Starts a new meeting, or ends the active one." },
  { id: "toggle_compact", label: "Toggle compact mode", description: "Different from the in-app Ctrl/⌘+Shift+L, which only works while CatchM is focused." },
]

/**
 * M22 §5: global shortcuts, launch at login, and the toast-notification
 * opt-out — all desktop-only, so this page only renders inside the
 * Wails window (App.tsx/Settings.tsx hide the nav item entirely in
 * `catchm serve`, matching M21's hasWindowControls() convention; this
 * component assumes it's already gated and doesn't re-check).
 */
export function SettingsShortcuts() {
  const [bindings, setBindings] = useState<Record<string, string>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [loaded, setLoaded] = useState(false)
  const [saveState, setSaveState] = useState<SaveState>("idle")
  const saveTimer = useRef<number | null>(null)

  const [autostart, setAutostartState] = useState(false)
  const [notifications, setNotificationsState] = useState(true)

  useEffect(() => {
    let cancelled = false
    Promise.all([getShortcuts(), getAutostart(), getNotificationsEnabled()])
      .then(([shortcuts, auto, notif]) => {
        if (cancelled) return
        setBindings(shortcuts.bindings)
        setErrors(shortcuts.errors)
        setAutostartState(auto)
        setNotificationsState(notif)
        setLoaded(true)
      })
      .catch(() => {
        if (!cancelled) setLoaded(true)
      })
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

  const applyShortcuts = async (next: Record<string, string>) => {
    setBindings(next)
    setSaveState("saving")
    try {
      const result: ShortcutsInfo = await setShortcuts(next)
      setBindings(result.bindings)
      setErrors(result.errors)
      flashSaved()
    } catch {
      setSaveState("error")
    }
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

  if (!loaded) return null

  return (
    <div>
      <SettingsSection title="Shortcuts" description="Global keyboard shortcuts that work even when CatchM isn't focused.">
        {ACTIONS.map(({ id, label, description }) => (
          <SettingRow
            key={id}
            label={label}
            htmlFor={`shortcut-${id}`}
            description={errors[id] ? <span className="text-destructive">{errors[id]}</span> : description}
            control={
              <Input
                id={`shortcut-${id}`}
                className="w-40"
                defaultValue={bindings[id] ?? ""}
                aria-invalid={Boolean(errors[id])}
                onBlur={(e) => {
                  const value = e.target.value.trim()
                  if (value !== (bindings[id] ?? "")) void applyShortcuts({ ...bindings, [id]: value })
                }}
              />
            }
          />
        ))}
        <div className="flex items-center justify-end">
          <SaveStatus state={saveState} error={saveState === "error" ? "Couldn't save that shortcut." : null} />
        </div>
      </SettingsSection>

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
    </div>
  )
}
