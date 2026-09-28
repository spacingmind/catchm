import { useCallback, useEffect, useState, type KeyboardEvent, type ReactNode } from "react"

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
    label: "Show/hide mini window",
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

/** Named keys internal/hotkeys accepts, by KeyboardEvent.code, in its canonical spelling. */
const NAMED_KEYS: Record<string, string> = {
  Space: "Space",
  Enter: "Return",
  NumpadEnter: "Return",
  Escape: "Escape",
  Delete: "Delete",
  Tab: "Tab",
  ArrowLeft: "Left",
  ArrowRight: "Right",
  ArrowUp: "Up",
  ArrowDown: "Down",
}

const MODIFIER_KEYS = new Set(["Control", "Alt", "Shift", "Meta", "OS", "AltGraph"])

/**
 * The key half of a combo, spelled exactly as internal/hotkeys'
 * normalizeKey outputs it (A–Z, 0–9, F1–F20, or a NAMED_KEYS value), or
 * null for a key it doesn't support. Uses the physical key (code), not
 * the typed character, so Shift/Alt can't turn "L" into "¬" or "1" into "!".
 */
function comboKey(e: KeyboardEvent): string | null {
  const letter = /^Key([A-Z])$/.exec(e.code)
  if (letter) return letter[1]
  const digit = /^Digit([0-9])$/.exec(e.code)
  if (digit) return digit[1]
  if (/^F([1-9]|1[0-9]|20)$/.test(e.code)) return e.code
  return NAMED_KEYS[e.code] ?? null
}

/** Modifiers in internal/hotkeys' canonical Combo.String() order: Ctrl, Alt, Shift, Win. */
function comboMods(e: KeyboardEvent): string[] {
  return [e.ctrlKey && "Ctrl", e.altKey && "Alt", e.shiftKey && "Shift", e.metaKey && "Win"].filter(
    (m): m is string => Boolean(m),
  )
}

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
 * setShortcuts replaces the recorded text). Each input records key
 * presses: pressing a combination fills in its "Ctrl+Alt+M" string, a
 * plain Enter (or leaving the field) commits it, and a plain Escape
 * reverts it. Typing or pasting still works as a fallback.
 */
export function SettingsShortcuts() {
  const [bindings, setBindings] = useState<Record<string, string>>({})
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [errors, setErrors] = useState<Record<string, string>>({})
  // Local, not-yet-sent problems with a key press (no modifier, a key
  // the backend can't register); cleared by the next good press.
  const [pressHints, setPressHints] = useState<Record<string, string>>({})
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
    }, "Couldn’t save that shortcut. Try again, or restart CatchM if it keeps failing.")
  }

  const setPressHint = (id: string, hint: string | null) =>
    setPressHints((h) => {
      const next = { ...h }
      if (hint) next[id] = hint
      else delete next[id]
      return next
    })

  const handleKeyDown = (id: string, e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing || MODIFIER_KEYS.has(e.key)) return
    const mods = comboMods(e)
    if (mods.length === 0) {
      if (e.key === "Enter") {
        e.preventDefault()
        setPressHint(id, null)
        commit(id)
        return
      }
      if (e.key === "Escape") {
        setPressHint(id, null)
        setDrafts((d) => ({ ...d, [id]: bindings[id] ?? "" }))
        return
      }
      // Plain Tab still moves focus, so keyboard users can leave the field.
      if (e.key === "Tab") return
    }
    // Everything else is a combination attempt: don't let it type into
    // the field or trigger the in-app shortcuts (e.g. compact mode).
    e.preventDefault()
    e.stopPropagation()
    const key = comboKey(e)
    if (!key) {
      setPressHint(id, "That key can’t be used. Try a letter, a number, or F1–F20.")
    } else if (mods.length === 0) {
      setPressHint(id, "Hold Ctrl, Alt, Shift, or Win while you press the key.")
    } else {
      setPressHint(id, null)
      setDrafts((d) => ({ ...d, [id]: [...mods, key].join("+") }))
    }
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
                pressHints[id] ? (
                  <span className="text-destructive">{pressHints[id]}</span>
                ) : errors[id] ? (
                  <span className="text-destructive">{sentence(errors[id])} Try a different combination.</span>
                ) : (
                  <>
                    {description} Click, then press the keys.
                  </>
                )
              }
              control={
                <Input
                  id={inputId}
                  name={inputId}
                  {...noAutofill}
                  className="w-40"
                  placeholder="Press keys…"
                  value={drafts[id] ?? ""}
                  onChange={(e) => setDrafts((d) => ({ ...d, [id]: e.target.value }))}
                  aria-invalid={Boolean(errors[id] || pressHints[id])}
                  aria-describedby={descId(inputId)}
                  onBlur={() => {
                    setPressHint(id, null)
                    commit(id)
                  }}
                  onKeyDown={(e) => handleKeyDown(id, e)}
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
