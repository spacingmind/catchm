import { Clock } from "lucide-react";

const tree = [
  ["meeting.md", "summary + YAML frontmatter"],
  ["analysis.json", "topics and evidence"],
  ["utterances.jsonl", "the live transcript"],
  ["utterances.v2.jsonl", "the refined transcript"],
  ["audio.wav", "the recording"],
];

export function Files() {
  return (
    <section id="files" aria-labelledby="files-title" className="bg-deep text-deep-fg">
      <div className="mx-auto grid max-w-6xl items-center gap-12 px-5 py-20 sm:px-8 sm:py-28 lg:grid-cols-2 lg:gap-16">
        <div>
          <p className="eyebrow text-deep-muted!">Agent-readable</p>
          <h2 id="files-title" className="display mt-3 text-4xl sm:text-5xl">
            Your meetings are plain files.
          </h2>
          <p className="mt-5 max-w-md text-lg text-deep-muted">
            One folder per meeting: Markdown with YAML frontmatter, next to the
            raw transcript. Point your own tools or agents at it.
          </p>
          <p className="mt-8 inline-flex items-center gap-2 rounded-full border border-deep-line px-3.5 py-1.5 text-sm text-deep-muted">
            <Clock aria-hidden="true" className="size-4" />
            MCP server: coming soon
          </p>
        </div>

        <figure
          className="overflow-hidden rounded-2xl border border-deep-line bg-deep-card text-left"
          aria-label="Example of a meeting folder and its meeting.md file"
        >
          <div className="border-b border-deep-line px-5 py-3.5 font-mono text-[0.8rem] text-deep-muted">
            ~/.catchm/meetings/&lt;id&gt;/
          </div>
          <ul className="space-y-1 px-5 py-4 font-mono text-[0.8rem] leading-relaxed">
            {tree.map(([name, note]) => (
              <li key={name} className="flex flex-col sm:flex-row sm:gap-x-4">
                <span className="min-w-[10.5rem]">{name}</span>
                <span className="text-deep-muted"># {note}</span>
              </li>
            ))}
          </ul>
          <pre tabIndex={0} aria-label="Example meeting.md frontmatter" className="overflow-x-auto border-t border-deep-line px-5 py-4 font-mono text-[0.8rem] leading-relaxed">
            <code>
              <span className="text-deep-muted">---</span>
              {"\n"}title: Q3 Roadmap Sync
              {"\n"}started_at: 2026-09-25T12:18:41+07:00
              {"\n"}langs: [en, vi]
              {"\n"}tags: [planning, roadmap]
              {"\n"}decisions:
              {"\n"}  - text: Ship the new pricing tiers in Q3
              {"\n"}    scope: pricing
              {"\n"}action_items:
              {"\n"}  - text: Circulate onboarding discovery brief
              {"\n"}    owner: Bao Tran
              {"\n"}    due: &quot;2026-09-10&quot;
              {"\n"}<span className="text-deep-muted">---</span>
            </code>
          </pre>
        </figure>
      </div>
    </section>
  );
}
