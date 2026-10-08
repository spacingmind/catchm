import { Cloud, House, SlidersHorizontal, type LucideIcon } from "lucide-react";

export type Where = "local" | "cloud" | "either";

const where: Record<Where, { label: string; Icon: LucideIcon }> = {
  local: { label: "On your computer", Icon: House },
  cloud: { label: "Cloud", Icon: Cloud },
  either: { label: "Your choice", Icon: SlidersHorizontal },
};

export function WhereBadge({ where: w }: { where: Where }) {
  const { label, Icon } = where[w];
  return (
    <span className="inline-flex items-center gap-1 rounded-full bg-chip text-chip-fg px-2 py-0.5 text-xs font-medium whitespace-nowrap">
      <Icon aria-hidden="true" className="size-3" />
      {label}
    </span>
  );
}

/** "Name [badge]" rows: which engine does the work, and where it runs. */
export function EngineList({
  title,
  items,
}: {
  title: string;
  items: [name: string, where: Where, note: string][];
}) {
  return (
    <div className="rounded-2xl border border-line bg-card p-5">
      <h4 className="text-sm font-semibold tracking-tight">{title}</h4>
      <dl className="mt-3 space-y-3">
        {items.map(([name, w, note]) => (
          <div key={name} className="border-t border-line pt-3 first:border-t-0 first:pt-0">
            <dt className="flex flex-wrap items-center justify-between gap-2 text-sm font-medium">
              {name}
              <WhereBadge where={w} />
            </dt>
            <dd className="mt-1 text-sm text-muted">{note}</dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
