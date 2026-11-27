import { siteConfig } from "@/lib/site";
import { HardDrive, KeyRound, LockKeyhole, MicOff, type LucideIcon } from "lucide-react";

const points: { Icon: LucideIcon; title: string; body: string }[] = [
  {
    Icon: MicOff,
    title: "No bot in your call",
    body: "CatchM listens on your own computer. Nobody has to let it in.",
  },
  {
    Icon: HardDrive,
    title: "Your files stay here",
    body: "Meetings, recordings, settings and API keys live in a folder on your computer.",
  },
  {
    Icon: KeyRound,
    title: "Your keys, your accounts",
    body: "With your own API key, audio goes from your computer straight to the service you picked, never through CatchM. Or self-host it all.",
  },
  {
    Icon: LockKeyhole,
    title: "Sync only if you want it",
    body: "Sign up to sync across your devices. Meetings are locked with your password before they leave this computer, so CatchM can't read them.",
  },
];

export function Privacy() {
  return (
    <section id="privacy" aria-labelledby="privacy-title" className="border-y border-line bg-bg-alt">
      <div className="mx-auto max-w-6xl px-5 py-20 sm:px-8 sm:py-24">
        <p className="eyebrow">Privacy</p>
        <h2 id="privacy-title" className="display mt-3 max-w-2xl text-4xl sm:text-5xl">
          Your files, your keys. Sync only by choice.
        </h2>
        <ul className="mt-12 grid gap-10 md:grid-cols-2 md:gap-8 lg:grid-cols-4">
          {points.map(({ Icon, title, body }) => (
            <li key={title}>
              <Icon aria-hidden="true" className="size-6" />
              <h3 className="mt-4 text-xl font-semibold tracking-tight">{title}</h3>
              <p className="mt-2 max-w-sm text-muted">{body}</p>
            </li>
          ))}
        </ul>
        <p className="mt-10 max-w-2xl text-sm text-subtle">
          Self-hosted engines send nothing off your computer; a cloud service
          sees the audio or text it works on, under your own account. That includes summaries made with CatchM
          Cloud, which send the transcript unencrypted to be summarised. It
          isn&rsquo;t stored.
        </p>
        <p className="mt-3 max-w-2xl text-sm text-subtle">
          Don&rsquo;t take our word for it: CatchM is open source under the MIT
          licence.{" "}
          <a href={siteConfig.repo} className="font-medium text-ink underline underline-offset-4">
            Read the code on GitHub
          </a>
          .
        </p>
      </div>
    </section>
  );
}
