import { DownloadButtons } from "@/components/download-buttons";
import { StatusTag } from "@/components/feature-row";
import { MindMapMock } from "@/components/mind-map-mock";
import { Shot } from "@/components/ui/shot";
import { formatDate, type Release } from "@/lib/release";
import { siteConfig } from "@/lib/site";

const delay = (ms: number) => ({ "--d": `${ms}ms` }) as React.CSSProperties;

export function Hero({ release }: { release: Release }) {
  return (
    <section id="top" className="relative isolate overflow-clip">
      {/* Soft chestnut glow, top right. Decorative only. */}
      <div
        aria-hidden="true"
        className="pointer-events-none absolute -top-48 -right-48 -z-10 size-[52rem] rounded-full opacity-70 blur-3xl"
        style={{
          background:
            "radial-gradient(closest-side, var(--glow), transparent)",
        }}
      />
      <div className="mx-auto max-w-6xl px-5 pt-14 pb-28 sm:px-8 sm:pt-20 sm:pb-32">
        <div className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_19rem] lg:items-end lg:gap-16">
          <div>
            <p className="eyebrow rise">Open source · For Windows and macOS</p>
            <h1
              className="display rise mt-5 text-[clamp(2.9rem,7.4vw,5.6rem)]"
              style={delay(60)}
            >
              Catch the meeting.
              <br />
              <span className="text-h1-soft">
                Keep the mind.
              </span>
            </h1>
            <p className="rise mt-6 max-w-lg text-lg leading-relaxed text-muted" style={delay(140)}>
              Every meeting becomes a live transcript, a summary and action
              items, kept as files you own. Bring your own API key, or run it
              all on your computer.
            </p>
          </div>

          <div className="rise lg:pb-2" style={delay(220)}>
            <DownloadButtons release={release} />
            <p className="mt-4 text-sm text-subtle">
              v{release.version} · {formatDate(release.publishedAt)}
              <br />
              Windows 10/11 · macOS on Apple Silicon
              <br />
              <a href={siteConfig.repo} className="underline underline-offset-4 transition-colors hover:text-ink">
                Source on GitHub
              </a>{" "}
              · {siteConfig.license.name} licence
            </p>
          </div>
        </div>

        <HeroStage />
      </div>
    </section>
  );
}

function HeroStage() {
  return (
    <div className="rise relative mt-14 sm:mt-20" style={delay(300)}>
      <div className="lift overflow-hidden rounded-2xl border border-line-strong/70 bg-card">
        <div
          className="flex h-9 items-center gap-1.5 border-b border-line bg-bg-alt px-4"
          aria-hidden="true"
        >
          <span className="size-2.5 rounded-full bg-line-strong/80" />
          <span className="size-2.5 rounded-full bg-line-strong/80" />
          <span className="size-2.5 rounded-full bg-line-strong/80" />
        </div>
        <div className="aspect-[5/4] overflow-hidden sm:aspect-[16/9]">
          <Shot
            name="overview"
            alt="CatchM's meeting page for a Q3 roadmap sync, with its decisions, action items with owners and due dates, and a summary."
            width={1280}
            height={800}
            priority
            sizes="(min-width: 1152px) 1152px, 100vw"
            className="h-full max-w-none object-cover object-top max-sm:!w-[190%] max-sm:-ml-[57%] max-sm:!h-auto"
          />
        </div>
      </div>

      <figure className="absolute -bottom-24 left-4 w-[78%] sm:-right-4 sm:bottom-[-3.5rem] sm:left-auto sm:w-[44%]">
        <div className="lift overflow-hidden rounded-xl border border-line-strong/70 bg-card">
          <MindMapMock className="block h-auto w-full" />
        </div>
        <figcaption className="mt-2.5 flex items-center gap-2 text-xs font-medium text-subtle sm:justify-end">
          <span className="sr-only">Illustration: </span>
          The same meeting as a mind map
          <StatusTag stage="soon" />
        </figcaption>
      </figure>
    </div>
  );
}
