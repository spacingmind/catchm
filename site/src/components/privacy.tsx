import { HardDrive, LockKeyhole, MicOff, type LucideIcon } from "lucide-react";

const points: { Icon: LucideIcon; title: string; body: string }[] = [
  {
    Icon: MicOff,
    title: "No bot in your call",
    body: "CatchM listens on your own computer. Nobody has to let it in.",
  },
  {
    Icon: HardDrive,
    title: "Stays here by default",
    body: "Meetings, recordings and settings live in a folder on your computer.",
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
          Local first. Sync only by choice.
        </h2>
        <ul className="mt-12 grid gap-10 md:grid-cols-3 md:gap-8">
          {points.map(({ Icon, title, body }) => (
            <li key={title}>
              <Icon aria-hidden="true" className="size-6" />
              <h3 className="mt-4 text-xl font-semibold tracking-tight">{title}</h3>
              <p className="mt-2 max-w-sm text-muted">{body}</p>
            </li>
          ))}
        </ul>
        <p className="mt-10 max-w-2xl text-sm text-muted">
          One exception: summaries made with CatchM Cloud send that meeting&rsquo;s
          transcript unencrypted to be summarised. It isn&rsquo;t stored.
        </p>
      </div>
    </section>
  );
}
