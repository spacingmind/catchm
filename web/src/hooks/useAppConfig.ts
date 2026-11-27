import { useEffect, useState } from "react"

import { CONFIG_EVENT, getConfig, type AppConfig } from "@/lib/api"

export interface AppConfigState {
  /** null until loaded, or when the load failed. */
  config: AppConfig | null
  /** True once the first load answered (either way). */
  loaded: boolean
}

/**
 * The app config for views that don't own a settings form (App's
 * first-run check, Get started). Loaded once, then kept current from
 * every successful PUT /api/config (CONFIG_EVENT), so choosing a setup
 * anywhere shows up here at once.
 */
export function useAppConfig(): AppConfigState {
  const [state, setState] = useState<AppConfigState>({ config: null, loaded: false })

  useEffect(() => {
    let cancelled = false
    // A save that lands first is newer than this GET's answer.
    let saved = false
    getConfig()
      .then((c) => {
        if (!cancelled && !saved) setState({ config: c, loaded: true })
      })
      .catch(() => {
        if (!cancelled && !saved) setState({ config: null, loaded: true })
      })
    const onSaved = (e: Event) => {
      saved = true
      setState({ config: (e as CustomEvent<AppConfig>).detail, loaded: true })
    }
    window.addEventListener(CONFIG_EVENT, onSaved)
    return () => {
      cancelled = true
      window.removeEventListener(CONFIG_EVENT, onSaved)
    }
  }, [])

  return state
}
