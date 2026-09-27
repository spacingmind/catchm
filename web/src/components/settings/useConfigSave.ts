import { useCallback, useEffect, useRef, useState } from "react"

import type { SaveState } from "@/components/settings/pieces"
import { updateConfig, type AppConfig, type UpdateConfigRequest } from "@/lib/api"

/**
 * Quiet autosave for config-backed settings rows. PUT /api/config is a
 * partial update (setIfNonEmpty per field), so only the patched fields
 * are sent — never a full snapshot, which could silently overwrite
 * newer values saved from another window/page. The full AppConfig is
 * kept for rendering only. Sequence-guarded so a stale response never
 * clobbers a newer save's status.
 */
export function useConfigSave(config: AppConfig | null, onSaved: (c: AppConfig) => void) {
  const [save, setSave] = useState<{ state: SaveState; error?: string }>({ state: "idle" })
  const seq = useRef(0)
  const timer = useRef<number | null>(null)

  useEffect(
    () => () => {
      if (timer.current !== null) window.clearTimeout(timer.current)
    },
    [],
  )

  const put = useCallback(
    async (patch: Partial<UpdateConfigRequest>) => {
      if (!config) return false
      const id = ++seq.current
      // A previous save's "Saved → idle" timer mustn't wipe this one's "Saving…".
      if (timer.current !== null) window.clearTimeout(timer.current)
      setSave({ state: "saving" })
      try {
        const c = await updateConfig(patch as UpdateConfigRequest)
        if (id !== seq.current) return true
        onSaved(c)
        setSave({ state: "saved" })
        if (timer.current !== null) window.clearTimeout(timer.current)
        timer.current = window.setTimeout(() => setSave({ state: "idle" }), 2000)
        return true
      } catch (err) {
        if (id !== seq.current) return false
        setSave({ state: "error", error: err instanceof Error ? err.message : String(err) })
        return false
      }
    },
    [config, onSaved],
  )

  return { save, put }
}

/**
 * The same save-status bookkeeping for settings that don't go through
 * /api/config (desktop-bridge calls, localStorage prefs). One instance
 * per independently-saved section, so a failure in one never shows up
 * under another. `run` wraps async work ("Saving…" → "Saved" fading to
 * idle, or "error" with `errorMessage` — falling back to the thrown
 * message); `flashSaved` is for synchronous saves. Sequence-guarded
 * like useConfigSave.
 */
export function useSaveStatus() {
  const [save, setSave] = useState<{ state: SaveState; error?: string }>({ state: "idle" })
  const seq = useRef(0)
  const timer = useRef<number | null>(null)

  useEffect(
    () => () => {
      if (timer.current !== null) window.clearTimeout(timer.current)
    },
    [],
  )

  const markSaved = useCallback(() => {
    setSave({ state: "saved" })
    if (timer.current !== null) window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setSave({ state: "idle" }), 2000)
  }, [])

  const flashSaved = useCallback(() => {
    ++seq.current
    markSaved()
  }, [markSaved])

  const run = useCallback(
    async (work: () => Promise<unknown>, errorMessage?: string) => {
      const id = ++seq.current
      if (timer.current !== null) window.clearTimeout(timer.current)
      setSave({ state: "saving" })
      try {
        await work()
        if (id === seq.current) markSaved()
        return true
      } catch (err) {
        if (id === seq.current) {
          setSave({ state: "error", error: errorMessage ?? (err instanceof Error ? err.message : String(err)) })
        }
        return false
      }
    },
    [markSaved],
  )

  return { save, run, flashSaved }
}
