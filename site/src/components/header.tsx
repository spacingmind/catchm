import { Logo } from "@/components/logo";
import { GitHubIcon } from "@/components/platform-icons";
import { ButtonLink } from "@/components/ui/button";
import { siteConfig } from "@/lib/site";

const links = [
  { href: "#summary", label: "Summary" },
  { href: "#mindmap", label: "Mind map" },
  { href: "#privacy", label: "Privacy" },
  { href: "#live", label: "Live" },
];

export function Header() {
  return (
    <header className="sticky top-0 z-40 border-b border-line/70 bg-bg/85 backdrop-blur-md">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-5 sm:px-8">
        <a href="#top" className="flex items-center gap-2.5 font-semibold tracking-tight">
          <Logo size={32} />
          <span className="text-lg">CatchM</span>
        </a>
        <nav aria-label="Main" className="flex items-center gap-1">
          <ul className="hidden items-center gap-1 md:flex">
            {links.map((l) => (
              <li key={l.href}>
                <a
                  href={l.href}
                  className="rounded-lg px-3 py-2 text-sm text-muted transition-colors hover:text-ink"
                >
                  {l.label}
                </a>
              </li>
            ))}
          </ul>
          <a
            href={siteConfig.repo}
            className="rounded-lg p-2 text-muted transition-colors hover:text-ink"
            aria-label="CatchM on GitHub"
            title="Source code on GitHub"
          >
            <GitHubIcon className="size-5" />
          </a>
          <ButtonLink href="#download" size="md" className="ml-2">
            Download
          </ButtonLink>
        </nav>
      </div>
    </header>
  );
}
