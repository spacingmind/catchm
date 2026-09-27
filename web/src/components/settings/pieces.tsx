import { useId, type ReactNode } from "react"
import { cn } from "cn"

import { StatusMessage } from "@/components/StatusMessage"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

// The id helpers live beside SettingRow on purpose (Phase 2 imports them
// from here); only-export-components is just about Fast Refresh, and
// these pure functions never change at runtime.

/** id of a SettingRow's description, for the control's aria-describedby. */
// oxlint-disable-next-line react/only-export-components
export function descId(id: string): string {
  return `${id}-desc`
}

/** id of a SettingRow's label, for controls named via aria-labelledby. */
// oxlint-disable-next-line react/only-export-components
export function labelId(id: string): string {
  return `${id}-label`
}

/** A settings page section: heading + short description + rows/content. */
export function SettingsSection({
  title,
  description,
  children,
  className,
}: {
  title: string
  description?: string
  children: ReactNode
  className?: string
}) {
  return (
    <section className={cn("mb-8", className)}>
      <h2 className="text-lg font-semibold">{title}</h2>
      {description && <p className="mb-2 mt-0.5 text-sm text-muted-foreground">{description}</p>}
      {children}
    </section>
  )
}

/**
 * One setting row: label + description on the left, control on the right.
 * Rows stack vertically below the control on narrow panes.
 *
 * The label gets id={labelId(base)} and the description
 * id={descId(base)}, where base is htmlFor (or id); the control wires
 * aria-describedby={descId(base)} itself since it's an opaque node here.
 */
export function SettingRow({
  label,
  htmlFor,
  id,
  description,
  control,
}: {
  label: string
  /** id of the row's main control, if it's labelable (input, select
   *  trigger, button) — renders a real <label for>. */
  htmlFor?: string
  /** Base id for rows whose control isn't labelable (e.g. a
   *  SegmentedControl, which is a role=group): the label renders as a
   *  <span id={labelId(id)}> for the control's labelledBy. Ignored when
   *  htmlFor is set. */
  id?: string
  description?: ReactNode
  control: ReactNode
}) {
  const base = htmlFor ?? id
  return (
    <div className="grid gap-2 border-b border-border py-4 last:border-b-0 sm:grid-cols-[1fr_auto] sm:items-center sm:gap-8">
      <div className="min-w-0">
        {htmlFor ? (
          <label id={labelId(htmlFor)} htmlFor={htmlFor} className="text-sm font-medium">
            {label}
          </label>
        ) : (
          <span id={base ? labelId(base) : undefined} className="text-sm font-medium">
            {label}
          </span>
        )}
        {description && (
          <p id={base ? descId(base) : undefined} className="mt-0.5 text-sm text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      <div className="shrink-0 sm:justify-self-end">{control}</div>
    </div>
  )
}

export type SaveState = "idle" | "saving" | "saved" | "error"

/**
 * Quiet save-status indicator ("Saved" fades back to idle). Always
 * mounted (empty when idle) so each state change is announced.
 */
export function SaveStatus({
  state,
  error,
  id,
  className,
}: {
  state: SaveState
  error?: string | null
  id?: string
  className?: string
}) {
  return (
    <StatusMessage id={id} className={className} tone={state === "error" ? "error" : "muted"}>
      {state === "saving"
        ? "Saving…"
        : state === "saved"
          ? "Saved"
          : state === "error"
            ? error || "Couldn’t save. Check that CatchM is still running, then try again."
            : null}
    </StatusMessage>
  )
}

/**
 * Segmented control for 2–4 choices (per M20 research: not a dropdown),
 * built on shadcn ToggleGroup single-select. A disabled option's reason
 * is shown as visible text under the group and linked to the option via
 * aria-describedby — a title tooltip alone is invisible to keyboard and
 * touch users. Identical reasons (e.g. two options gated on the same
 * capability) are shown once.
 */
export function SegmentedControl<T extends string>({
  value,
  onChange,
  options,
  label,
  labelledBy,
  describedBy,
  id,
}: {
  value: T
  onChange: (value: T) => void
  options: {
    value: T
    label: string
    disabled?: boolean
    /** Why the option is disabled; shown as text while it is. */
    reason?: string
    /** Legacy tooltip-only reason; used as the reason when reason is unset. */
    title?: string
  }[]
  /** aria-label; unnecessary when labelledBy points at a visible label. */
  label?: string
  /** id of a visible label element (e.g. labelId(rowId) from SettingRow). */
  labelledBy?: string
  /** id of the row description (e.g. descId(rowId)). */
  describedBy?: string
  id?: string
}) {
  const autoId = useId()
  const reasonBase = `${id ?? autoId}-reason`

  const reasons: string[] = []
  const reasonIdFor = (opt: (typeof options)[number]): string | undefined => {
    const reason = opt.disabled ? (opt.reason ?? opt.title) : undefined
    if (!reason) return undefined
    let i = reasons.indexOf(reason)
    if (i < 0) i = reasons.push(reason) - 1
    return `${reasonBase}-${i}`
  }
  const items = options.map((opt) => ({ opt, reasonId: reasonIdFor(opt) }))

  const group = (
    <ToggleGroup
      id={id}
      type="single"
      value={value}
      onValueChange={(v) => {
        if (v) onChange(v as T)
      }}
      aria-label={labelledBy ? undefined : label}
      aria-labelledby={labelledBy}
      aria-describedby={describedBy}
      variant="outline"
    >
      {items.map(({ opt, reasonId }) => (
        <ToggleGroupItem
          key={opt.value}
          value={opt.value}
          disabled={opt.disabled}
          aria-describedby={reasonId}
        >
          {opt.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )

  if (reasons.length === 0) return group
  return (
    <div className="grid gap-1.5">
      {group}
      {reasons.map((reason, i) => (
        <p key={reason} id={`${reasonBase}-${i}`} className="max-w-80 text-xs text-muted-foreground">
          {reason}
        </p>
      ))}
    </div>
  )
}
