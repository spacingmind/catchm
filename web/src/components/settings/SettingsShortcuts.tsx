import { useCallback, useEffect, useState, type ReactNode } from "react"

import { StatusMessage } from "@/components/StatusMessage"
import { SettingRow, SettingsSection, SaveStatus, descId } from "@/components/settings/pieces"
import { useSaveStatus } from "@/components/settings/useConfigSave"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { noAutofill } from "@/lib/inputProps"
import { getShortcuts, setShortcuts } from "@/lib/wailsRuntime"

// The in-app shortcut accepts Ctrl or ⌘ (useCompactMode); name the one
// this platform's keyboard actually has.
const MOD_KEY = /Mac|iPhone|iPad/.test(navigator.userAgent) ? "⌘" : "Ctrl"

/** Matches internal/hotkeys.Action string values exactly. */
const ACTIONS: { id: string; label: string; description: ReactNode }[] = [
  { id: "show_hide", label: "Show/hide CatchM", description: "Works even when another app is focused." },
  { id: "start_stop_meeting", label: "Start/stop meeting", description: "Starts a new meeting, or ends the active one." },
  {
    id: "toggle_compact",
    label: "Toggle compact mode",
    description: (
      <>
        Different from the in-app{" "}
        <kbd className="rounded border border-border bg-muted px-1 font-mono text-xs">{MOD_KEY}+Shift+L</kbd>, which only
        works while CatchM is focused.
      </>
    ),
  },
]

type Load = "loading" | "ready" | "error"

/** Backend messages may or may not end in a period; end with exactly one. */
const sentence = (s: string) => `${s.replace(/[.\s]+$/, "")}.`

/**
 * M22 §5: the global shortcuts. Desktop-only, so this page only
 * renders inside the Wails window (Settings.tsx hides the nav item
 * entirely in `catchm serve`, matching M21's hasWindowControls()
 * convention; this component assumes it's already gated and doesn't
 * re-check). Launch-at-login/notifications/updates used to live here
 * too — they moved to the System page (SettingsSystem.tsx) so neither
 * page outgrows the default 1180x760 window.
 *
 * `bindings` is what the backend last confirmed; `drafts` is what the
 * inputs show (controlled, so a normalized or rejected combo from
 * setShortcuts replaces the typed text). A draft commits on blur or
 * Enter; Escape reverts it.
 */
export function SettingsShortcuts() {
  const [bindings, setBindings] = useState<Record<string, string>>({})
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [load, setLoad] = useState<Load>("loading")
  const { save, run } = useSaveStatus()

  const fetchShortcuts = useCallback((isCancelled: () => boolean = () => false) => {
    getShortcuts()
      .then((shortcuts) => {
        if (isCancelled()) return
        setBindings(shortcuts.bindings)
        setDrafts(shortcuts.bindings)
        setErrors(shortcuts.errors)
        setLoad("ready")
      })
      .catch(() => {
        // Nothing is editable on a failed load: sending a partial set
        // from here would unbind every shortcut we couldn't read.
        if (!isCancelled()) setLoad("error")
      })
  }, [])

  useEffect(() => {
    let cancelled = false
    fetchShortcuts(() => cancelled)
    return () => {
      cancelled = true
    }
  }, [fetchShortcuts])

  const retryLoad = () => {
    setLoad("loading")
    fetchShortcuts()
  }

  const commit = (id: string) => {
    const value = (drafts[id] ?? "").trim()
    if (value === (bindings[id] ?? "")) {
      // Only whitespace changed (or nothing): show the saved value again.
      setDrafts((d) => ({ ...d, [id]: bindings[id] ?? "" }))
      return
    }
    // Always send the full confirmed set plus this one change, never
    // other rows' uncommitted drafts.
    const prevSaved = bindings
    void run(async () => {
      const result = await setShortcuts({ ...bindings, [id]: value })
      setBindings(result.bindings)
      setErrors(result.errors)
      // Replace this row and any row the user isn't mid-edit on (its
      // draft still matches the old saved value) with what the backend
      // actually registered.
      setDrafts((d) => {
        const next = { ...d }
        for (const [k, v] of Object.entries(result.bindings)) {
          if (k === id || (d[k] ?? "") === (prevSaved[k] ?? "")) next[k] = v
        }
        return next
      })
    }, "Couldn’t save that shortcut. Check that CatchM is still running, then try again.")
  }

  if (load !== "ready") {
    return (
      <div>
        <SettingsSection title="Shortcuts" description="Global keyboard shortcuts that work even when CatchM isn’t focused.">
          <StatusMessage tone={load === "error" ? "error" : "muted"}>
            {load === "loading"
              ? "Loading shortcuts…"
              : "Couldn’t load your shortcuts. Try again, or restart CatchM if it keeps failing."}
          </StatusMessage>
          {load === "error" && (
            <Button variant="outline" className="mt-3" onClick={retryLoad}>
              Try again
            </Button>
          )}
        </SettingsSection>
      </div>
    )
  }

  return (
    <div>
      <SettingsSection title="Shortcuts" description="Global keyboard shortcuts that work even when CatchM isn’t focused.">
        {ACTIONS.map(({ id, label, description }) => {
          const inputId = `shortcut-${id}`
          return (
            <SettingRow
              key={id}
              label={label}
              htmlFor={inputId}
              description={
                errors[id] ? (
                  <span className="text-destructive">{sentence(errors[id])} Try a different combination.</span>
                ) : (
                  description
                )
              }
              control={
                <Input
                  id={inputId}
                  name={inputId}
                  {...noAutofill}
                  className="w-40"
                  placeholder="Ctrl+Shift+H…"
                  value={drafts[id] ?? ""}
                  onChange={(e) => setDrafts((d) => ({ ...d, [id]: e.target.value }))}
                  aria-invalid={Boolean(errors[id])}
                  aria-describedby={descId(inputId)}
                  onBlur={() => commit(id)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      e.preventDefault()
                      commit(id)
                    } else if (e.key === "Escape") {
                      setDrafts((d) => ({ ...d, [id]: bindings[id] ?? "" }))
                    }
                  }}
                />
              }
            />
          )
        })}
        <div className="flex items-center justify-end">
          <SaveStatus state={save.state} error={save.error} />
        </div>
      </SettingsSection>
    </div>
  )
}
