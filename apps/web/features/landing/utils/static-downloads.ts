import { existsSync } from "node:fs";
import { join } from "node:path";
import type { DownloadAssets } from "./parse-release-assets";

/**
 * Internal HTTP deployment downloads. The files are copied into
 * apps/web/public/downloads during the release packaging step and are served
 * by the same Web origin, so the page does not depend on GitHub or CORS.
 */
export interface StaticDownloadRelease {
  version: string;
  publishedAt: string;
  htmlUrl: string;
  assets: DownloadAssets;
}

const DOWNLOAD_BASE = "/downloads";
const assetFiles = {
  macArm64Dmg: "silieco-desktop-mac-arm64.dmg",
  macArm64Zip: "silieco-desktop-mac-arm64.zip",
  macX64Dmg: "silieco-desktop-mac-x64.dmg",
  macX64Zip: "silieco-desktop-mac-x64.zip",
  winX64Zip: "silieco-desktop-windows-x64.zip",
} satisfies Partial<Record<keyof DownloadAssets, string>>;

export function getStaticDownloadRelease(): StaticDownloadRelease {
  const directory = process.env.SILIECO_DOWNLOAD_DIR || join(process.cwd(), "public/downloads");
  const assets: DownloadAssets = {};
  for (const [key, filename] of Object.entries(assetFiles)) {
    if (existsSync(join(directory, filename))) {
      assets[key as keyof DownloadAssets] = `${DOWNLOAD_BASE}/${filename}`;
    }
  }
  return {
    version: process.env.SILIECO_DESKTOP_VERSION?.trim() || "internal",
    publishedAt: new Date().toISOString(),
    htmlUrl: `${DOWNLOAD_BASE}/`,
    assets,
  };
}
