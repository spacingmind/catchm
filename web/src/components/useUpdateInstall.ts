import i18next from "i18next"
import { useSyncExternalStore } from "react"

import {
  applyUpdate,
  cancelDownload,
  deferUpdate,
  downloadUpdate,
  windowEventsOn,
  type UpdateStatus,
} from "@/lib/wailsRuntime"

export type UpdatePhase = "idle" | "downloading" | "cancelling" | "confirm" | "applying"

/**
 * M23's download → confirm → apply flow. A module-level store (not
 * per-component useState): UpdateBanner and the Settings Updates row
 * both render this state, and with per-hook copies two "Update now"
 * clicks downloaded the installer twice while one surface said
 * "downloading" and the other sat idle. One store means one phase, one
 * error, one in-flight download — download() and apply() are no-ops
 * while either is already running, whichever surface calls them.
 *
 * The UpdateStatus from the "update:available" event is shared the
 * same way (the banner's visibility state): one subscription lives
 * here at module scope, so a check's event reaches every surface even
 * if the banner happens to be unmounted (compact mode).
 *
 * "Apply" always means the user just confirmed: the app quits and
 * Windows asks for permission (UAC) — no surprise quit.
 *
 * download: fetch + SHA-256-verify the installer (checksum mismatch /
 * network failure surface as `error` and stop — deferred server-side
 * on mismatch, no retry loop here). cancel: stop that download; Go
 * removes the partial file and the flow is back at idle, ready to
 * retry, with `notice` saying so rather than an error. apply: run the
 * verified installer elevated; on success the app quits and nothing
 * after it runs long.
 */
interface UpdateInstallState {
  phase: UpdatePhase
  error: string | null
  /** Neutral outcome of the last action ("Download cancelled."). */
  notice: string | null
  /** Last "update:available" payload; null until a check finds one. */
  status: UpdateStatus | null
}

let state: UpdateInstallState = { phase: "idle", error: null, notice: null, status: null }
const listeners = new Set<() => void>()

function setState(patch: Partial<UpdateInstallState>) {
  state = { ...state, ...patch }
  for (const notify of listeners) notify()
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function getSnapshot(): UpdateInstallState {
  return state
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err)
}

function isInFlight(phase: UpdatePhase): boolean {
  return phase === "downloading" || phase === "cancelling" || phase === "applying"
}

/** download: no-op while a download or apply is already in flight. */
async function download() {
  if (isInFlight(state.phase)) return
  setState({ phase: "downloading", error: null, notice: null })
  try {
    const result = await downloadUpdate()
    setState(
      result.cancelled
        ? { phase: "idle", notice: i18next.t("updateBanner.downloadCancelled") }
        : { phase: "confirm", error: null },
    )
  } catch (err) {
    setState({ error: errorMessage(err), phase: "idle" })
  }
}

/** cancel: only while downloading. The phase change itself comes from
 *  download() resolving as cancelled; this just asks Go to stop. */
async function cancel() {
  if (state.phase !== "downloading") return
  setState({ phase: "cancelling" })
  try {
    await cancelDownload()
  } catch {
    // The download is still running (unless it finished while we were
    // asking): say so and offer Cancel again.
    // getSnapshot(), not state: TS keeps the pre-await narrowing.
    if (getSnapshot().phase === "cancelling") {
      setState({ phase: "downloading", error: i18next.t("updateBanner.cancelFailed") })
    }
  }
}

/** apply: only ever valid from "confirm" — a no-op otherwise (which
 *  covers an apply already in flight). */
async function apply() {
  if (state.phase !== "confirm") return
  setState({ phase: "applying" })
  try {
    await applyUpdate()
    // On success the app quits; nothing after this line runs long.
  } catch (err) {
    setState({ error: errorMessage(err), phase: "confirm" })
  }
}

/** Back to idle after a decline/error — the shared status stays. */
function reset() {
  setState({ phase: "idle", error: null, notice: null })
}

/** Hide the banner surface and stop prompting this session. */
function dismiss() {
  setState({ status: null })
  deferUpdate()
}

// One subscription for the whole app (module scope — no surface can
// mount late and miss the event). A fresh status returns the flow to
// idle unless a download/apply is mid-flight, whose phase must win
// over the reset.
windowEventsOn("update:available", (data) => {
  const s = Array.isArray(data) ? data[0] : data
  if (!s || typeof s !== "object") return
  setState(
    isInFlight(state.phase)
      ? { status: s as UpdateStatus }
      : { status: s as UpdateStatus, phase: "idle", error: null, notice: null },
  )
})

/**
 * Per-surface read of the shared update-install state. The returned
 * actions are stable module functions; only the snapshot changes.
 */
export function useUpdateInstall() {
  const snap = useSyncExternalStore(subscribe, getSnapshot)
  return {
    phase: snap.phase,
    error: snap.error,
    notice: snap.notice,
    status: snap.status,
    download,
    cancel,
    apply,
    reset,
    dismiss,
  }
}
