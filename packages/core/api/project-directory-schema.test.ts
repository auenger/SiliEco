import { describe, expect, it } from "vitest";
import { ProjectDirectorySchema } from "./project-directory-schema";
import { parseWithFallback } from "./schema";
describe("project directory response", () => {
  it("rejects malformed payloads and unsafe entry names", () => {
    for (const raw of [
      null,
      {},
      {
        entries: [{ name: "../secret", directory: false, size: 1 }],
        next_offset: 0,
        content: "",
        mime: "",
      },
      { entries: [], next_offset: -1, content: "", mime: "" },
    ]) {
      expect(
        parseWithFallback(raw, ProjectDirectorySchema, null, {
          endpoint: "test",
        }),
      ).toBeNull();
    }
  });
  it("accepts an empty folder and preview chunks", () => {
    expect(
      ProjectDirectorySchema.safeParse({
        entries: [],
        next_offset: 0,
        content: "",
        mime: "text/markdown",
      }).success,
    ).toBe(true);
  });
});
