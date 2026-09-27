import type { ComponentProps } from "react"
import { cn } from "cn"

import { Toggle } from "@/components/ui/toggle"

/**
 * A pressable filter chip (tag, owner, meeting). A toggle button, so
 * screen readers get aria-pressed; group chips under a labelled
 * role="group" at the call site.
 */
export function FilterChip({
  pressed,
  onPressedChange,
  children,
  className,
  ...toggleProps
}: Omit<ComponentProps<typeof Toggle>, "pressed" | "onPressedChange" | "variant" | "size"> & {
  pressed: boolean
  onPressedChange: (pressed: boolean) => void
}) {
  return (
    <Toggle
      variant="outline"
      size="sm"
      pressed={pressed}
      onPressedChange={onPressedChange}
      className={cn(
        // Pressed = filled brand chip. The base toggle styles "on" via
        // both aria-pressed: and data-[state=on]:, so override both.
        "rounded-full hover:border-ring",
        "aria-pressed:border-primary aria-pressed:bg-primary aria-pressed:text-primary-foreground aria-pressed:hover:bg-primary/85 aria-pressed:hover:text-primary-foreground",
        "data-[state=on]:border-primary data-[state=on]:bg-primary data-[state=on]:text-primary-foreground data-[state=on]:hover:bg-primary/85 data-[state=on]:hover:text-primary-foreground",
        className,
      )}
      {...toggleProps}
    >
      {children}
    </Toggle>
  )
}
