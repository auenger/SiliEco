import { QueryClient } from "@tanstack/react-query";
import { beforeEach, describe, expect, it } from "vitest";

import { cleanupDeletedTaskCaches } from "./delete-cache";
import { taskKeys } from "./queries";
import { useRecentTasksStore } from "./stores/recent-tasks-store";

const WS_ID = "ws-a";

beforeEach(() => {
  useRecentTasksStore.setState({ byWorkspace: {} });
});

describe("cleanupDeletedTaskCaches — recent tasks store", () => {
  it("removes the deleted task from the recent tasks bucket", () => {
    const { recordVisit } = useRecentTasksStore.getState();
    recordVisit(WS_ID, "task-1");
    recordVisit(WS_ID, "task-2");

    const qc = new QueryClient();
    cleanupDeletedTaskCaches(qc, WS_ID, "task-1");

    const ids = useRecentTasksStore
      .getState()
      .byWorkspace[WS_ID]?.map((e) => e.id);
    expect(ids).toEqual(["task-2"]);
  });

  it("does not touch the recent bucket of an unrelated workspace", () => {
    const { recordVisit } = useRecentTasksStore.getState();
    recordVisit(WS_ID, "task-1");
    recordVisit("ws-b", "task-1");

    const qc = new QueryClient();
    cleanupDeletedTaskCaches(qc, WS_ID, "task-1");

    const state = useRecentTasksStore.getState().byWorkspace;
    expect(state[WS_ID]).toBeUndefined();
    expect(state["ws-b"]?.map((e) => e.id)).toEqual(["task-1"]);
  });

  it("still removes the cached detail query for the deleted task", () => {
    const qc = new QueryClient();
    qc.setQueryData(taskKeys.detail(WS_ID, "task-1"), { id: "task-1" });

    cleanupDeletedTaskCaches(qc, WS_ID, "task-1");

    expect(qc.getQueryData(taskKeys.detail(WS_ID, "task-1"))).toBeUndefined();
  });
});
