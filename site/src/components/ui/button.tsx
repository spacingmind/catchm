import type { AnchorHTMLAttributes } from "react";
import { cn } from "@/lib/utils";

type Variant = "primary" | "outline" | "quiet";

const base =
  "inline-flex items-center justify-center gap-2 rounded-xl font-medium whitespace-nowrap transition-colors duration-150 select-none";

const variants: Record<Variant, string> = {
  primary: "bg-primary text-primary-fg hover:bg-primary-hover",
  outline:
    "border border-line-strong bg-transparent text-ink hover:bg-tan/60",
  quiet: "text-ink hover:bg-tan/60",
};

const sizes = {
  md: "h-10 px-4 text-sm",
  lg: "h-12 px-5 text-[0.95rem]",
} as const;

type ButtonLinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & {
  variant?: Variant;
  size?: keyof typeof sizes;
};

/** Every call to action on this page is a link (a download or an anchor). */
export function ButtonLink({ variant = "primary", size = "lg", className, ...props }: ButtonLinkProps) {
  return <a className={cn(base, variants[variant], sizes[size], className)} {...props} />;
}
