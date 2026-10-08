"use client";

import { Download as DownloadIcon } from "lucide-react";
import { MacIcon, WindowsIcon } from "@/components/platform-icons";
import { ButtonLink } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useOS } from "@/lib/use-os";
import type { Release } from "@/lib/release";

/**
 * Hero call to action. The server HTML carries both buttons with equal
 * weight; once the OS is known the matching one leads and the other
 * steps back. No layout is hidden behind JS.
 */
export function DownloadButtons({ release }: { release: Release }) {
  const os = useOS();
  const known = os === "windows" || os === "mac";
  const buttons = [
    {
      id: "windows" as const,
      href: release.windows.url,
      label: "Download for Windows",
      Icon: WindowsIcon,
    },
    {
      id: "mac" as const,
      href: release.mac.url,
      label: "Download for macOS",
      Icon: MacIcon,
    },
  ];

  return (
    <div className="flex w-full max-w-sm flex-col gap-3 lg:max-w-none">
      {buttons.map(({ id, href, label, Icon }) => (
        <ButtonLink
          key={id}
          href={href}
          download
          variant={known && os !== id ? "outline" : "primary"}
          className={cn("w-full justify-between", known && os === id && "order-first")}
          data-platform={id}
        >
          <Icon className="size-[1.1rem]" />
          {label}
          <DownloadIcon aria-hidden="true" className="size-4 opacity-70" />
        </ButtonLink>
      ))}
    </div>
  );
}
