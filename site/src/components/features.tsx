import {
  FileAudio,
  FileDown,
  Languages,
  Network,
  Search,
  Sparkles,
  type LucideIcon,
} from "lucide-react";
import { Shot } from "@/components/ui/shot";
import { SubtitleScene } from "@/components/subtitle-scene";
import { cn } from "@/lib/utils";

/** A product screenshot sitting on a panel. */
function Frame({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("lift-sm overflow-hidden rounded-xl border border-line-strong/60 bg-bg", className)}>
      {children}
    </div>
  );
}

function Row({
  title,
  body,
  note,
  visual,
  flip,
}: {
  title: string;
  body: string;
  note?: string;
  visual: React.ReactNode;
  flip?: boolean;
}) {
  return (
    <article className="grid items-center gap-10 lg:grid-cols-12 lg:gap-16">
      <div className={cn("lg:col-span-5", flip && "lg:order-2")}>
        <h3 className="display text-[2rem] leading-[1.08] sm:text-4xl">{title}</h3>
        <p className="mt-4 max-w-md text-lg text-muted">{body}</p>
        {note && <p className="mt-4 max-w-md text-sm text-muted">{note}</p>}
      </div>
      <div className={cn("lg:col-span-7", flip && "lg:order-1")}>
        <div className="rounded-[2rem] bg-[color-mix(in_oklab,var(--tan)_55%,var(--bg))] p-5 sm:p-8">
          {visual}
        </div>
      </div>
    </article>
  );
}

function Pill({ children }: { children: React.ReactNode }) {
  return (
    <span className="rounded-full bg-card/90 px-2.5 py-1 text-xs font-medium text-ink shadow-sm ring-1 ring-line">
      {children}
    </span>
  );
}

const tiles: { Icon: LucideIcon; title: string; body: string }[] = [
  {
    Icon: Network,
    title: "Mind map and flows",
    body: "See a meeting as a mind map, or pull out the process it walked through.",
  },
  {
    Icon: Search,
    title: "Search, tags, projects",
    body: "Find any meeting. Group them by project and tag.",
  },
  {
    Icon: Sparkles,
    title: "Refine afterwards",
    body: "Run the recording through a second, more accurate pass.",
  },
  {
    Icon: FileDown,
    title: "Export",
    body: "Copy a meeting, or save it as .txt, Markdown or Word.",
  },
  {
    Icon: FileAudio,
    title: "Import a recording",
    body: "Turn an audio file into a meeting.",
  },
  {
    Icon: Languages,
    title: "English and Vietnamese",
    body: "Switch the app itself between the two.",
  },
];

export function Features() {
  return (
    <section id="features" aria-labelledby="features-title" className="mx-auto max-w-6xl px-5 py-20 sm:px-8 sm:py-28">
      <p className="eyebrow">Features</p>
      <h2 id="features-title" className="display mt-3 max-w-2xl text-4xl sm:text-5xl">
        Everything a meeting leaves behind
      </h2>

      <div className="mt-16 space-y-20 sm:space-y-28">
        <Row
          title="Live transcript, live translation"
          body="Each line shows up as it's said, with its translation underneath. Two-way mode for bilingual meetings."
          visual={
            <div className="space-y-4">
              <Frame>
                <Shot
                  name="transcript"
                  alt="A live transcript with each line in its original language and its translation underneath. Buttons switch between original plus translation, side by side, and translation only."
                  width={848}
                  height={284}
                  sizes="(min-width: 1152px) 600px, 90vw"
                />
              </Frame>
              <Frame className="ml-6 sm:ml-14">
                <Shot
                  name="sidebyside"
                  alt="The same transcript in side-by-side layout, original on the left and translation on the right."
                  width={848}
                  height={212}
                  sizes="(min-width: 1152px) 540px, 80vw"
                />
              </Frame>
            </div>
          }
        />

        <Row
          flip
          title="A subtitle window for your call"
          body="A small always-on-top overlay. Make it see-through and it floats over your call like captions."
          note="See-through mode works on macOS today. Windows 11 is in testing."
          visual={
            <div className="relative">
              <SubtitleScene />
              <div className="absolute top-3 left-3">
                <Pill>See-through</Pill>
              </div>
            </div>
          }
        />

        <Row
          title="Decisions with receipts"
          body="Decisions and action items, with an owner and due date for each. Click one to jump to the line it came from."
          visual={
            <Frame>
              <Shot
                name="decisions"
                alt="A meeting's decisions and action items. Each action item shows an owner and a due date, and finished ones are ticked."
                width={744}
                height={480}
                sizes="(min-width: 1152px) 600px, 90vw"
              />
            </Frame>
          }
        />

        <Row
          flip
          title="One action list for every meeting"
          body="Filter by person or meeting, group by due date, tick things off."
          visual={
            <Frame>
              <Shot
                name="actions"
                alt="The Action items page: every meeting's action items in one list, grouped by meeting, with owner chips, due dates and overdue markers."
                width={744}
                height={600}
                sizes="(min-width: 1152px) 600px, 90vw"
              />
            </Frame>
          }
        />
      </div>

      <ul className="mt-24 grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
        {tiles.map(({ Icon, title, body }) => (
          <li key={title} className="rounded-2xl border border-line bg-card p-6">
            <span className="grid size-10 place-items-center rounded-xl bg-tan/70">
              <Icon aria-hidden="true" className="size-5" />
            </span>
            <h3 className="mt-4 text-lg font-semibold tracking-tight">{title}</h3>
            <p className="mt-1.5 text-muted">{body}</p>
          </li>
        ))}
      </ul>
    </section>
  );
}
