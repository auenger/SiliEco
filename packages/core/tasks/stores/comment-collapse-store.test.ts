// @vitest-environment jsdom
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import { useCommentCollapseStore } from "./comment-collapse-store";

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

describe("comment collapse store", () => {
  beforeEach(() => {
    useCommentCollapseStore.setState({ collapsedByTask: {} });
  });

  it("toggle collapses and expands a single comment", () => {
    const { toggle } = useCommentCollapseStore.getState();

    toggle("task-1", "c1");
    expect(useCommentCollapseStore.getState().isCollapsed("task-1", "c1")).toBe(true);
    expect(useCommentCollapseStore.getState().isCollapsed("task-1", "c2")).toBe(false);

    toggle("task-1", "c1");
    expect(useCommentCollapseStore.getState().isCollapsed("task-1", "c1")).toBe(false);
    expect(useCommentCollapseStore.getState().collapsedByTask).toEqual({});
  });

  it("collapseAll replaces the task's collapsed set and dedupes ids", () => {
    const { toggle, collapseAll } = useCommentCollapseStore.getState();
    toggle("task-1", "stale");
    toggle("task-2", "kept");

    collapseAll("task-1", ["c1", "c2", "c1"]);

    const state = useCommentCollapseStore.getState();
    expect(state.collapsedByTask["task-1"]).toEqual(["c1", "c2"]);
    expect(state.isCollapsed("task-1", "stale")).toBe(false);
    expect(state.isCollapsed("task-2", "kept")).toBe(true);
  });

  it("collapseAll with no ids clears the task entry", () => {
    const { collapseAll } = useCommentCollapseStore.getState();
    collapseAll("task-1", ["c1"]);

    collapseAll("task-1", []);
    expect(useCommentCollapseStore.getState().collapsedByTask).toEqual({});

    const before = useCommentCollapseStore.getState().collapsedByTask;
    collapseAll("task-1", []);
    expect(useCommentCollapseStore.getState().collapsedByTask).toBe(before);
  });

  it("expandAll clears one task without touching others", () => {
    const { collapseAll, expandAll } = useCommentCollapseStore.getState();
    collapseAll("task-1", ["c1", "c2"]);
    collapseAll("task-2", ["c9"]);

    expandAll("task-1");

    const state = useCommentCollapseStore.getState();
    expect(state.collapsedByTask["task-1"]).toBeUndefined();
    expect(state.collapsedByTask["task-2"]).toEqual(["c9"]);

    const before = state.collapsedByTask;
    expandAll("task-1");
    expect(useCommentCollapseStore.getState().collapsedByTask).toBe(before);
  });
});
