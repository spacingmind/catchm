import { Cloud, House, SlidersHorizontal, type LucideIcon } from "lucide-react";

type Where = "local" | "cloud" | "either";

const where: Record<Where, { label: string; Icon: LucideIcon }> = {
  local: { label: "On your computer", Icon: House },
  cloud: { label: "Cloud", Icon: Cloud },
  either: { label: "Your choice", Icon: SlidersHorizontal },
};

const groups: { title: string; options: [string, Where, string][] }[] = [
  {
    title: "Speech to text",
    options: [
      ["Soniox", "cloud", "Labels who's speaking and translates as it goes. Bring your own key."],
      ["Whisper", "local", "Runs through the whisper.cpp server."],
    ],
  },
  {
    title: "Translation",
    options: [
      ["Soniox", "cloud", "Built in, live, one-way or two-way."],
      ["Ollama", "local", "A local translation model."],
    ],
  },
  {
    title: "Summaries",
    options: [
      ["Ollama", "local", "The default."],
      ["CatchM Cloud", "cloud", "Hosted for you on Cloudflare Workers AI."],
      ["Your own", "either", "Any OpenAI-compatible service."],
    ],
  },
];

export function Engines() {
  return (
    <section aria-labelledby="engines-title" className="mx-auto max-w-6xl px-5 py-20 sm:px-8 sm:py-28">
      <p className="eyebrow">Engines</p>
      <h2 id="engines-title" className="display mt-3 max-w-2xl text-4xl sm:text-5xl">
        Bring your own engines
      </h2>
      <div className="mt-12 grid gap-5 md:grid-cols-3">
        {groups.map((g) => (
          <div key={g.title} className="rounded-3xl border border-line bg-card p-7">
            <h3 className="text-lg font-semibold tracking-tight">{g.title}</h3>
            <dl className="mt-5 space-y-5">
              {g.options.map(([name, w, body]) => {
                const { label, Icon } = where[w];
                return (
                  <div key={name} className="border-t border-line pt-4 first:border-t-0 first:pt-0">
                    <dt className="flex flex-wrap items-center justify-between gap-2 font-medium">
                      {name}
                      <span className="inline-flex items-center gap-1 rounded-full bg-tan/70 px-2 py-0.5 text-xs font-medium">
                        <Icon aria-hidden="true" className="size-3" />
                        {label}
                      </span>
                    </dt>
                    <dd className="mt-1.5 text-sm text-muted">{body}</dd>
                  </div>
                );
              })}
            </dl>
          </div>
        ))}
      </div>
      <p className="mt-6 max-w-2xl text-sm text-muted">
        A cloud engine sees the audio or text it works on. Local ones send
        nothing off your computer.
      </p>
    </section>
  );
}
