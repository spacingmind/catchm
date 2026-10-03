import type { MouseEvent } from "react"
import { useTranslation } from "react-i18next"
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
  const { t } = useTranslation()
  const { phase, error, notice, status, download, openDownload, cancel, apply, dismiss } = useUpdateInstall()

  if (!status) return null

  const notesURL = status.notesURL && isAbsoluteUrl(status.notesURL, ["https", "http"]) ? status.notesURL : null
  // Optional in the wire shape; interpolation needs a string (the old
  // template rendered an empty gap when absent, so "" preserves that).
  const version = status.latestVersion ?? ""

  return (
    <section aria-label={t("updateBanner.region")} className="border-b border-border bg-card px-6 py-3">
      <div className="mx-auto flex max-w-4xl flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 flex-1">
          <StatusMessage className="text-foreground">
            {phase === "confirm" ? (
              <>
                <span className="font-medium">{t("updateBanner.confirmTitle", { version })}</span> {t("updateBanner.confirmBody")}
              </>
            ) : phase === "downloading" ? (
              <>{t("updateBanner.downloading", { version })}</>
            ) : phase === "cancelling" ? (
              <>{t("updateBanner.cancelling")}</>
            ) : phase === "applying" ? (
              <>{t("updateBanner.applying", { version })}</>
            ) : (
              <>
                <span className="font-medium">{t("updateBanner.available", { version })}</span>
                {status.publishedAt ? t("updateBanner.released", { date: formatDay(status.publishedAt) }) : ""}
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
                <ExternalLink /> {t("updateBanner.releaseNotes")}
                <span className="sr-only">{t("settings.pieces.opensInBrowser")}</span>
              </a>
            </Button>
          )}
          {status.selfInstall === false ? (
            phase === "idle" ? (
              <>
                <Button size="sm" variant="ghost" onClick={dismiss}>
                  {t("app.dismiss")}
                </Button>
                <Button size="sm" onClick={() => void openDownload()}>
                  {t("updateBanner.downloadUpdate")}
                </Button>
              </>
            ) : null
          ) : phase === "confirm" ? (
            <>
              <Button size="sm" variant="outline" onClick={dismiss}>
                {t("updateBanner.notNow")}
              </Button>
              <Button size="sm" onClick={() => void apply()}>
                {t("updateBanner.installRestart")}
              </Button>
            </>
          ) : phase === "downloading" || phase === "cancelling" ? (
            <Button size="sm" variant="outline" disabled={phase === "cancelling"} onClick={() => void cancel()}>
              {phase === "cancelling" ? t("updateBanner.cancellingShort") : t("updateBanner.cancelDownload")}
            </Button>
          ) : phase === "idle" ? (
            <>
              <Button size="sm" variant="ghost" onClick={dismiss}>
                {t("app.dismiss")}
              </Button>
              <Button size="sm" onClick={() => void download()}>
                {t("updateBanner.downloadUpdate")}
              </Button>
            </>
          ) : null}
        </div>
      </div>
    </section>
  )
}
