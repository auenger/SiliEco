import type { MetadataRoute } from "next";

export default function robots(): MetadataRoute.Robots {
  const baseUrl = "https://www.silieco.ai";

  return {
    rules: [
      {
        userAgent: "*",
        allow: ["/"],
        disallow: [
          "/api/",
          "/ws",
          "/auth/",
          "/issues",
          "/board",
          "/inbox",
          "/agents",
          "/settings",
          "/my-issues",
          "/runtimes",
          "/skills",
        ],
      },
    ],
    sitemap: `${baseUrl}/sitemap.xml`,
  };
}
