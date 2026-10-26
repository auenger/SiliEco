import type { MetadataRoute } from "next";
export default function sitemap(): MetadataRoute.Sitemap {
  return [
    {
      url: "https://www.silieco.ai",
      lastModified: new Date("2026-10-26"),
      changeFrequency: "monthly",
      priority: 1,
    },
  ];
}
