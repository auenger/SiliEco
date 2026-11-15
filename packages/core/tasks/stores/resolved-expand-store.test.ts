import { beforeEach, describe, expect, it } from "vitest";
import { selectExpandedResolved, useResolvedExpandStore } from "./resolved-expand-store";

describe("resolved expand store", () => {
  beforeEach(() => {
    useResolvedExpandStore.setState({ expandedByTask: {} });
  });

  it("setExpanded adds and removes ids per task", () => {
    const { setExpanded } = useResolvedExpandStore.getState();

    setExpanded("task-1", "c1", true);
    setExpanded("task-1", "c2", true);
    setExpanded("task-2", "c9", true);

    const state = useResolvedExpandStore.getState();
    expect([...state.expandedByTask["task-1"]!]).toEqual(["c1", "c2"]);
    expect([...state.expandedByTask["task-2"]!]).toEqual(["c9"]);

    setExpanded("task-1", "c1", false);
    expect([...useResolvedExpandStore.getState().expandedByTask["task-1"]!]).toEqual(["c2"]);
  });

  it("setExpanded drops the task key when its last id is removed", () => {
    const { setExpanded } = useResolvedExpandStore.getState();
    setExpanded("task-1", "c1", true);
    setExpanded("task-1", "c1", false);

    expect(useResolvedExpandStore.getState().expandedByTask).toEqual({});
  });

  it("setExpanded is a no-op when the id is already in the requested state", () => {
    const { setExpanded } = useResolvedExpandStore.getState();
    setExpanded("task-1", "c1", true);
    const before = useResolvedExpandStore.getState().expandedByTask;

    setExpanded("task-1", "c1", true);
    setExpanded("task-1", "missing", false);

    expect(useResolvedExpandStore.getState().expandedByTask).toBe(before);
  });

  it("expandAll unions ids into the task's set and ignores empty input", () => {
    const { setExpanded, expandAll } = useResolvedExpandStore.getState();
    setExpanded("task-1", "c1", true);

    expandAll("task-1", ["c2", "c3", "c2"]);
    expect([...useResolvedExpandStore.getState().expandedByTask["task-1"]!].sort()).toEqual([
      "c1",
      "c2",
      "c3",
    ]);

    const before = useResolvedExpandStore.getState().expandedByTask;
    expandAll("task-1", []);
    expect(useResolvedExpandStore.getState().expandedByTask).toBe(before);
  });

  it("collapseAll clears one task without touching others", () => {
    const { expandAll, collapseAll } = useResolvedExpandStore.getState();
    expandAll("task-1", ["c1", "c2"]);
    expandAll("task-2", ["c9"]);

    collapseAll("task-1");

    const state = useResolvedExpandStore.getState();
    expect(state.expandedByTask["task-1"]).toBeUndefined();
    expect([...state.expandedByTask["task-2"]!]).toEqual(["c9"]);

    const before = state.expandedByTask;
    collapseAll("task-1");
    expect(useResolvedExpandStore.getState().expandedByTask).toBe(before);
  });

  it("selectExpandedResolved returns a stable empty set for untracked tasks", () => {
    const select = selectExpandedResolved("task-1");
    const a = select(useResolvedExpandStore.getState());
    useResolvedExpandStore.getState().setExpanded("task-2", "c9", true);
    const b = select(useResolvedExpandStore.getState());

    expect(a.size).toBe(0);
    expect(b).toBe(a);
  });
});
