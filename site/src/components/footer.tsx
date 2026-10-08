import { Logo } from "@/components/logo";
import { siteConfig } from "@/lib/site";

const link =
  "underline-offset-4 transition-colors hover:text-ink hover:underline";

export function Footer() {
  return (
    <footer className="border-t border-line bg-bg-alt">
      <div className="mx-auto flex max-w-6xl flex-col gap-8 px-5 py-12 sm:px-8 md:flex-row md:items-start md:justify-between">
        <div className="max-w-xs">
          <div className="flex items-center gap-2.5 font-semibold tracking-tight">
            <Logo size={28} />
            <span className="text-lg">CatchM</span>
          </div>
          <p className="display mt-3 text-lg text-muted">{siteConfig.tagline}</p>
        </div>
        <div className="space-y-3 text-sm text-subtle md:text-right">
          <p>
            Made by{" "}
            <a href={siteConfig.maker.url} className={`font-medium text-ink ${link}`}>
              {siteConfig.maker.name}
            </a>
            {" · "}
            <a href={siteConfig.maker.contact} className={link}>
              Contact
            </a>
          </p>
          <p>
            Also from Spacing Mind:{" "}
            <a href={siteConfig.maker.url} className={link}>
              sMind
            </a>
          </p>
          <p>© {new Date().getFullYear()} Spacing Mind</p>
        </div>
      </div>
    </footer>
  );
}
