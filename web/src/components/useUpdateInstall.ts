import { useCallback, useState } from "react"

import { applyUpdate, downloadUpdate } from "@/lib/wailsRuntime"

export type UpdatePhase = "idle" | "downloading" | "confirm" | "applying"

/**
 * M23's download → confirm → apply flow, shared by UpdateBanner and
 * the Settings Updates row's "Update now" button so there's exactly
 * one copy. "Apply" always means the user just confirmed: the app
 * quits and Windows asks for permission (UAC) — no surprise quit.
 *
 * download: fetch + SHA-256-verify the installer (checksum mismatch /
 * network failure surface as `error` and stop — deferred server-side
 * on mismatch, no retry loop here). apply: run the verified installer
 * elevated; on success the app quits and nothing after it runs long.
 */
export function useUpdateInstall() {
  const [phase, setPhase] = useState<UpdatePhase>("idle")
  const [error, setError] = useState<string | null>(null)

  const download = useCallback(async () => {
    setPhase("downloading")
    setError(null)
    try {
      await downloadUpdate()
      setPhase("confirm")
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setPhase("idle")
    }
  }, [])

  const apply = useCallback(async () => {
    setPhase("applying")
    try {
      await applyUpdate()
      // On success the app quits; nothing after this line runs long.
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setPhase("confirm")
    }
  }, [])

  const reset = useCallback(() => {
    setPhase("idle")
    setError(null)
  }, [])

  return { phase, error, download, apply, reset }
}
