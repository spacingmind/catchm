import { cn } from "@/lib/utils";

/** The squirrel mark from assets/logo.png (192 px copy in public/). */
export function Logo({ className, size = 36 }: { className?: string; size?: number }) {
  return (
    // eslint-disable-next-line @next/next/no-img-element
    <img
      src="/logo.png"
      alt=""
      width={size}
      height={size}
      className={cn("block rounded-[22%]", className)}
    />
  );
}
