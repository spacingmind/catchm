import { useEffect, useSyncExternalStore } from "react"

import { listProjects, normalizeProject, projectKey, type Project } from "@/lib/api"

/**
 * Project colours: a small palette of earthy hues that sit with the
 * chocolate/cream brand. Picked by a stable hash of the name, so a
 * project keeps its colour everywhere without a registry. Each dot
 * clears 3:1 (non-text) on both themes' backgrounds: the light shade on
 * cream, the dark: shade on deep chocolate.
 */
const PROJECT_COLORS = [
  "bg-orange-700 dark:bg-orange-400",
  "bg-teal-700 dark:bg-teal-300",
  "bg-amber-700 dark:bg-amber-300",
  "bg-sky-700 dark:bg-sky-300",
  "bg-rose-700 dark:bg-rose-300",
  "bg-lime-700 dark:bg-lime-300",
  "bg-violet-600 dark:bg-violet-300",
  "bg-fuchsia-700 dark:bg-fuchsia-300",
] as const

/** FNV-1a over the matching key, so "Acme" and "acme" share a colour. */
function hash(s: string): number {
  let h = 0x811c9dc5
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 0x01000193)
  }
  return h >>> 0
}

/** Tailwind background classes for a project's colour dot. */
export function projectColor(name: string): string {
  return PROJECT_COLORS[hash(projectKey(name)) % PROJECT_COLORS.length]
}

// ---- Prefs (non-secret UI state, catchm.prefs.*) ----

const LAST_PROJECT_KEY = "catchm.prefs.project.last"
const GROUP_BY_KEY = "catchm.prefs.meetings.groupBy"

function readStored(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

function writeStored(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // Storage unavailable — the pref just won't persist.
  }
}

/** The project last picked when starting or importing ("" = none). */
export function readLastProject(): string {
  return readStored(LAST_PROJECT_KEY) ?? ""
}

export function writeLastProject(name: string) {
  writeStored(LAST_PROJECT_KEY, name)
}

export type MeetingGroupBy = "date" | "project"

export function readGroupBy(): MeetingGroupBy {
  return readStored(GROUP_BY_KEY) === "project" ? "project" : "date"
}

export function writeGroupBy(v: MeetingGroupBy) {
  writeStored(GROUP_BY_KEY, v)
}

// ---- Shared project list ----
//
// One cached GET /api/projects for the sidebar, pickers and selects.
// Anything that changes a meeting's project calls refreshProjects() so
// every view catches up; views also refetch when they mount.

let cache: Project[] | null = null
let seq = 0
const listeners = new Set<() => void>()

function emit() {
  for (const l of listeners) l()
}

/** Refetch the project list; only the newest answer lands. */
export function refreshProjects(): Promise<void> {
  const id = ++seq
  return listProjects()
    .then((ps) => {
      if (id !== seq) return
      cache = ps
      emit()
    })
    .catch(() => {
      // Keep the last known list; the next refresh tries again.
    })
}

function subscribe(onChange: () => void) {
  listeners.add(onChange)
  return () => {
    listeners.delete(onChange)
  }
}

const getSnapshot = () => cache

/** The project list (null until the first load), refreshed on mount and
 *  when the window regains focus (meetings may change elsewhere). */
export function useProjects(): Project[] | null {
  const projects = useSyncExternalStore(subscribe, getSnapshot)
  useEffect(() => {
    void refreshProjects()
    const onFocus = () => void refreshProjects()
    window.addEventListener("focus", onFocus)
    return () => window.removeEventListener("focus", onFocus)
  }, [])
  return projects
}

/** The existing project with this name (case-insensitive), if any. */
export function findProject(projects: readonly Project[] | null, name: string): Project | undefined {
  const key = projectKey(name)
  return key ? projects?.find((p) => projectKey(p.name) === key) : undefined
}

/** A project choice in a form: an existing project by name, or a new
 *  one being typed. `{ name: "", isNew: false }` is "No project". */
export interface ProjectChoice {
  name: string
  isNew: boolean
}

/** The project name to send for a choice ("" = none). An existing
 *  choice that no longer exists (renamed since) falls back to none; a
 *  new name that matches an existing project uses its spelling. */
export function choiceProject(choice: ProjectChoice, projects: readonly Project[] | null): string {
  if (choice.isNew) return findProject(projects, choice.name)?.name ?? normalizeProject(choice.name)
  return findProject(projects, choice.name)?.name ?? ""
}
