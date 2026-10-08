"use client";

import { Download as DownloadIcon, ShieldCheck } from "lucide-react";
import { MacIcon, WindowsIcon } from "@/components/platform-icons";
import { Logo } from "@/components/logo";
import { ButtonLink } from "@/components/ui/button";
import { formatDate, formatSize, type Download as DownloadFile, type Release } from "@/lib/release";
import { useOS } from "@/lib/use-os";
import { cn } from "@/lib/utils";

type Card = {
  id: "windows" | "mac";
  name: string;
  detail: string;
  cta: string;
  file: DownloadFile;
  Icon: typeof WindowsIcon;
  notes: React.ReactNode[];
};

/**
 * Both cards are in the server HTML. After hydration the visitor's
 * platform moves first and gets a "your computer" tag.
 */
export function Download({ release }: { release: Release }) {
  const os = useOS();
  const cards: Card[] = [
    {
      id: "windows",
      name: "Windows",
      detail: "Windows 10 and 11",
      cta: "Download installer",
      file: release.windows,
      Icon: WindowsIcon,
      notes: [
        "Updates itself inside the app.",
        "Captures your microphone and what your PC plays, or both.",
      ],
    },
    {
      id: "mac",
      name: "macOS",
      detail: "Apple Silicon (M1 or later)",
      cta: "Download .dmg",
      file: release.mac,
      Icon: MacIcon,
      notes: [
        "Not signed by Apple yet. The first time, right-click CatchM and choose Open. On macOS 15 and later, use System Settings → Privacy & Security → Open Anyway.",
        "Listens to your microphone; system audio is Windows-only for now.",
      ],
    },
  ];

  return (
    <section id="download" aria-labelledby="download-title" className="bg-deep text-deep-fg">
      <div className="mx-auto max-w-6xl px-5 py-20 sm:px-8 sm:py-28">
        <div className="flex items-center gap-5 sm:gap-7">
          <Logo size={96} className="size-16 shrink-0 -rotate-6 shadow-[0_18px_30px_-12px_rgba(0,0,0,0.5)] sm:size-24" />
          <div>
            <p className="eyebrow text-deep-muted!">Download</p>
            <h2 id="download-title" className="display mt-2 text-4xl sm:text-5xl">
              Get CatchM
            </h2>
          </div>
        </div>
        <p className="mt-6 text-lg text-deep-muted">
          Version {release.version} · {formatDate(release.publishedAt)}
        </p>
        {os === "other" && (
          <p className="mt-3 text-sm text-deep-muted">
            CatchM is a desktop app. Open this page on a Windows or Mac
            computer to install it.
          </p>
        )}

        <div className="mt-10 grid gap-5 md:grid-cols-2">
          {cards.map((c) => (
            <article
              key={c.id}
              data-platform={c.id}
              className={cn(
                "flex min-w-0 flex-col rounded-3xl border border-transparent bg-card p-7 text-ink sm:p-8",
                os === c.id && "order-first",
              )}
            >
              <header className="flex items-start justify-between gap-4">
                <div className="flex items-center gap-3">
                  <span className="grid size-11 place-items-center rounded-xl bg-chip text-chip-fg">
                    <c.Icon className="size-5" />
                  </span>
                  <div>
                    <h3 className="text-xl font-semibold tracking-tight">{c.name}</h3>
                    <p className="text-sm text-subtle">{c.detail}</p>
                  </div>
                </div>
                {os === c.id && (
                  <span className="shrink-0 whitespace-nowrap rounded-full bg-primary px-2.5 py-1 text-xs font-medium text-primary-fg">
                    Your computer
                  </span>
                )}
              </header>

              <ButtonLink href={c.file.url} download className="mt-7 w-full">
                <DownloadIcon aria-hidden="true" className="size-4" />
                {c.cta}
                <span className="font-normal">· {formatSize(c.file.size)}</span>
              </ButtonLink>

              <p className="mt-3 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-sm text-subtle">
                <ShieldCheck aria-hidden="true" className="size-4" />
                <a
                  href={c.file.sha256Url}
                  className="underline underline-offset-2 hover:text-ink"
                >
                  SHA-256 checksum
                </a>
                <span aria-hidden="true">·</span>
                <span className="break-all">{c.file.name}</span>
              </p>

              <ul className="mt-6 space-y-3 border-t border-line pt-5 text-sm text-muted">
                {c.notes.map((n, i) => (
                  <li key={i}>{n}</li>
                ))}
              </ul>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}
