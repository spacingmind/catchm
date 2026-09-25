import { useEffect, useState } from "react"
import { ExternalLink } from "lucide-react"

import { Button } from "@/components/ui/button"
import { applyUpdate, deferUpdate, downloadUpdate, windowEventsOn, type UpdateStatus } from "@/lib/wailsRuntime"

type Phase = "idle" | "downloading" | "confirm" | "applying"

/**
 * M23: unobtrusive banner shown when a check (background or manual)
 * finds a newer release — the Go side emits "update:available" with the
 * same UpdateStatus shape CheckForUpdate returns. Visual treatment
 * mirrors LiveMeeting's capture-error banner (flex-wrap rounded-lg
 * border row with a trailing button cluster) rather than a new
 * component family. "Update now" always confirms first: the app will
 * quit and Windows will ask for permission (UAC) — no surprise quit.
 * Dismissing calls DeferUpdate: no further prompting this session.
 */
export function UpdateBanner() {
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [phase, setPhase] = useState<Phase>("idle")
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const cancel = windowEventsOn("update:available", (data) => {
      const s = Array.isArray(data) ? data[0] : data
      if (s && typeof s === "object") {
        setStatus(s as UpdateStatus)
        setPhase("idle")
        setError(null)
      }
    })
    return cancel
  }, [])

  if (!status) return null

  const dismiss = () => {
    setStatus(null)
    deferUpdate()
  }

  const startDownload = async () => {
    setPhase("downloading")
    setError(null)
    try {
      await downloadUpdate()
      setPhase("confirm")
    } catch (err) {
      // Checksum mismatch / network failure: surface it and stop
      // (deferred server-side on mismatch; no retry loop here).
      setError(err instanceof Error ? err.message : String(err))
      setPhase("idle")
    }
  }

  const confirmAndApply = async () => {
    setPhase("applying")
    try {
      await applyUpdate()
      // On success the app quits; nothing after this line runs long.
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setPhase("confirm")
    }
  }

  return (
    <div
      role="status"
      className="fixed inset-x-0 top-0 z-50 border-b border-border bg-card px-6 py-3 shadow-md"
    >
      <div className="mx-auto flex max-w-4xl flex-wrap items-center justify-between gap-3">
        <p className="min-w-0 text-sm">
          {phase === "confirm" ? (
            <>
              <span className="font-medium">Install version {status.latestVersion}?</span> CatchM will quit to
              update, and Windows may ask for permission. Your meetings and settings are untouched.
            </>
          ) : phase === "downloading" ? (
            <>Downloading version {status.latestVersion}…</>
          ) : phase === "applying" ? (
            <>Updating — CatchM will restart shortly.</>
          ) : (
            <>
              <span className="font-medium">Version {status.latestVersion} is available</span>
              {status.publishedAt ? ` (released ${status.publishedAt})` : ""}
            </>
          )}
          {error && <span className="ml-2 text-destructive">{error}</span>}
        </p>
        <div className="flex shrink-0 items-center gap-2">
          {status.notesURL && (
            <Button size="sm" variant="ghost" onClick={() => window.open(status.notesURL, "_blank")}>
              <ExternalLink /> Release notes
            </Button>
          )}
          {phase === "confirm" ? (
            <>
              <Button size="sm" variant="outline" onClick={dismiss}>
                Not now
              </Button>
              <Button size="sm" onClick={() => void confirmAndApply()}>
                Update now
              </Button>
            </>
          ) : phase === "idle" ? (
            <>
              <Button size="sm" variant="ghost" onClick={dismiss}>
                Dismiss
              </Button>
              <Button size="sm" onClick={() => void startDownload()}>
                Update now
              </Button>
            </>
          ) : null}
        </div>
      </div>
    </div>
  )
}
