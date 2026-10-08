import type { NextConfig } from "next";

// Fully static: `next build` writes the site to out/, which the Worker in
// worker/index.ts serves as static assets. Nothing here needs a server.
const nextConfig: NextConfig = {
  output: "export",
  images: { unoptimized: true },
  poweredByHeader: false,
};

export default nextConfig;
