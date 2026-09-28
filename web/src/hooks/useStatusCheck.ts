import { useCallback, useEffect, useRef, useState } from "react"

export interface StatusCheck<T> {
  /** The newest result; kept while a re-check runs, so nothing flashes. */
  data: T | null
  /** The last check's error message; cleared by the next success. */
  error: string | null
  /** True while the first check or a refresh() runs. Background re-checks
   *  (polling, focus) don't set it, so "Check again" doesn't flicker. */
  checking: boolean
  /** Re-check now, showing checking=true until it answers. */
  refresh: () => void
}

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

/**
 * Fetches a setup status (STT, translation, summaries) and keeps it fresh
 * while the user sets things up in another app: it re-checks when the
 * window regains focus or the page becomes visible again, and, with
 * pollMs, every pollMs while the page is visible and pollWhile(data)
 * holds (e.g. "a step is still to do"). Sequence-guarded: only the
 * newest response lands.
 *
 * `fetcher` should be a stable (module-level) function.
 */
export function useStatusCheck<T>(
  fetcher: () => Promise<T>,
  {
    enabled = true,
    pollMs = null,
    pollWhile,
  }: { enabled?: boolean; pollMs?: number | null; pollWhile?: (data: T | null) => boolean } = {},
): StatusCheck<T> {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [checking, setChecking] = useState(enabled)
  const seq = useRef(0)
  const inFlight = useRef(false)

  const run = useCallback(() => {
    const id = ++seq.current
    inFlight.current = true
    fetcher()
      .then((next) => {
        if (id !== seq.current) return
        setData(next)
        setError(null)
      })
      .catch((err: unknown) => {
        if (id === seq.current) setError(errorMessage(err))
      })
      .finally(() => {
        if (id !== seq.current) return
        inFlight.current = false
        setChecking(false)
      })
  }, [fetcher])

  const refresh = useCallback(() => {
    setChecking(true)
    run()
  }, [run])

  useEffect(() => {
    if (!enabled) return
    run()
    const recheck = () => {
      if (document.visibilityState === "visible" && !inFlight.current) run()
    }
    window.addEventListener("focus", recheck)
    document.addEventListener("visibilitychange", recheck)
    return () => {
      window.removeEventListener("focus", recheck)
      document.removeEventListener("visibilitychange", recheck)
      // Drop an answer that arrives after unmount or disable. seq is a
      // counter, not a DOM node, so reading it at cleanup is the point.
      // oxlint-disable-next-line react-hooks/exhaustive-deps
      seq.current++
      inFlight.current = false
    }
  }, [enabled, run])

  const poll = enabled && pollMs != null && (!pollWhile || pollWhile(data))
  useEffect(() => {
    if (!poll || pollMs == null) return
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible" && !inFlight.current) run()
    }, pollMs)
    return () => window.clearInterval(timer)
  }, [poll, pollMs, run])

  return { data, error, checking, refresh }
}

/**
 * Text for a live region that speaks only when a status really changes.
 * `value` describes the current state (null while unknown, which is
 * skipped). The first known value isn't news, and polls that return the
 * same state don't change the text, so screen readers hear only
 * transitions.
 */
export function useAnnounceChange(value: string | null): string {
  const [last, setLast] = useState(value)
  const [message, setMessage] = useState("")
  if (value !== null && value !== last) {
    setLast(value)
    if (last !== null) setMessage(value)
  }
  return message
}
