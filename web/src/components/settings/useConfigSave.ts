import { useCallback, useEffect, useRef, useState } from "react"

import type { SaveState } from "@/components/settings/pieces"
import { updateConfig, type AppConfig, type UpdateConfigRequest } from "@/lib/api"

/**
 * PUT /api/config sends the whole config, so every settings page keeps a
 * full AppConfig snapshot and patches only its own fields — the other
 * fields ride along unchanged (this is how the old single SettingsConfig
 * form worked; the API itself is unchanged in M20).
 */
export function toConfigRequest(
  c: AppConfig,
  patch: Partial<UpdateConfigRequest>,
): UpdateConfigRequest {
  return {
    engine: c.engine,
    whisper_url: c.whisper_url,
    soniox: { url: c.soniox.url },
    translate: {
      host: c.translate.host,
      live_model: c.translate.live_model,
      batch_model: c.translate.batch_model,
    },
    ...patch,
  }
}

/**
 * Quiet autosave for config-backed settings rows: last-write-wins with a
 * sequence guard so a stale response never clobbers a newer save's status.
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
      setSave({ state: "saving" })
      try {
        const c = await updateConfig(toConfigRequest(config, patch))
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
