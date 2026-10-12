import type { MetadataRoute } from "next";

/** Public site has not passed publication acceptance; never expose Console routes to crawlers. */
export default function robots(): MetadataRoute.Robots {
  return { rules: { userAgent: "*", disallow: "/" } };
}
