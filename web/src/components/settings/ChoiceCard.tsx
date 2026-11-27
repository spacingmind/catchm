import type { ReactNode } from "react"
import { cn } from "cn"

/**
 * A radio option drawn as a card: the whole card is the label, so
 * clicking anywhere selects it, and arrow keys move between options
 * like any native radio group. The description (and the reason, when
 * disabled) is the radio's accessible description.
 *
 * Selected: `border-primary bg-primary/5`. Unusable: dashed border,
 * dimmed, with `reason` as visible text (never a tooltip alone).
 * Keep buttons and links out of it: it's a <label>, so a click inside
 * would also select the option. Put actions next to the group instead.
 */
export function ChoiceCard({
  name,
  value,
  checked,
  onChange,
  title,
  description,
  extra,
  badges,
  disabled = false,
  reason,
  className,
}: {
  name: string
  value: string
  checked: boolean
  onChange: (value: string) => void
  title: ReactNode
  description: ReactNode
  extra?: ReactNode
  /** Short facts shown after the title (Badge elements). */
  badges?: ReactNode
  disabled?: boolean
  /** Why the option can't be chosen; shown while disabled. */
  reason?: ReactNode
  className?: string
}) {
  const id = `${name}-${value.replace(/[^a-z0-9-]/gi, "-")}`
  return (
    <label
      htmlFor={id}
      className={cn(
        "flex items-start gap-3 rounded-lg border p-3 transition-colors has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ring",
        disabled
          ? "cursor-not-allowed border-dashed border-border opacity-60"
          : checked
            ? "cursor-pointer border-primary bg-primary/5"
            : "cursor-pointer border-border hover:bg-muted/50",
        className,
      )}
    >
      <input
        id={id}
        type="radio"
        name={name}
        value={value}
        checked={checked}
        disabled={disabled}
        onChange={() => onChange(value)}
        aria-describedby={`${id}-desc`}
        className="mt-1 size-4 shrink-0 accent-primary"
      />
      <span className="min-w-0">
        {badges ? (
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="text-sm font-medium">{title}</span>
            {badges}
          </span>
        ) : (
          <span className="block text-sm font-medium">{title}</span>
        )}
        <span id={`${id}-desc`} className="block text-sm text-muted-foreground">
          {description}
          {extra}
          {disabled && reason && <span className="mt-1 block font-medium text-foreground">{reason}</span>}
        </span>
      </span>
    </label>
  )
}
