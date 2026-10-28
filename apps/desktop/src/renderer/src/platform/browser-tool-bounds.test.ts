import { describe, expect, it } from "vitest";
import { browserBoundsBesideChat } from "./browser-tool-bounds";

describe("native browser beside chat", () => {
  const surface = { x: 200, y: 100, width: 1000, height: 700 };
  it("keeps the website visible to the left of the agent conversation", () => {
    expect(browserBoundsBesideChat(surface, { x: 750, y: 250, width: 430, height: 530 })).toEqual({ ...surface, width: 542 });
  });
  it("keeps the launcher clickable without removing the website", () => {
    expect(browserBoundsBesideChat(surface, { x: 1130, y: 720, width: 48, height: 48 })).toEqual({ ...surface, width: 922 });
  });
  it("leaves unrelated overlays alone and hides only when no usable space remains", () => {
    expect(browserBoundsBesideChat(surface, null)).toEqual(surface);
    expect(browserBoundsBesideChat(surface, { x: 1220, y: 250, width: 430, height: 530 })).toEqual(surface);
    expect(browserBoundsBesideChat(surface, { x: 300, y: 150, width: 800, height: 600 })).toBeNull();
  });
});
