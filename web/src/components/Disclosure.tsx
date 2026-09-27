import type { ReactNode } from "react"
import { ChevronRight } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"

/**
 * A show/hide section ("Advanced", "Options"). Radix wires the trigger's
 * aria-expanded and aria-controls to the content; controlled when `open`
 * is passed, otherwise uncontrolled from `defaultOpen`.
 */
export function Disclosure({
  label,
  open,
  defaultOpen,
  onOpenChange,
  children,
  className,
}: {
  label: ReactNode
  open?: boolean
  defaultOpen?: boolean
  onOpenChange?: (open: boolean) => void
  children: ReactNode
  className?: string
}) {
  return (
    <Collapsible open={open} defaultOpen={defaultOpen} onOpenChange={onOpenChange} className={className}>
      <CollapsibleTrigger asChild>
        <Button variant="ghost" size="sm" className="-ml-2.5">
          <ChevronRight
            aria-hidden="true"
            className="motion-safe:transition-transform group-data-[state=open]/button:rotate-90"
          />
          {label}
        </Button>
      </CollapsibleTrigger>
      <CollapsibleContent className="pt-2">{children}</CollapsibleContent>
    </Collapsible>
  )
}
