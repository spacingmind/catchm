import { Download } from "@/components/download";
import { Also } from "@/components/also";
import { Files } from "@/components/files";
import { Footer } from "@/components/footer";
import { Header } from "@/components/header";
import { Hero } from "@/components/hero";
import { HowItWorks } from "@/components/how-it-works";
import { Live } from "@/components/live";
import { MindMap } from "@/components/mind-map";
import { Privacy } from "@/components/privacy";
import { Summary } from "@/components/summary";
import { getRelease } from "@/lib/release";
import { siteConfig } from "@/lib/site";

export default async function Page() {
  const release = await getRelease();

  // Escape "<" so the JSON can't close the script tag.
  const jsonLd = JSON.stringify({
    "@context": "https://schema.org",
    "@type": "SoftwareApplication",
    name: siteConfig.name,
    description: siteConfig.description,
    url: siteConfig.url,
    image: `${siteConfig.url}/opengraph-image.png`,
    applicationCategory: "BusinessApplication",
    operatingSystem: "Windows 10, Windows 11, macOS (Apple Silicon)",
    softwareVersion: release.version,
    datePublished: release.publishedAt,
    inLanguage: "en",
    downloadUrl: [release.windows.url, release.mac.url],
    publisher: { "@type": "Organization", name: siteConfig.maker.name, url: siteConfig.maker.url },
  }).replace(/</g, "\\u003c");

  return (
    <>
      <Header />
      <main id="main">
        <Hero release={release} />
        <HowItWorks />
        <Summary />
        <MindMap />
        <Files />
        <Privacy />
        <Also />
        <Live />
        <Download release={release} />
      </main>
      <Footer />
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: jsonLd }} />
    </>
  );
}
