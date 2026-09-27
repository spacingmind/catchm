import { ExternalLink } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useUpdateInstall } from "@/components/useUpdateInstall"

/**
 * M23: unobtrusive banner shown when a check (background or manual)
 * finds a newer release — the Go side emits "update:available" with the
 * same UpdateStatus shape CheckForUpdate returns. Visual treatment
 * mirrors LiveMeeting's capture-error banner (flex-wrap rounded-lg
 * border row with a trailing button cluster) rather than a new
 * component family. The download → confirm → apply flow AND the
 * update:available status live in useUpdateInstall's shared store —
 * the same state the Settings Updates row reads, so both surfaces
 * always agree on the phase. Dismissing calls DeferUpdate: no further
 * prompting this session (a manual check from Settings still reports
 * and re-triggers this banner — manual=true bypasses the deferred flag
 * Go-side).
 */
export function UpdateBanner() {
  const { phase, error, status, download, apply, dismiss } = useUpdateInstall()

  if (!status) return null

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
            <Button size="sm" variant="ghost" onClick={() => window.open(status.notesURL!, "_blank")}>
              <ExternalLink /> Release notes
            </Button>
          )}
          {phase === "confirm" ? (
            <>
              <Button size="sm" variant="outline" onClick={dismiss}>
                Not now
              </Button>
              <Button size="sm" onClick={() => void apply()}>
                Update now
              </Button>
            </>
          ) : phase === "idle" ? (
            <>
              <Button size="sm" variant="ghost" onClick={dismiss}>
                Dismiss
              </Button>
              <Button size="sm" onClick={() => void download()}>
                Update now
              </Button>
            </>
          ) : null}
        </div>
      </div>
    </div>
  )
}
