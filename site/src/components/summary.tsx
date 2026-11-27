import { EngineList } from "@/components/engine-chips";
import { Frame, Row, SectionHead } from "@/components/feature-row";
import { Shot } from "@/components/ui/shot";

export function Summary() {
  return (
    <section id="summary" aria-labelledby="summary-title" className="mx-auto max-w-6xl px-5 py-20 sm:px-8 sm:py-28">
      <SectionHead id="summary-title" eyebrow="Summary" title="What was decided, and who does what" stage="beta" />

      <div className="mt-16 space-y-20 sm:space-y-28">
        <Row
          title="A summary you can act on"
          body="A short summary, the decisions, and the action items with an owner and due date. Click one to jump to the line it came from."
          extra={
            <div className="mt-6 max-w-md">
              <EngineList
                title="Who writes it"
                items={[
                  ["Your AI provider", "cloud", "OpenAI, Gemini, Anthropic, Groq, OpenRouter and more, with your own API key."],
                  ["CatchM Cloud", "cloud", "Free to start, hosted for you. Needs a CatchM account."],
                  ["Ollama", "local", "Runs on your computer. Free and private."],
                ]}
              />
            </div>
          }
          note="You create it when a meeting ends. Summaries are in beta: check what matters against the transcript."
          visual={
            <Frame>
              <Shot
                name="decisions"
                alt="A meeting's decisions, its action items with an owner and a due date each, and the start of its written summary."
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

    </section>
  );
}
