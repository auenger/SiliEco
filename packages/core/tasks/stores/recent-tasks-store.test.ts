import { beforeEach, describe, expect, it } from "vitest";
import { useRecentTasksStore, selectRecentTasks } from "./recent-tasks-store";

beforeEach(() => {
  useRecentTasksStore.setState({ byWorkspace: {} });
});

describe("useRecentTasksStore.recordVisit", () => {
  it("keeps visits namespaced by workspace id", () => {
    const { recordVisit } = useRecentTasksStore.getState();
    recordVisit("ws-a", "task-1");
    recordVisit("ws-b", "task-2");

    const state = useRecentTasksStore.getState().byWorkspace;
    expect(state["ws-a"]?.map((e) => e.id)).toEqual(["task-1"]);
    expect(state["ws-b"]?.map((e) => e.id)).toEqual(["task-2"]);
  });

  it("moves the most recent visit to the front and dedupes", () => {
    const { recordVisit } = useRecentTasksStore.getState();
    recordVisit("ws-a", "task-1");
    recordVisit("ws-a", "task-2");
    recordVisit("ws-a", "task-1");

    const ids = useRecentTasksStore
      .getState()
      .byWorkspace["ws-a"]?.map((e) => e.id);
    expect(ids).toEqual(["task-1", "task-2"]);
  });

  it("caps each workspace's bucket at 20 entries", () => {
    const { recordVisit } = useRecentTasksStore.getState();
    for (let i = 0; i < 25; i++) recordVisit("ws-a", `task-${i}`);
    expect(useRecentTasksStore.getState().byWorkspace["ws-a"]).toHaveLength(20);
  });
});

describe("useRecentTasksStore.forgetTask", () => {
  it("removes a single id from the workspace bucket", () => {
    const { recordVisit, forgetTask } = useRecentTasksStore.getState();
    recordVisit("ws-a", "task-1");
    recordVisit("ws-a", "task-2");
    recordVisit("ws-a", "task-3");

    forgetTask("ws-a", "task-2");

    const ids = useRecentTasksStore
      .getState()
      .byWorkspace["ws-a"]?.map((e) => e.id);
    expect(ids).toEqual(["task-3", "task-1"]);
  });

  it("drops the bucket entirely when the last id is removed", () => {
    const { recordVisit, forgetTask } = useRecentTasksStore.getState();
    recordVisit("ws-a", "task-1");
    recordVisit("ws-b", "task-2");

    forgetTask("ws-a", "task-1");

    const state = useRecentTasksStore.getState().byWorkspace;
    expect(state["ws-a"]).toBeUndefined();
    expect(state["ws-b"]?.map((e) => e.id)).toEqual(["task-2"]);
  });

  it("does not touch other workspaces' buckets", () => {
    const { recordVisit, forgetTask } = useRecentTasksStore.getState();
    recordVisit("ws-a", "task-1");
    recordVisit("ws-b", "task-1");

    forgetTask("ws-a", "task-1");

    const state = useRecentTasksStore.getState().byWorkspace;
    expect(state["ws-a"]).toBeUndefined();
    expect(state["ws-b"]?.map((e) => e.id)).toEqual(["task-1"]);
  });

  it("is a no-op when the id is not in the bucket", () => {
    const { recordVisit, forgetTask } = useRecentTasksStore.getState();
    recordVisit("ws-a", "task-1");
    const before = useRecentTasksStore.getState().byWorkspace;

    forgetTask("ws-a", "task-missing");

    expect(useRecentTasksStore.getState().byWorkspace).toBe(before);
  });

  it("is a no-op when the workspace has no bucket", () => {
    const { forgetTask } = useRecentTasksStore.getState();
    const before = useRecentTasksStore.getState().byWorkspace;

    forgetTask("ws-missing", "task-1");

    expect(useRecentTasksStore.getState().byWorkspace).toBe(before);
  });
});

describe("useRecentTasksStore.pruneWorkspaces", () => {
  it("drops buckets for workspaces not in the active set", () => {
    const { recordVisit, pruneWorkspaces } = useRecentTasksStore.getState();
    recordVisit("ws-a", "task-1");
    recordVisit("ws-b", "task-2");
    recordVisit("ws-c", "task-3");

    pruneWorkspaces(["ws-a", "ws-c"]);
    const state = useRecentTasksStore.getState().byWorkspace;
    expect(Object.keys(state).sort()).toEqual(["ws-a", "ws-c"]);
  });

  it("is a no-op when every bucket is still active", () => {
    const { recordVisit, pruneWorkspaces } = useRecentTasksStore.getState();
    recordVisit("ws-a", "task-1");
    const before = useRecentTasksStore.getState().byWorkspace;
    pruneWorkspaces(["ws-a"]);
    expect(useRecentTasksStore.getState().byWorkspace).toBe(before);
  });
});

describe("selectRecentTasks", () => {
  it("returns the bucket for the given workspace", () => {
    useRecentTasksStore.getState().recordVisit("ws-a", "task-1");
    const items = selectRecentTasks("ws-a")(useRecentTasksStore.getState());
    expect(items.map((e) => e.id)).toEqual(["task-1"]);
  });

  it("returns a stable empty array when wsId is null or unknown", () => {
    const a = selectRecentTasks(null)(useRecentTasksStore.getState());
    const b = selectRecentTasks(null)(useRecentTasksStore.getState());
    const c = selectRecentTasks("missing")(useRecentTasksStore.getState());
    expect(a).toBe(b);
    expect(a).toBe(c);
    expect(a).toEqual([]);
  });
});
