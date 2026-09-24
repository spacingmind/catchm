import type { ReactNode } from "react"
import { cn } from "cn"

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
 */
export function SettingRow({
  label,
  htmlFor,
  description,
  control,
}: {
  label: string
  /** id of the row's main control, if any — wires the label to it. */
  htmlFor?: string
  description?: ReactNode
  control: ReactNode
}) {
  return (
    <div className="grid gap-2 border-b border-border py-4 last:border-b-0 sm:grid-cols-[1fr_auto] sm:items-center sm:gap-8">
      <div className="min-w-0">
        <label htmlFor={htmlFor} className="text-sm font-medium">
          {label}
        </label>
        {description && <p className="mt-0.5 text-sm text-muted-foreground">{description}</p>}
      </div>
      <div className="shrink-0 sm:justify-self-end">{control}</div>
    </div>
  )
}

export type SaveState = "idle" | "saving" | "saved" | "error"

/** Quiet save-status indicator ("Saved" fades back to idle). */
export function SaveStatus({ state, error }: { state: SaveState; error?: string | null }) {
  if (state === "idle") return null
  return (
    <p
      aria-live="polite"
      className={cn(
        "text-sm",
        state === "error" ? "text-destructive" : "text-muted-foreground",
      )}
    >
      {state === "saving" && "Saving…"}
      {state === "saved" && "Saved"}
      {state === "error" && (error ?? "Couldn't save")}
    </p>
  )
}

import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"

/**
 * Segmented control for 2–4 choices (per M20 research: not a dropdown),
 * built on shadcn ToggleGroup single-select. Disabled options keep their
 * reason in the title tooltip.
 */
export function SegmentedControl<T extends string>({
  value,
  onChange,
  options,
  label,
}: {
  value: T
  onChange: (value: T) => void
  options: { value: T; label: string; disabled?: boolean; title?: string }[]
  label: string
}) {
  return (
    <ToggleGroup
      type="single"
      value={value}
      onValueChange={(v) => {
        if (v) onChange(v as T)
      }}
      aria-label={label}
      variant="outline"
    >
      {options.map((opt) => (
        <ToggleGroupItem
          key={opt.value}
          value={opt.value}
          disabled={opt.disabled}
          title={opt.disabled ? opt.title : undefined}
        >
          {opt.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
