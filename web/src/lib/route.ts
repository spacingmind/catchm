import { useMemo, useSyncExternalStore } from "react"

/**
 * Hash routing (docs/plans/active/web-ui-followups.md, Decisions): the
 * same URLs work under `catchm serve` and the Wails asset server, with no
 * Go SPA fallback and no router dependency.
 *
 *   #/meetings
 *   #/meetings/{id}[?tab=mindmap|flows|transcript]
 *   #/live/{id}
 *   #/action-items
 *   #/settings/{section}
 *
 * Anything else parses as the meetings list; App replaces the URL with
 * the canonical form so the address bar never shows a route that isn't
 * the one on screen.
 */

export type MeetingTab = "overview" | "mindmap" | "flows" | "transcript"

const MEETING_TABS: readonly MeetingTab[] = ["overview", "mindmap", "flows", "transcript"]

/** A tab param or tab-trigger value as a MeetingTab; anything else is Overview. */
export function meetingTab(value: string | null | undefined): MeetingTab {
  return MEETING_TABS.find((t) => t === value) ?? "overview"
}

export type Route =
  | { name: "meetings" }
  | { name: "meeting"; id: string; tab: MeetingTab }
  | { name: "live"; id: string }
  | { name: "action-items" }
  // section stays a plain string here: Settings owns the list of valid
  // sections (some are desktop-only) and replaces an unknown one.
  | { name: "settings"; section: string | null }

export const MEETINGS: Route = { name: "meetings" }

/** Fired after pushState/replaceState, which don't fire hashchange. */
const ROUTE_EVENT = "catchm:route"

function decode(segment: string): string | null {
  try {
    return decodeURIComponent(segment)
  } catch {
    return null
  }
}

export function parseRoute(hash: string): Route {
  const raw = hash.replace(/^#/, "")
  const q = raw.indexOf("?")
  const path = q === -1 ? raw : raw.slice(0, q)
  const params = new URLSearchParams(q === -1 ? "" : raw.slice(q + 1))
  const parts = path.split("/").filter(Boolean)
  const [head, arg, extra] = parts
  if (extra !== undefined) return MEETINGS
  const id = arg === undefined ? null : decode(arg)

  switch (head) {
    case "meetings":
      if (arg === undefined) return MEETINGS
      if (!id) return MEETINGS
      return { name: "meeting", id, tab: meetingTab(params.get("tab")) }
    case "live":
      return id ? { name: "live", id } : MEETINGS
    case "action-items":
      return arg === undefined ? { name: "action-items" } : MEETINGS
    case "settings":
      return { name: "settings", section: id || null }
    default:
      return MEETINGS
  }
}

/** Canonical hash (with the leading "#") for a route. */
export function formatRoute(route: Route): string {
  switch (route.name) {
    case "meetings":
      return "#/meetings"
    case "meeting":
      // Overview is the default tab, so it stays out of the URL.
      return `#/meetings/${encodeURIComponent(route.id)}${route.tab === "overview" ? "" : `?tab=${route.tab}`}`
    case "live":
      return `#/live/${encodeURIComponent(route.id)}`
    case "action-items":
      return "#/action-items"
    case "settings":
      return route.section ? `#/settings/${encodeURIComponent(route.section)}` : "#/settings"
  }
}

/**
 * Go to a route. Push (the default) adds a history entry, so Back
 * returns here; replace swaps the current entry — for redirects and
 * in-page state like the meeting tab that shouldn't pile up in Back.
 * A no-op when the URL wouldn't change.
 */
export function navigate(route: Route, { replace = false }: { replace?: boolean } = {}) {
  const hash = formatRoute(route)
  if (window.location.hash === hash) return
  // history.* rather than location.hash: it updates synchronously and
  // lets replace work the same way; ROUTE_EVENT stands in for the
  // hashchange that these calls don't fire.
  if (replace) window.history.replaceState(window.history.state, "", hash)
  else window.history.pushState(null, "", hash)
  window.dispatchEvent(new Event(ROUTE_EVENT))
}

function subscribe(onChange: () => void) {
  // popstate covers Back/Forward between pushState entries, hashchange a
  // typed or clicked #link, ROUTE_EVENT our own navigate().
  window.addEventListener("hashchange", onChange)
  window.addEventListener("popstate", onChange)
  window.addEventListener(ROUTE_EVENT, onChange)
  return () => {
    window.removeEventListener("hashchange", onChange)
    window.removeEventListener("popstate", onChange)
    window.removeEventListener(ROUTE_EVENT, onChange)
  }
}

const getHash = () => window.location.hash

/** The raw location hash, re-rendering on every route change. */
export function useHash(): string {
  return useSyncExternalStore(subscribe, getHash)
}

/** The current parsed route, re-rendering on every route change. */
export function useRoute(): Route {
  const hash = useHash()
  return useMemo(() => parseRoute(hash), [hash])
}
