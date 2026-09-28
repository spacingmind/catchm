import { useId, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { Check, ChevronDown, Folder, Loader2, Plus } from "lucide-react"
import { cn } from "cn"

import { StatusMessage } from "@/components/StatusMessage"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Select, SelectContent, SelectItem, SelectSeparator, SelectTrigger, SelectValue } from "@/components/ui/select"
import { MAX_PROJECT_NAME, normalizeProject, projectKey, renameProject, type Project } from "@/lib/api"
import { noAutofill } from "@/lib/inputProps"
import { findProject, projectColor, refreshProjects, type ProjectChoice } from "@/lib/projects"
import { pluralize } from "@/lib/utils"

const errorText = (err: unknown) => (err instanceof Error ? err.message : String(err))
const sentence = (s: string) => `${s.replace(/[.\s]+$/, "")}.`

/** A project's colour dot. Decorative: the name is always shown too. */
export function ProjectDot({ name, className }: { name: string; className?: string }) {
  return <span aria-hidden="true" className={cn("size-2 shrink-0 rounded-full", projectColor(name), className)} />
}

/** A meeting's project, shown apart from plain tag chips. */
export function ProjectBadge({ name, className }: { name: string; className?: string }) {
  return (
    <Badge variant="outline" className={cn("max-w-full gap-1.5", className)} title={`Project: ${name}`}>
      <Folder aria-hidden="true" />
      <ProjectDot name={name} />
      <span className="sr-only">Project: </span>
      <span className="min-w-0 truncate">{name}</span>
    </Badge>
  )
}

// ---- Start / Import: a project select ----

/** Radix Select items can't have value "". */
const NONE = "__none__"
const NEW = "__new__"

/**
 * Project select for the Start popover and Import dialog: existing
 * projects, "New project…" (reveals a name field) and "No project".
 * `error` is shown under the name field (e.g. an empty new name).
 */
export function ProjectSelectField({
  id,
  value,
  onChange,
  projects,
  error,
}: {
  id: string
  value: ProjectChoice
  onChange: (choice: ProjectChoice) => void
  projects: readonly Project[] | null
  error?: string | null
}) {
  const nameRef = useRef<HTMLInputElement>(null)
  // Set when "New project…" is picked: the select then hands focus to
  // the name field instead of back to its trigger.
  const focusName = useRef(false)
  const existing = value.isNew ? undefined : findProject(projects, value.name)
  const selectValue = value.isNew ? NEW : (existing?.name ?? NONE)

  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>Project</Label>
      <Select
        value={selectValue}
        onValueChange={(v) => {
          if (v === NEW) {
            onChange({ name: "", isNew: true })
            focusName.current = true
          } else {
            onChange({ name: v === NONE ? "" : v, isNew: false })
          }
        }}
      >
        <SelectTrigger id={id} className="w-full" aria-describedby={`${id}-hint`}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent
          onCloseAutoFocus={(e) => {
            if (!focusName.current) return
            focusName.current = false
            e.preventDefault()
            if (nameRef.current) nameRef.current.focus()
            else window.setTimeout(() => nameRef.current?.focus(), 0)
          }}
        >
          <SelectItem value={NONE}>No project</SelectItem>
          {projects && projects.length > 0 && <SelectSeparator />}
          {projects?.map((p) => (
            <SelectItem key={p.name} value={p.name}>
              <ProjectDot name={p.name} />
              {p.name}
            </SelectItem>
          ))}
          <SelectSeparator />
          <SelectItem value={NEW}>
            <Plus aria-hidden="true" />
            New project…
          </SelectItem>
        </SelectContent>
      </Select>
      {value.isNew && (
        <div className="grid gap-1">
          <Label htmlFor={`${id}-name`} className="text-xs font-normal text-muted-foreground">
            Name of the new project
          </Label>
          <Input
            ref={nameRef}
            id={`${id}-name`}
            name="project"
            {...noAutofill}
            value={value.name}
            maxLength={MAX_PROJECT_NAME}
            onChange={(e) => onChange({ name: e.target.value, isNew: true })}
            placeholder="For example: Website redesign"
            aria-invalid={error ? true : undefined}
            aria-describedby={`${id}-error`}
          />
        </div>
      )}
      <p id={`${id}-hint`} className="text-xs text-muted-foreground">
        Group meetings that belong together. You can change this later.
      </p>
      <StatusMessage id={`${id}-error`} tone="error">
        {error}
      </StatusMessage>
    </div>
  )
}

// ---- Meeting page: a project combobox ----

type PickerOption =
  | { kind: "none"; id: string; label: string }
  | { kind: "project"; id: string; label: string; name: string }
  | { kind: "create"; id: string; label: string; name: string }
  | { kind: "new"; id: string; label: string }

/**
 * Combobox for a meeting's project: a trigger showing the project, and
 * a popover with a filter field over a listbox of existing projects,
 * "No project" and "New project…" (typing a name that doesn't exist
 * offers to create it). Arrow keys move, Enter picks, Escape closes.
 */
export function ProjectPicker({
  value,
  projects,
  onChange,
  saving,
  labelledBy,
}: {
  /** Current project display name, "" for none. */
  value: string
  projects: readonly Project[] | null
  onChange: (name: string) => void
  saving?: boolean
  /** id of the visible "Project" label. */
  labelledBy: string
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [active, setActive] = useState(0)
  const [hint, setHint] = useState<string | null>(null)
  const baseId = useId()
  const listId = `${baseId}-list`
  const inputRef = useRef<HTMLInputElement>(null)

  const options = useMemo<PickerOption[]>(() => {
    const q = normalizeProject(query)
    const qKey = q.toLowerCase()
    const list: PickerOption[] = []
    if (!q) list.push({ kind: "none", id: `${baseId}-none`, label: "No project" })
    const matches = (projects ?? []).filter((p) => !qKey || projectKey(p.name).includes(qKey))
    matches.forEach((p, i) => list.push({ kind: "project", id: `${baseId}-p${i}`, label: p.name, name: p.name }))
    if (q && !findProject(projects, q)) {
      list.push({ kind: "create", id: `${baseId}-create`, label: `New project “${q}”`, name: q })
    } else if (!q) {
      list.push({ kind: "new", id: `${baseId}-new`, label: "New project…" })
    }
    return list
  }, [projects, query, baseId])

  const activeIndex = Math.min(active, options.length - 1)
  const activeOption = options[activeIndex]

  const openChange = (next: boolean) => {
    if (next && saving) return
    setOpen(next)
    if (next) {
      setQuery("")
      setHint(null)
      // Start on the current project, so Enter keeps it.
      const current = value ? (projects ?? []).findIndex((p) => projectKey(p.name) === projectKey(value)) : -1
      setActive(current >= 0 ? current + 1 : 0)
    }
  }

  const pick = (opt: PickerOption) => {
    switch (opt.kind) {
      case "new":
        setHint("Type a name for the new project, then press Enter.")
        inputRef.current?.focus()
        return
      case "none":
        onChange("")
        break
      default:
        onChange(opt.name)
    }
    setOpen(false)
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    const last = options.length - 1
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault()
        setActive(activeIndex >= last ? 0 : activeIndex + 1)
        break
      case "ArrowUp":
        e.preventDefault()
        setActive(activeIndex <= 0 ? last : activeIndex - 1)
        break
      case "Home":
        e.preventDefault()
        setActive(0)
        break
      case "End":
        e.preventDefault()
        setActive(last)
        break
      case "Enter":
        e.preventDefault()
        if (activeOption) pick(activeOption)
        break
    }
  }

  return (
    <Popover open={open} onOpenChange={openChange}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="max-w-full min-w-0 justify-start"
          aria-labelledby={`${labelledBy} ${baseId}-value`}
          aria-haspopup="listbox"
          aria-busy={saving || undefined}
        >
          {saving ? (
            <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />
          ) : (
            <Folder aria-hidden="true" />
          )}
          {value && <ProjectDot name={value} />}
          <span id={`${baseId}-value`} className={cn("min-w-0 truncate", !value && "text-muted-foreground")}>
            {value || "No project"}
          </span>
          <ChevronDown aria-hidden="true" className="opacity-60" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 gap-2 p-2">
        <Input
          ref={inputRef}
          role="combobox"
          aria-label="Find or create a project"
          aria-expanded="true"
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={activeOption?.id}
          aria-describedby={hint ? `${baseId}-hint` : undefined}
          {...noAutofill}
          value={query}
          maxLength={MAX_PROJECT_NAME}
          onChange={(e) => {
            setQuery(e.target.value)
            setActive(0)
            setHint(null)
          }}
          onKeyDown={onKeyDown}
          placeholder="Find or create a project…"
        />
        {hint && (
          <p id={`${baseId}-hint`} className="px-1 text-xs text-muted-foreground">
            {hint}
          </p>
        )}
        <ul id={listId} role="listbox" aria-label="Projects" className="grid max-h-64 gap-0.5 overflow-y-auto">
          {options.map((opt, i) => {
            const selected =
              opt.kind === "none" ? !value : opt.kind === "project" && projectKey(opt.name) === projectKey(value)
            return (
              <li
                key={opt.id}
                id={opt.id}
                role="option"
                aria-selected={selected}
                // Keep focus in the filter field; the click still picks.
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => pick(opt)}
                onMouseMove={() => setActive(i)}
                className={cn(
                  "flex cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-1 text-sm",
                  i === activeIndex && "bg-accent text-accent-foreground",
                  (opt.kind === "none" || opt.kind === "new") && "text-muted-foreground",
                )}
              >
                {/* One size-4 slot (the menu icon size), so every label lines up. */}
                <span className="grid size-4 shrink-0 place-items-center">
                  {opt.kind === "project" ? (
                    <ProjectDot name={opt.name} />
                  ) : opt.kind === "none" ? null : (
                    <Plus aria-hidden="true" className="size-4" />
                  )}
                </span>
                <span className="min-w-0 flex-1 truncate">{opt.label}</span>
                {selected && <Check aria-hidden="true" className="size-4 shrink-0" />}
              </li>
            )
          })}
        </ul>
      </PopoverContent>
    </Popover>
  )
}

// ---- Rename ----

/**
 * Rename a project across all its meetings. A name that matches another
 * project merges the two, and the dialog says so before it happens.
 */
export function RenameProjectDialog({
  project,
  projects,
  onOpenChange,
  onRenamed,
}: {
  /** The project being renamed; null keeps the dialog closed. */
  project: string | null
  projects: readonly Project[] | null
  onOpenChange: (open: boolean) => void
  onRenamed: (to: string, count: number) => void
}) {
  const [name, setName] = useState(project ?? "")
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Fresh field each time a different project is opened.
  const [openedFor, setOpenedFor] = useState(project)
  if (project !== openedFor) {
    setOpenedFor(project)
    setName(project ?? "")
    setError(null)
  }

  const to = normalizeProject(name)
  const target = project ? findProject(projects, to) : undefined
  const merging = target !== undefined && project !== null && projectKey(target.name) !== projectKey(project)

  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    if (!project || saving) return
    if (!to) {
      setError("Type a name for the project.")
      return
    }
    if (to === project) {
      onOpenChange(false)
      return
    }
    setSaving(true)
    setError(null)
    try {
      // Merging keeps the existing project's spelling.
      const finalName = merging ? target.name : to
      const { renamed } = await renameProject(project, finalName)
      await refreshProjects()
      onRenamed(finalName, renamed)
      onOpenChange(false)
    } catch (err) {
      setError(`Couldn’t rename the project: ${sentence(errorText(err))} Try again.`)
    } finally {
      setSaving(false)
    }
  }

  const count = project ? (findProject(projects, project)?.meetings ?? 0) : 0

  return (
    <Dialog
      open={project !== null}
      onOpenChange={(next) => {
        if (!next && saving) return
        onOpenChange(next)
      }}
    >
      <DialogContent>
        <form onSubmit={submit} noValidate className="grid gap-4" aria-busy={saving || undefined}>
          <DialogHeader>
            <DialogTitle>Rename project</DialogTitle>
            <DialogDescription>
              {count === 1
                ? `The meeting in “${project}” gets the new name.`
                : count > 1
                  ? `All ${pluralize(count, "meeting")} in “${project}” get the new name.`
                  : `Every meeting in “${project}” gets the new name.`}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-1.5">
            <Label htmlFor="rename-project">New name</Label>
            <Input
              id="rename-project"
              name="project"
              {...noAutofill}
              value={name}
              maxLength={MAX_PROJECT_NAME}
              onChange={(e) => {
                setName(e.target.value)
                setError(null)
              }}
              aria-invalid={error ? true : undefined}
              aria-describedby="rename-project-note rename-project-error"
            />
            <StatusMessage id="rename-project-note" tone="warning">
              {merging && `“${target.name}” already exists. Its meetings and these will be put together in “${target.name}”.`}
            </StatusMessage>
            <StatusMessage id="rename-project-error" tone="error">
              {error}
            </StatusMessage>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
              Cancel
            </Button>
            <Button type="submit" disabled={saving} aria-busy={saving || undefined}>
              {saving && <Loader2 aria-hidden="true" className="motion-safe:animate-spin" />}
              {merging ? "Merge projects" : "Rename"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
