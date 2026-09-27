import type { MouseEvent } from "react"
import { ExternalLink } from "lucide-react"

import { StatusMessage } from "@/components/StatusMessage"
import { Button } from "@/components/ui/button"
import { useUpdateInstall } from "@/components/useUpdateInstall"
import { formatDay } from "@/lib/format"
import { isAbsoluteUrl } from "@/lib/url"
import { browserOpenURL } from "@/lib/wailsRuntime"

/** Hands the link to the system browser inside the Wails window
 *  (see browserOpenURL); a plain browser follows the <a> normally. */
function openExternal(e: MouseEvent<HTMLAnchorElement>) {
  if (browserOpenURL(e.currentTarget.href)) e.preventDefault()
}

/**
 * M23: unobtrusive banner shown when a check (background or manual)
 * finds a newer release — the Go side emits "update:available" with the
 * same UpdateStatus shape CheckForUpdate returns. Visual treatment
 * mirrors LiveMeeting's capture-error banner (flex-wrap rounded-lg
 * border row with a trailing button cluster) rather than a new
 * component family. It sits in normal flow at the top of <main> (App)
 * so it never covers the sidebar header or the page's first row. The
 * download → confirm → apply flow AND the update:available status live
 * in useUpdateInstall's shared store — the same state the Settings
 * Updates row reads, so both surfaces always agree on the phase.
 * Dismissing calls DeferUpdate: no further prompting this session (a
 * manual check from Settings still reports and re-triggers this banner
 * — manual=true bypasses the deferred flag Go-side).
 */
export function UpdateBanner() {
  const { phase, error, notice, status, download, cancel, apply, dismiss } = useUpdateInstall()

  if (!status) return null

  const notesURL = status.notesURL && isAbsoluteUrl(status.notesURL, ["https", "http"]) ? status.notesURL : null

  return (
    <section aria-label="App update" className="border-b border-border bg-card px-6 py-3">
      <div className="mx-auto flex max-w-4xl flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 flex-1">
          <StatusMessage className="text-foreground">
            {phase === "confirm" ? (
              <>
                <span className="font-medium">Install version {status.latestVersion}?</span> CatchM will quit to
                update, and Windows may ask for permission. Your meetings and settings are untouched.
              </>
            ) : phase === "downloading" ? (
              <>Downloading version {status.latestVersion}…</>
            ) : phase === "cancelling" ? (
              <>Cancelling the download…</>
            ) : phase === "applying" ? (
              <>Installing version {status.latestVersion} and restarting CatchM…</>
            ) : (
              <>
                <span className="font-medium">Version {status.latestVersion} is available</span>
                {status.publishedAt ? ` (released ${formatDay(status.publishedAt)})` : ""}
              </>
            )}
          </StatusMessage>
          {/* One region for the last action's outcome: a failure, or a
              neutral notice like "Download cancelled." */}
          <StatusMessage tone={error ? "error" : "muted"} className="break-words">
            {error ?? notice}
          </StatusMessage>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          {notesURL && (
            <Button asChild size="sm" variant="ghost">
              <a href={notesURL} target="_blank" rel="noreferrer" onClick={openExternal}>
                <ExternalLink /> Release notes
                <span className="sr-only"> (opens in your browser)</span>
              </a>
            </Button>
          )}
          {phase === "confirm" ? (
            <>
              <Button size="sm" variant="outline" onClick={dismiss}>
                Not now
              </Button>
              <Button size="sm" onClick={() => void apply()}>
                Install &amp; restart
              </Button>
            </>
          ) : phase === "downloading" || phase === "cancelling" ? (
            <Button size="sm" variant="outline" disabled={phase === "cancelling"} onClick={() => void cancel()}>
              {phase === "cancelling" ? "Cancelling…" : "Cancel download"}
            </Button>
          ) : phase === "idle" ? (
            <>
              <Button size="sm" variant="ghost" onClick={dismiss}>
                Dismiss
              </Button>
              <Button size="sm" onClick={() => void download()}>
                Download update
              </Button>
            </>
          ) : null}
        </div>
      </div>
    </section>
  )
}
