import { cn } from "@/lib/utils";

type ShotProps = {
  name: string;
  alt: string;
  width: number;
  height: number;
  priority?: boolean;
  className?: string;
  sizes?: string;
};

/** A product screenshot: AVIF, then WebP, from public/shots. */
export function Shot({ name, alt, width, height, priority, className, sizes }: ShotProps) {
  return (
    <picture>
      <source srcSet={`/shots/${name}.avif`} type="image/avif" sizes={sizes} />
      <source srcSet={`/shots/${name}.webp`} type="image/webp" sizes={sizes} />
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img
        src={`/shots/${name}.webp`}
        alt={alt}
        width={width}
        height={height}
        loading={priority ? "eager" : "lazy"}
        decoding="async"
        fetchPriority={priority ? "high" : undefined}
        className={cn("block h-auto w-full", className)}
      />
    </picture>
  );
}
