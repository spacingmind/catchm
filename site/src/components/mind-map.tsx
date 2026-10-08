import { Frame, Row } from "@/components/feature-row";
import { MindMapWindow } from "@/components/mind-map-mock";

export function MindMap() {
  return (
    <section id="mindmap" aria-labelledby="mindmap-title" className="border-y border-line bg-bg-alt">
      <div className="mx-auto max-w-6xl px-5 py-20 sm:px-8 sm:py-28">
        <div>
          <Row
            id="mindmap-title"
            wide
            eyebrow="Mind map"
            title="See the shape of the meeting"
            body="Topics branch into the decisions and action items under them. Fold a topic, or click an item to see where it was said."
            extra={
              <p className="mt-4 max-w-md text-muted">
                Need the steps of a process? &ldquo;Find flows&rdquo; pulls those out too, on request.
              </p>
            }
            note="Made from the summary, so it needs a summary written by Ollama or your own AI service. CatchM Cloud's can't draw one yet."
            visual={
              <Frame className="bg-card p-3 sm:p-4">
                <MindMapWindow />
                <p className="sr-only">
                  Illustration of the Mind map tab: a meeting called Q3 Roadmap Sync branches into two topics,
                  Pricing tiers and Onboarding redesign, each with its decisions and action items.
                </p>
              </Frame>
            }
          />
        </div>
      </div>
    </section>
  );
}
