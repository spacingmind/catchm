import { FileAudio, FileDown, Languages, RefreshCw, Search, Sparkles, type LucideIcon } from "lucide-react";

const tiles: { Icon: LucideIcon; title: string; body: string }[] = [
  {
    Icon: FileDown,
    title: "Export",
    body: "Copy a meeting, or save it as .txt, Markdown or Word.",
  },
  {
    Icon: Search,
    title: "Search, tags, projects",
    body: "Find any meeting. Group them by project and tag.",
  },
  {
    Icon: FileAudio,
    title: "Import a recording",
    body: "Turn an audio file into a meeting.",
  },
  {
    Icon: Sparkles,
    title: "Refine afterwards",
    body: "Run the recording through a second, more accurate pass.",
  },
  {
    Icon: Languages,
    title: "English and Vietnamese",
    body: "Switch the app itself between the two.",
  },
  {
    Icon: RefreshCw,
    title: "Stays current",
    body: "Windows updates in the app. On macOS, CatchM tells you when a new version is out.",
  },
];

export function Also() {
  return (
    <section aria-labelledby="also-title" className="mx-auto max-w-6xl px-5 py-20 sm:px-8 sm:py-24">
      <p className="eyebrow">And</p>
      <h2 id="also-title" className="display mt-3 max-w-2xl text-4xl sm:text-5xl">
        Everything around it
      </h2>
      <ul className="mt-12 grid gap-5 sm:grid-cols-2 lg:grid-cols-3">
        {tiles.map(({ Icon, title, body }) => (
          <li key={title} className="rounded-2xl border border-line bg-card p-6">
            <span className="grid size-10 place-items-center rounded-xl bg-chip text-chip-fg">
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
