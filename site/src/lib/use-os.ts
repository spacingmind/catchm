"use client";

import { useSyncExternalStore } from "react";

export type OS = "windows" | "mac" | "other" | "unknown";

function detect(): OS {
  const ua = navigator.userAgent;
  // Phones and tablets (iPadOS claims to be a Mac) can't run CatchM.
  if (/iPhone|iPad|iPod|Android/i.test(ua)) return "other";
  if (/Macintosh|Mac OS X/i.test(ua) && navigator.maxTouchPoints > 1) {
    return "other";
  }
  const nav = navigator as Navigator & { userAgentData?: { platform?: string } };
  const platform = (nav.userAgentData?.platform || navigator.platform || "").toLowerCase();
  if (platform.includes("win") || /Windows/i.test(ua)) return "windows";
  if (platform.includes("mac") || /Macintosh/i.test(ua)) return "mac";
  return "other";
}

const subscribe = () => () => {};

/**
 * The visitor's OS. The server snapshot is "unknown", so the static HTML
 * and the first client render match; the real value lands right after
 * hydration with no mismatch.
 */
export function useOS(): OS {
  return useSyncExternalStore(subscribe, detect, () => "unknown");
}
