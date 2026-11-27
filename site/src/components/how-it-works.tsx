import { FolderOpen, KeyRound, Mic } from "lucide-react";

const steps = [
  {
    Icon: KeyRound,
    title: "Pick your setup",
    body: "Paste an API key from Soniox, Deepgram, ElevenLabs or another service. Or self-host everything with Whisper and Ollama.",
  },
  {
    Icon: Mic,
    title: "Hold the meeting",
    body: "Start CatchM before you join. It listens on your computer, so no bot joins your call.",
  },
  {
    Icon: FolderOpen,
    title: "Keep what matters",
    body: "When it ends, CatchM writes the summary, decisions and action items, saved on your computer as Markdown.",
  },
];

export function HowItWorks() {
  return (
    <section id="how" aria-labelledby="how-title" className="border-y border-line bg-bg-alt">
      <div className="mx-auto max-w-6xl px-5 py-20 sm:px-8 sm:py-24">
        <p className="eyebrow">How it works</p>
        <h2 id="how-title" className="display mt-3 max-w-xl text-4xl sm:text-5xl">
          Bring a key. Talk. Keep what matters.
        </h2>
        <ol className="mt-14 grid gap-10 md:grid-cols-3 md:gap-8">
          {steps.map(({ Icon, title, body }, i) => (
            <li key={title} className="relative">
              <span
                aria-hidden="true"
                className="display block text-7xl leading-none text-numeral select-none"
              >
                {i + 1}
              </span>
              <div className="mt-4 flex items-center gap-2.5">
                <Icon aria-hidden="true" className="size-5" />
                <h3 className="text-xl font-semibold tracking-tight">{title}</h3>
              </div>
              <p className="mt-2 max-w-xs text-muted">{body}</p>
            </li>
          ))}
        </ol>
      </div>
    </section>
  );
}
