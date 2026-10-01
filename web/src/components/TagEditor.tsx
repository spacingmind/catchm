import { useId, useRef, useState, type KeyboardEvent } from "react"
import { useTranslation } from "react-i18next"
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
  const { t } = useTranslation()
  const [draft, setDraft] = useState("")
  const [problem, setProblem] = useState<string | null>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const baseId = useId()
  const listId = `${baseId}-suggestions`
  const has = (x: string) => tags.some((y) => y.toLowerCase() === x.toLowerCase())
  const offered = suggestions.filter((s) => !has(s))

  const add = () => {
    const tag = draft.trim().replace(/\s+/g, " ")
    if (!tag || busy) return
    if (isProjectTag(tag)) {
      setProblem(t("tagEditor.projectHint"))
      return
    }
    if (has(tag)) {
      setProblem(t("tagEditor.duplicateTag", { tag }))
      return
    }
    setProblem(null)
    setDraft("")
    onChange([...tags, tag])
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
        {tags.map((tag) => (
          <Badge key={tag} variant="secondary" className="h-6 max-w-full gap-1 pr-0.5">
            <span className="min-w-0 truncate">{tag}</span>
            <button
              type="button"
              onClick={() => {
                if (busy) return
                onChange(tags.filter((x) => x !== tag))
                // The chip (and its button) goes away; keep focus nearby.
                inputRef.current?.focus()
              }}
              aria-disabled={busy || undefined}
              aria-label={t("tagEditor.removeTagAria", { tag })}
              title={t("tagEditor.removeTag")}
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
          aria-label={t("tagEditor.addTag")}
          aria-describedby={`${baseId}-hint ${baseId}-problem`}
          placeholder={t("tagEditor.addTagPlaceholder")}
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
        {t("tagEditor.enterHint")}
      </p>
      <StatusMessage id={`${baseId}-problem`} tone="error">
        {problem}
      </StatusMessage>
    </div>
  )
}
