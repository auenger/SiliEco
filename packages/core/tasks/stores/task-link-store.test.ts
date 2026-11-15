// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useTaskLinkStore } from "./task-link-store";

// Node 25 ships a partial `localStorage` shim under jsdom that's missing
// `clear`/`removeItem`; replace it with a real in-memory Storage so persist
// can round-trip values.
beforeAll(() => {
  if (typeof globalThis.localStorage?.setItem !== "function") {
    const values = new Map<string, string>();
    const storage: Storage = {
      get length() { return values.size; },
      clear: () => values.clear(),
      getItem: (k) => values.get(k) ?? null,
      key: (i) => Array.from(values.keys())[i] ?? null,
      removeItem: (k) => { values.delete(k); },
      setItem: (k, v) => { values.set(k, v); },
    };
    Object.defineProperty(globalThis, "localStorage", { configurable: true, value: storage });
    Object.defineProperty(window, "localStorage", { configurable: true, value: storage });
  }
});

describe("task link store", () => {
  beforeEach(() => {
    useTaskLinkStore.setState({ openInNewTab: true });
  });

  it("defaults to opening task links in a new tab", () => {
    expect(useTaskLinkStore.getState().openInNewTab).toBe(true);
  });

  it("setOpenInNewTab switches to in-place navigation and back", () => {
    useTaskLinkStore.getState().setOpenInNewTab(false);
    expect(useTaskLinkStore.getState().openInNewTab).toBe(false);

    useTaskLinkStore.getState().setOpenInNewTab(true);
    expect(useTaskLinkStore.getState().openInNewTab).toBe(true);
  });
});
