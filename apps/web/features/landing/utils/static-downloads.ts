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
const version = process.env.SILIECO_DESKTOP_VERSION?.trim() || "internal";

function asset(name: string): string {
  return `${DOWNLOAD_BASE}/${name}`;
}

export function getStaticDownloadRelease(): StaticDownloadRelease {
  return {
    version,
    publishedAt: new Date().toISOString(),
    htmlUrl: `${DOWNLOAD_BASE}/`,
    assets: {
      macArm64Dmg: asset("silieco-desktop-mac-arm64.dmg"),
      macArm64Zip: asset("silieco-desktop-mac-arm64.zip"),
      macX64Dmg: asset("silieco-desktop-mac-x64.dmg"),
      macX64Zip: asset("silieco-desktop-mac-x64.zip"),
      winX64Zip: asset("silieco-desktop-windows-x64.zip"),
    },
  };
}
