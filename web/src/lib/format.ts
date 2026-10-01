// Locale-aware date/time formatting. Every formatter uses the active
// UI language's locale (i18n's activeLocale) and is built once per
// locale, cached and rebuilt lazily when the language changes —
// constructing Intl.DateTimeFormat is far more expensive than calling
// format(). Unparseable input comes back as the raw string, never
// "Invalid Date".

import { activeLocale } from "@/i18n"

const fmts = (() => {
  let locale = ""
  let dateTime: Intl.DateTimeFormat
  let time: Intl.DateTimeFormat
  let day: Intl.DateTimeFormat
  let month: Intl.DateTimeFormat
  return () => {
    const l = activeLocale()
    if (l !== locale) {
      locale = l
      dateTime = new Intl.DateTimeFormat(l, { dateStyle: "medium", timeStyle: "short" })
      time = new Intl.DateTimeFormat(l, { timeStyle: "short" })
      day = new Intl.DateTimeFormat(l, { dateStyle: "medium" })
      month = new Intl.DateTimeFormat(l, { month: "long", year: "numeric" })
    }
    return { dateTime, time, day, month }
  }
})()

function parseIso(iso: string | null | undefined): Date | null {
  if (!iso) return null
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? null : d
}

/** "Sep 27, 2026, 3:04 PM" (locale-dependent), no seconds. */
export function formatDateTime(iso: string | null | undefined): string {
  const d = parseIso(iso)
  return d ? fmts().dateTime.format(d) : (iso ?? "")
}

/** "3:04 PM" (locale-dependent), no seconds. */
export function formatTime(iso: string | null | undefined): string {
  const d = parseIso(iso)
  return d ? fmts().time.format(d) : (iso ?? "")
}

/**
 * A meeting's span: "Sep 27, 2026, 3:04 – 4:10 PM" when both ends fall on
 * the same day (formatRange collapses the shared parts), a full range
 * across days, or just the start while it's still live (no end).
 */
export function formatDateRange(startIso: string, endIso?: string | null): string {
  const start = parseIso(startIso)
  if (!start) return startIso ?? ""
  const end = parseIso(endIso)
  if (!end || end < start) return fmts().dateTime.format(start)
  return fmts().dateTime.formatRange(start, end)
}

/**
 * Parses a date-only "YYYY-MM-DD" as local midnight. `new Date("2026-09-27")`
 * is UTC midnight, which is the previous evening west of UTC and shifts
 * "is it overdue" comparisons east of it. Returns null when it isn't a
 * valid calendar date.
 */
export function parseLocalDate(ymd: string | null | undefined): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(ymd?.trim() ?? "")
  if (!m) return null
  const [y, mo, d] = [Number(m[1]), Number(m[2]) - 1, Number(m[3])]
  const date = new Date(y, mo, d)
  // Reject rollovers like 2026-02-31 → Mar 3.
  if (date.getFullYear() !== y || date.getMonth() !== mo || date.getDate() !== d) return null
  return date
}

/**
 * "Sep 27, 2026" for a date-only "YYYY-MM-DD" (parsed as local, see
 * parseLocalDate). Also accepts a full ISO timestamp, formatted as that
 * instant's local day.
 */
export function formatDay(ymd: string | null | undefined): string {
  const d = parseLocalDate(ymd) ?? parseIso(ymd)
  return d ? fmts().day.format(d) : (ymd ?? "")
}

/** "September 2026" (locale-dependent) — meeting list month headers. */
export function formatMonth(d: Date): string {
  return fmts().month.format(d)
}
