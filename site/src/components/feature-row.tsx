import { cn } from "@/lib/utils";

/** A product screenshot sitting on a panel. */
export function Frame({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("lift-sm overflow-hidden rounded-xl border border-line-strong/60 bg-bg", className)}>
      {children}
    </div>
  );
}

export function Pill({ children }: { children: React.ReactNode }) {
  return (
    <span className="rounded-full bg-card/90 px-2.5 py-1 text-xs font-medium text-ink shadow-sm ring-1 ring-line">
      {children}
    </span>
  );
}

/** A feature's stage: "beta" works but is still being tuned; "soon"
 *  isn't available yet. */
export type Stage = "beta" | "soon";

export function StatusTag({ stage, className }: { stage: Stage; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-xs font-semibold tracking-normal normal-case whitespace-nowrap",
        stage === "beta" ? "bg-chip text-chip-fg" : "bg-card text-muted ring-1 ring-line",
        className,
      )}
    >
      {stage === "beta" ? "Beta" : "Coming soon"}
    </span>
  );
}

/** Text on one side, a visual on a warm panel on the other. */
export function Row({
  title,
  body,
  note,
  extra,
  visual,
  flip,
  id,
  wide,
  eyebrow,
  stage,
}: {
  id?: string;
  eyebrow?: string;
  stage?: Stage;
  title: string;
  body: string;
  note?: string;
  extra?: React.ReactNode;
  visual: React.ReactNode;
  flip?: boolean;
  /** Give the visual 8 of 12 columns instead of 7. */
  wide?: boolean;
}) {
  return (
    <article className="grid items-center gap-10 lg:grid-cols-12 lg:gap-16">
      <div className={cn(wide ? "lg:col-span-4" : "lg:col-span-5", flip && "lg:order-2")}>
        {(eyebrow || stage) && (
          <p className="mb-3 flex flex-wrap items-center gap-2.5">
            {eyebrow && <span className="eyebrow">{eyebrow}</span>}
            {stage && <StatusTag stage={stage} />}
          </p>
        )}
        {id ? (
          <h2 id={id} className="display text-4xl leading-[1.08] sm:text-5xl">
            {title}
          </h2>
        ) : (
          <h3 className="display text-[2rem] leading-[1.08] sm:text-4xl">{title}</h3>
        )}
        <p className="mt-4 max-w-md text-lg text-muted">{body}</p>
        {extra}
        {note && <p className="mt-4 max-w-md text-sm text-subtle">{note}</p>}
      </div>
      <div className={cn(wide ? "lg:col-span-8" : "lg:col-span-7", flip && "lg:order-1")}>
        <div className="rounded-[2rem] bg-panel p-5 sm:p-8">
          {visual}
        </div>
      </div>
    </article>
  );
}

/** Section heading block shared by the feature sections. */
export function SectionHead({ id, eyebrow, title, stage }: { id: string; eyebrow: string; title: string; stage?: Stage }) {
  return (
    <>
      <p className="flex flex-wrap items-center gap-2.5">
        <span className="eyebrow">{eyebrow}</span>
        {stage && <StatusTag stage={stage} />}
      </p>
      <h2 id={id} className="display mt-3 max-w-2xl text-4xl sm:text-5xl">
        {title}
      </h2>
    </>
  );
}
