import { useEffect, useState, type ComponentType, type ReactNode } from "react"
import { CheckSquare, ListVideo, Mic, PanelLeftClose, PanelLeftOpen, Radio, Settings2 } from "lucide-react"
import { cn } from "cn"

import logo from "@/assets/logo.png"
import { Button } from "@/components/ui/button"

export type View = "list" | "live" | "detail" | "action-items" | "settings"

const COLLAPSED_KEY = "catchm.sidebar.collapsed"

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
  children?: ReactNode
}

function NavButton({ icon: Icon, label, title, active, collapsed, disabled, onClick, children }: NavButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={collapsed ? (title ?? label) : title}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex h-9 w-full items-center gap-2.5 rounded-md px-2.5 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
        collapsed && "justify-center px-0",
        active
          ? "bg-muted font-medium text-foreground"
          : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
        disabled && "pointer-events-none opacity-50",
      )}
    >
      <Icon className="size-4 shrink-0" />
      {!collapsed && <span className="min-w-0 flex-1 truncate text-left">{label}</span>}
      {!collapsed && children}
    </button>
  )
}

interface AppSidebarProps {
  view: View
  onNavigate: (view: View) => void
  /** Title of the in-progress meeting; null when nothing is recording. */
  liveTitle: string | null
  onStartMeeting: () => void
}

export function AppSidebar({ view, onNavigate, liveTitle, onStartMeeting }: AppSidebarProps) {
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
        "sticky top-0 flex h-svh shrink-0 flex-col border-r border-border bg-card transition-[width]",
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
          className="flex min-w-0 items-center gap-2 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          <img src={logo} alt="" className="size-6 shrink-0 rounded-md" />
          {!collapsed && <span className="text-lg font-semibold">CatchM</span>}
        </button>
        <Button
          variant="ghost"
          size={collapsed ? "icon-sm" : "icon-sm"}
          onClick={() => setCollapsed((c) => !c)}
          aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
          aria-expanded={!collapsed}
          title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
        >
          <ToggleIcon />
        </Button>
      </div>

      <nav className="flex flex-1 flex-col gap-1 p-3" aria-label="Main">
        <NavButton icon={ListVideo} label="Meetings" collapsed={collapsed} active={view === "list" || view === "detail"} onClick={() => onNavigate("list")} />
        <NavButton
          icon={Radio}
          label={liveTitle ?? "Live"}
          title={liveTitle ?? undefined}
          collapsed={collapsed}
          active={view === "live"}
          disabled={!liveTitle}
          onClick={() => onNavigate("live")}
        >
          {liveTitle && <span className="size-1.5 shrink-0 animate-pulse rounded-full bg-primary" aria-label="Recording" />}
        </NavButton>
        <NavButton icon={CheckSquare} label="Action items" collapsed={collapsed} active={view === "action-items"} onClick={() => onNavigate("action-items")} />
        <NavButton icon={Settings2} label="Settings" title="Settings (Ctrl/⌘,)" collapsed={collapsed} active={view === "settings"} onClick={() => onNavigate("settings")} />
      </nav>

      <div className="border-t border-border p-3">
        <Button
          size="lg"
          className={cn("w-full", collapsed && "px-0")}
          onClick={onStartMeeting}
          title="Start meeting (Ctrl/⌘ N)"
          aria-label="Start meeting"
        >
          <Mic />
          {!collapsed && <span>Start meeting</span>}
        </Button>
      </div>
    </aside>
  )
}
