import { useId, useRef, useState, type KeyboardEvent } from "react"
import { X } from "lucide-react"

import { StatusMessage } from "@/components/StatusMessage"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { isProjectTag } from "@/lib/api"

/**
 * Plain tags (never the project tag): chips with a remove button, and a
 * field where Enter adds a tag, with suggestions from existing tags.
 * `onChange` gets the full new list of plain tags.
 */
export function TagEditor({
  tags,
  suggestions,
  onChange,
  busy,
  labelledBy,
}: {
  tags: readonly string[]
  suggestions: readonly string[]
  onChange: (tags: string[]) => void
  /** A save is running: changes wait (the field stays focusable). */
  busy?: boolean
  /** id of the visible "Tags" label. */
  labelledBy: string
}) {
  const [draft, setDraft] = useState("")
  const [problem, setProblem] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const baseId = useId()
  const listId = `${baseId}-suggestions`
  const has = (t: string) => tags.some((x) => x.toLowerCase() === t.toLowerCase())
  const offered = suggestions.filter((s) => !has(s))

  const add = () => {
    const t = draft.trim().replace(/\s+/g, " ")
    if (!t || busy) return
    if (isProjectTag(t)) {
      setProblem("To put this meeting in a project, use Project above.")
      return
    }
    if (has(t)) {
      setProblem(`This meeting already has the tag “${t}”.`)
      return
    }
    setProblem(null)
    setDraft("")
    onChange([...tags, t])
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault()
      add()
    }
  }

  return (
    <div role="group" aria-labelledby={labelledBy} className="grid gap-1.5">
      <div className="flex flex-wrap items-center gap-1.5">
        {tags.map((t) => (
          <Badge key={t} variant="secondary" className="h-6 max-w-full gap-1 pr-0.5">
            <span className="min-w-0 truncate">{t}</span>
            <button
              type="button"
              onClick={() => {
                if (busy) return
                onChange(tags.filter((x) => x !== t))
                // The chip (and its button) goes away; keep focus nearby.
                inputRef.current?.focus()
              }}
              aria-disabled={busy || undefined}
              aria-label={`Remove tag “${t}”`}
              title="Remove tag"
              className="grid size-5 cursor-pointer place-items-center rounded-full hover:bg-foreground/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring aria-disabled:opacity-50"
            >
              <X aria-hidden="true" className="size-3" />
            </button>
          </Badge>
        ))}
        <Input
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value)
            setProblem(null)
          }}
          ref={inputRef}
          onKeyDown={onKeyDown}
          readOnly={busy}
          list={listId}
          aria-label="Add a tag"
          aria-describedby={`${baseId}-hint ${baseId}-problem`}
          placeholder="Add a tag…"
          autoComplete="off"
          maxLength={60}
          className="h-7 w-40 text-sm"
        />
        <datalist id={listId}>
          {offered.map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
      </div>
      <p id={`${baseId}-hint`} className="sr-only">
        Press Enter to add the tag.
      </p>
      <StatusMessage id={`${baseId}-problem`} tone="error" className="text-xs">
        {problem}
      </StatusMessage>
    </div>
  )
}
