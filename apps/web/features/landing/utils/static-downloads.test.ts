import { afterAll, afterEach, describe, expect, it, vi } from "vitest";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { getStaticDownloadRelease } from "./static-downloads";

const directory = mkdtempSync(join(tmpdir(), "silieco-downloads-"));
afterAll(() => rmSync(directory, { recursive: true, force: true }));
afterEach(() => {
  vi.unstubAllEnvs();
  rmSync(join(directory, "silieco-desktop-mac-x64.zip"), { force: true });
});

describe("internal release downloads", () => {
  it("only offers files available in the configured release directory", () => {
    vi.stubEnv("SILIECO_DOWNLOAD_DIR", directory);
    vi.stubEnv("SILIECO_DESKTOP_VERSION", "task-run-v1");
    writeFileSync(join(directory, "silieco-desktop-mac-x64.zip"), "fixture");
    expect(getStaticDownloadRelease()).toMatchObject({
      version: "task-run-v1",
      assets: { macX64Zip: "/downloads/silieco-desktop-mac-x64.zip" },
    });
    expect(Object.keys(getStaticDownloadRelease().assets)).toHaveLength(1);
  });

  it("offers no stale download links when no current package exists", () => {
    vi.stubEnv("SILIECO_DOWNLOAD_DIR", directory);
    expect(getStaticDownloadRelease().assets).toEqual({});
  });
});
