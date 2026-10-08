import type { Metadata, Viewport } from "next";
import "./globals.css";
import { siteConfig } from "@/lib/site";

const title = `${siteConfig.name}: ${siteConfig.tagline}`;

export const metadata: Metadata = {
  metadataBase: new URL(siteConfig.url),
  title,
  description: siteConfig.description,
  applicationName: siteConfig.name,
  authors: [{ name: siteConfig.maker.name, url: siteConfig.maker.url }],
  creator: siteConfig.maker.name,
  publisher: siteConfig.maker.name,
  keywords: [
    "meeting transcription",
    "live translation",
    "meeting notes",
    "action items",
    "local-first",
    "Windows",
    "macOS",
  ],
  alternates: { canonical: "/" },
  openGraph: {
    type: "website",
    url: siteConfig.url,
    siteName: siteConfig.name,
    title,
    description: siteConfig.description,
    locale: "en_US",
  },
  twitter: { card: "summary_large_image", title, description: siteConfig.description },
  robots: { index: true, follow: true },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#fbf7ea" },
    { media: "(prefers-color-scheme: dark)", color: "#1e1411" },
  ],
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body>
        <a
          href="#main"
          className="sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-50 focus:rounded-lg focus:bg-primary focus:px-4 focus:py-2 focus:text-primary-fg"
        >
          Skip to content
        </a>
        {children}
      </body>
    </html>
  );
}
