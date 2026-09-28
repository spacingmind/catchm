import { useEffect, useId, useState, type ComponentType, type ReactNode, type RefObject } from "react"
import { CheckSquare, FolderOpen, ListVideo, Mic, PanelLeftClose, PanelLeftOpen, Radio, Settings2, Upload } from "lucide-react"
import { cn } from "cn"

import logo from "@/assets/logo.png"
import { ProjectDot } from "@/components/Projects"
import { Button } from "@/components/ui/button"
import { projectKey } from "@/lib/api"
import { useProjects } from "@/lib/projects"
import { hasWindowControls } from "@/lib/wailsRuntime"

export type View = "list" | "live" | "detail" | "action-items" | "settings" | "projects"

/** How many recent projects the sidebar lists before "All projects". */
const SIDEBAR_PROJECTS = 6

const COLLAPSED_KEY = "catchm.sidebar.collapsed"

/**
 * " (Ctrl+N)"-style tooltip suffix for an app shortcut, shown only in the
 * desktop shell: in a plain browser (catchm serve) Ctrl+N / ⌘N and ⌘,
 * belong to the browser, which never lets the page see them, so the
 * hint would advertise a shortcut that doesn't work.
 */
function shortcutHint(key: string): string {
  if (!hasWindowControls()) return ""
  const mac = /Mac|iPhone|iPad/.test(navigator.userAgent)
  return mac ? ` (⌘${key})` : ` (Ctrl+${key})`
}

function readCollapsed(): boolean {
  try {
    return window.localStorage.getItem(COLLAPSED_KEY) === "1"
  } catch {
    return false
  }
}

interface NavButtonProps {
  icon: ComponentType<{ className?: string }>
  label: string
  /** Tooltip text shown when the sidebar is collapsed. */
  title?: string
  active?: boolean
  collapsed: boolean
  disabled?: boolean
  onClick: () => void
  /** Screen-reader-only text appended to the name, e.g. " (recording)". */
  srSuffix?: string
  /** Visual-only trailing adornment (expanded sidebar only). */
  children?: ReactNode
}

function NavButton({ icon: Icon, label, title, active, collapsed, disabled, onClick, srSuffix, children }: NavButtonProps) {
  // The accessible name comes from the (visually hidden when collapsed)
  // label text rather than aria-label, so srSuffix can extend it.
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={collapsed ? (title ?? label) : title}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex h-9 w-full cursor-pointer items-center gap-2.5 rounded-md px-2.5 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        collapsed && "justify-center px-0",
        active
          ? "bg-muted font-medium text-foreground"
          : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
        disabled && "pointer-events-none opacity-50",
      )}
    >
      <Icon className="size-4 shrink-0" />
      <span className={collapsed ? "sr-only" : "min-w-0 flex-1 truncate text-left"}>
        {label}
        {srSuffix && <span className="sr-only">{srSuffix}</span>}
      </span>
      {!collapsed && children}
    </button>
  )
}

/** Recent projects under the main nav; just its icon when collapsed. */
function ProjectsNav({
  view,
  activeProject,
  collapsed,
  onNavigate,
  onOpenProject,
}: {
  view: View
  activeProject: string | null
  collapsed: boolean
  onNavigate: (view: View) => void
  onOpenProject: (name: string) => void
}) {
  const projects = useProjects()
  const headingId = useId()
  const activeKey = activeProject ? projectKey(activeProject) : null

  if (collapsed) {
    return (
      <NavButton
        icon={FolderOpen}
        label="Projects"
        collapsed
        active={view === "projects" || activeKey !== null}
        onClick={() => onNavigate("projects")}
      />
    )
  }

  const recent = (projects ?? []).slice(0, SIDEBAR_PROJECTS)
  // Same row as the main nav (NavButton: h-9, text-sm, size-4 icon): the
  // heading is itself the "All projects" link, and each project is a
  // child row whose dot sits where a nav icon would, indented under it.
  return (
    <div role="group" aria-labelledby={headingId} className="grid gap-1">
      <span id={headingId} className="sr-only">
        Projects
      </span>
      <NavButton
        icon={FolderOpen}
        label="Projects"
        collapsed={false}
        active={view === "projects"}
        onClick={() => onNavigate("projects")}
      />
      {recent.map((p) => {
        const active = view === "list" && activeKey === projectKey(p.name)
        return (
          <button
            key={p.name}
            type="button"
            onClick={() => onOpenProject(p.name)}
            aria-current={active ? "page" : undefined}
            title={p.name}
            className={cn(
              "flex h-9 w-full cursor-pointer items-center gap-2.5 rounded-md pr-2.5 pl-9 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
              active ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
            )}
          >
            <span className="grid size-4 shrink-0 place-items-center">
              <ProjectDot name={p.name} />
            </span>
            <span className="min-w-0 flex-1 truncate text-left">{p.name}</span>
          </button>
        )
      })}
    </div>
  )
}

interface AppSidebarProps {
  view: View
  onNavigate: (view: View) => void
  /** The project the meetings list is filtered to, if any. */
  activeProject: string | null
  /** Open the meetings list filtered to a project. */
  onOpenProject: (name: string) => void
  /** Title of the in-progress meeting; null when nothing is recording. */
  liveTitle: string | null
  onStartMeeting: () => void
  /** Start button — exposed so the start popover can anchor to it. */
  startButtonRef?: RefObject<HTMLButtonElement | null>
  onImportRecording: () => void
}

export function AppSidebar({
  view,
  onNavigate,
  activeProject,
  onOpenProject,
  liveTitle,
  onStartMeeting,
  startButtonRef,
  onImportRecording,
}: AppSidebarProps) {
  const [collapsed, setCollapsed] = useState(readCollapsed)

  useEffect(() => {
    try {
      window.localStorage.setItem(COLLAPSED_KEY, collapsed ? "1" : "0")
    } catch {
      // Storage unavailable (e.g. locked-down WebView) — just don't remember.
    }
  }, [collapsed])

  const ToggleIcon = collapsed ? PanelLeftOpen : PanelLeftClose

  return (
    <aside
      className={cn(
        // app-sidebar: M22 §1 hook — index.css turns this transparent so
        // Mica shows through when running in the Wails desktop shell
        // (html.desktop-mica, set by App.tsx). bg-card stays as the
        // catchm-serve/browser fallback. select-none (M22 §4): pure nav
        // chrome, nothing here needs to be text-selectable.
        "app-sidebar sticky top-0 flex h-svh shrink-0 flex-col border-r border-border bg-card select-none motion-safe:transition-[width]",
        collapsed ? "w-16" : "w-56",
      )}
    >
      <div
        className={cn(
          "flex items-center gap-2 border-b border-border px-3 py-3",
          collapsed ? "flex-col justify-center" : "justify-between",
        )}
      >
        <button
          type="button"
          onClick={() => onNavigate("list")}
          title={collapsed ? "CatchM" : undefined}
          aria-label="CatchM — go to meetings"
          className="-m-1 flex min-w-0 cursor-pointer items-center gap-2 rounded-md p-1 transition-colors hover:bg-muted/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          <img src={logo} alt="" className="size-6 shrink-0 rounded-md" />
          {!collapsed && (
            <span translate="no" className="text-lg font-semibold">
              CatchM
            </span>
          )}
        </button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() => setCollapsed((c) => !c)}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          aria-expanded={!collapsed}
          title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
        >
          <ToggleIcon />
        </Button>
      </div>

      <nav className="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto p-3" aria-label="Main">
        <NavButton
          icon={ListVideo}
          label="Meetings"
          collapsed={collapsed}
          active={(view === "list" && !activeProject) || view === "detail"}
          onClick={() => onNavigate("list")}
        />
        <NavButton
          icon={Radio}
          label={liveTitle ?? "Live"}
          title={liveTitle ?? undefined}
          collapsed={collapsed}
          active={view === "live"}
          disabled={!liveTitle}
          onClick={() => onNavigate("live")}
          srSuffix={liveTitle ? " (recording)" : undefined}
        >
          {liveTitle && <span className="size-1.5 shrink-0 motion-safe:animate-pulse rounded-full bg-primary" aria-hidden="true" />}
        </NavButton>
        <NavButton icon={CheckSquare} label="Action items" collapsed={collapsed} active={view === "action-items"} onClick={() => onNavigate("action-items")} />
        <ProjectsNav
          view={view}
          activeProject={activeProject}
          collapsed={collapsed}
          onNavigate={onNavigate}
          onOpenProject={onOpenProject}
        />
      </nav>

      <div className="border-t border-border p-3">
        <div className="grid gap-1">
          <Button
            ref={startButtonRef}
            size="lg"
            className={cn("w-full", collapsed && "px-0")}
            onClick={onStartMeeting}
            title={`Start meeting${shortcutHint("N")}`}
          >
            <Mic />
            <span className={collapsed ? "sr-only" : undefined}>Start meeting</span>
          </Button>
          <Button
            variant="outline"
            size="sm"
            className={cn("w-full", collapsed && "px-0")}
            onClick={onImportRecording}
            title="Import recording"
          >
            <Upload />
            <span className={collapsed ? "sr-only" : undefined}>Import recording</span>
          </Button>
          <NavButton icon={Settings2} label="Settings" title={`Settings${shortcutHint(",")}`} collapsed={collapsed} active={view === "settings"} onClick={() => onNavigate("settings")} />
        </div>
      </div>
    </aside>
  )
}
