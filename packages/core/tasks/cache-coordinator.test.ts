import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { QueryClient, hashKey } from "@tanstack/react-query";
import {
  applyTaskChange,
  invalidateUpdatedAtSortedTaskLists,
  rollbackTaskChange,
  type TaskFlatCache,
} from "./cache-coordinator";
import { taskChangedDims } from "./surface/membership";
import { taskKeys, type TaskSortParam } from "./queries";
import { inboxKeys } from "../inbox/queries";
import type {
  InboxItem,
  Task,
  TaskTableRowsResponse,
  ListTasksCache,
} from "../types";

const WS_ID = "ws-1";
const sort: TaskSortParam = { sort_by: "position", sort_direction: undefined };

const wsKey = taskKeys.listSorted(WS_ID, sort);
const myAssignedKey = taskKeys.myListSorted(WS_ID, "assigned", { assignee_id: "me" }, sort);
const myAllKey = taskKeys.myListSorted(WS_ID, "all", {}, sort);
const involvedKey = taskKeys.myListSorted(WS_ID, "agents", { involves_user_id: "me" }, sort);
const projectP1Key = taskKeys.myListSorted(WS_ID, "project:p1", { project_id: "p1" }, sort);
const projectP2Key = taskKeys.myListSorted(WS_ID, "project:p2", { project_id: "p2" }, sort);
const membersKey = taskKeys.myListSorted(
  WS_ID,
  "workspace:members",
  { assignee_types: ["member"] },
  sort,
);
const inboxKey = inboxKeys.list(WS_ID);
const flatKey = taskKeys.flat(WS_ID, "workspace:all", {}, sort);
const flatTitleKey = taskKeys.flat(
  WS_ID,
  "workspace:all",
  {},
  { sort_by: "title", sort_direction: "asc" },
);
const flatFilteredKey = taskKeys.flat(
  WS_ID,
  "workspace:all",
  { statuses: ["todo"], priorities: ["high"] },
  sort,
);
const flatUpdatedWindowKey = taskKeys.flat(
  WS_ID,
  "workspace:all",
  {},
  {
    ...sort,
    date_field: "updated_at",
    date_start: "2025-01-01T00:00:00Z",
    date_end: "2025-02-01T00:00:00Z",
  },
);
const flatSearchKey = taskKeys.flat(
  WS_ID,
  "workspace:all",
  { q: "Task 1" },
  sort,
);
const tableRowKey = [
  ...taskKeys.tableRows(
    WS_ID,
    {
      scope: { kind: "workspace" },
      filters: {},
      sort: { field: "position", direction: "asc" },
    },
    { kind: "none" },
    null,
    false,
    null,
  ),
  "page",
  null,
] as const;
const updatedSort: TaskSortParam = {
  sort_by: "updated_at",
  sort_direction: "desc",
};
const wsUpdatedKey = taskKeys.listSorted(WS_ID, updatedSort);
const myAllUpdatedKey = taskKeys.myListSorted(WS_ID, "all", {}, updatedSort);
const flatUpdatedSortKey = taskKeys.flat(
  WS_ID,
  "workspace:all",
  {},
  updatedSort,
);
// Assignee-grouped boards fold the sort into their filter bag
// (taskAssigneeGroupsOptions does `{ ...filter, ...sort }`).
const assigneeGroupsUpdatedKey = taskKeys.assigneeGroups(WS_ID, {
  ...updatedSort,
});
const assigneeGroupsPositionKey = taskKeys.assigneeGroups(WS_ID, {
  sort_by: "position",
});
const myAssigneeGroupsUpdatedKey = taskKeys.myAssigneeGroups(WS_ID, "all", {
  ...updatedSort,
});

function makeTask(idx: number, overrides: Partial<Task> = {}): Task {
  return {
    id: `task-${idx}`,
    workspace_id: WS_ID,
    number: idx,
    identifier: `SILI-${idx}`,
    title: `Task ${idx}`,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: "member",
    assignee_id: "me",
    creator_type: "member",
    creator_id: "me",
    parent_task_id: null,
    project_id: "p1",
    position: idx,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
  properties: {},
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    ...overrides,
  };
}

function bucketed(tasks: Task[], extraTotal = 0): ListTasksCache {
  return {
    byStatus: {
      todo: {
        tasks: tasks.filter((i) => i.status === "todo"),
        total: tasks.filter((i) => i.status === "todo").length + extraTotal,
      },
      in_progress: {
        tasks: tasks.filter((i) => i.status === "in_progress"),
        total: tasks.filter((i) => i.status === "in_progress").length,
      },
    },
  };
}

function ids(qc: QueryClient, key: readonly unknown[], status: "todo" | "in_progress") {
  const cache = qc.getQueryData<ListTasksCache>(key);
  return (cache?.byStatus[status]?.tasks ?? []).map((i) => i.id);
}

function total(qc: QueryClient, key: readonly unknown[], status: "todo" | "in_progress") {
  return qc.getQueryData<ListTasksCache>(key)?.byStatus[status]?.total;
}

describe("applyTaskChange", () => {
  let qc: QueryClient;
  const task = () => makeTask(1);

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
  });

  it("plain field change: patches in place everywhere, no removals, no stale keys", () => {
    qc.setQueryData<ListTasksCache>(wsKey, bucketed([task()]));
    qc.setQueryData<ListTasksCache>(myAssignedKey, bucketed([task()]));
    qc.setQueryData<ListTasksCache>(involvedKey, bucketed([task()]));
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, "task-1"), task());

    const patch = { title: "renamed" };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    for (const key of [wsKey, myAssignedKey, involvedKey]) {
      const cache = qc.getQueryData<ListTasksCache>(key);
      expect(cache?.byStatus.todo?.tasks[0]?.title).toBe("renamed");
    }
    expect(qc.getQueryData<Task>(taskKeys.detail(WS_ID, "task-1"))?.title).toBe(
      "renamed",
    );
    expect(result.staleKeys).toEqual([]);
  });

  it("patches a loaded flat row without refetching an unrelated position window", () => {
    qc.setQueryData<TaskFlatCache>(flatKey, {
      pages: [{ tasks: [task()], total: 1 }],
      pageParams: [0],
    });

    const patch = { title: "renamed in table" };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    expect(
      qc.getQueryData<TaskFlatCache>(flatKey)?.pages[0]?.tasks[0]?.title,
    ).toBe("renamed in table");
    expect(result.staleKeys.map(hashKey)).not.toContain(hashKey(flatKey));

    rollbackTaskChange(qc, WS_ID, "task-1", result);
    expect(
      qc.getQueryData<TaskFlatCache>(flatKey)?.pages[0]?.tasks[0]?.title,
    ).toBe("Task 1");
  });

  it("patches and rolls back loaded server Table rows", () => {
    const snapshot: TaskTableRowsResponse = {
      query_fingerprint: "sha256:table",
      group_key: null,
      parent_id: null,
      total: 1,
      rows: [{ task: task(), direct_child_count: 0 }],
      branch_total: 1,
      next_cursor: null,
    };
    qc.setQueryData(tableRowKey, snapshot);

    const patch = { title: "optimistic table title" };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    expect(
      qc.getQueryData<TaskTableRowsResponse>(tableRowKey)?.rows[0]?.task.title,
    ).toBe("optimistic table title");
    expect(result.prevTableRows).toEqual([[tableRowKey, snapshot]]);

    rollbackTaskChange(qc, WS_ID, "task-1", result);
    expect(qc.getQueryData(tableRowKey)).toEqual(snapshot);
  });

  it("marks only flat windows whose sort or facet depends on the changed field", () => {
    for (const key of [
      flatKey,
      flatTitleKey,
      flatFilteredKey,
      flatUpdatedWindowKey,
    ]) {
      qc.setQueryData<TaskFlatCache>(key, {
        pages: [{ tasks: [task()], total: 1 }],
        pageParams: [0],
      });
    }

    const titlePatch = { title: "renamed" };
    const titleResult = applyTaskChange(qc, WS_ID, "task-1", titlePatch, {
      changed: taskChangedDims(titlePatch, task()),
      baseTask: task(),
    });
    expect(titleResult.staleKeys.map(hashKey)).toEqual([
      hashKey(flatTitleKey),
      hashKey(flatUpdatedWindowKey),
    ]);

    const statusPatch = { status: "done" as const };
    const statusResult = applyTaskChange(qc, WS_ID, "task-1", statusPatch, {
      changed: taskChangedDims(statusPatch, task()),
      baseTask: task(),
    });
    expect(statusResult.staleKeys.map(hashKey)).toContain(hashKey(flatFilteredKey));
    expect(statusResult.staleKeys.map(hashKey)).not.toContain(hashKey(flatKey));
  });

  it("reconciles a searched flat window when an edited title can change membership", () => {
    qc.setQueryData<TaskFlatCache>(flatSearchKey, {
      pages: [{ tasks: [task()], total: 1 }],
      pageParams: [0],
    });

    const patch = { title: "No longer matches" };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    expect(result.staleKeys.map(hashKey)).toContain(hashKey(flatSearchKey));
  });

  it("same-status field edit re-sorts an updated_at-sorted board but not a position-sorted one", () => {
    // Same card loaded in a position-sorted board and an updated_at-sorted one.
    qc.setQueryData<ListTasksCache>(wsKey, bucketed([task()]));
    qc.setQueryData<ListTasksCache>(wsUpdatedKey, bucketed([task()]));

    const patch = { priority: "high" as const };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    // The card is patched in place in both (no status/position move).
    expect(ids(qc, wsKey, "todo")).toEqual(["task-1"]);
    expect(ids(qc, wsUpdatedKey, "todo")).toEqual(["task-1"]);
    // Only the updated_at-sorted board is marked for a server re-sort; the
    // edit advanced updated_at so its loaded slot drifted.
    const stale = result.staleKeys.map(hashKey);
    expect(stale).toContain(hashKey(wsUpdatedKey));
    expect(stale).not.toContain(hashKey(wsKey));
  });

  it("marks an updated_at-sorted board stale even when the edited card is beyond its window", () => {
    // Card not in the loaded window (empty bucket, non-zero total): an edit
    // that bumps updated_at can still surface it, so the key must refetch.
    qc.setQueryData<ListTasksCache>(myAllUpdatedKey, bucketed([], 3));

    const patch = { title: "renamed" };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    expect(result.staleKeys.map(hashKey)).toContain(hashKey(myAllUpdatedKey));
  });

  it("status change: rebuckets loaded cards, patches inbox, adjusts counts for absent-but-member lists", () => {
    qc.setQueryData<ListTasksCache>(wsKey, bucketed([task()]));
    // p1 list loaded but the card is beyond its loaded window — the change
    // is certain (old + new status known, membership definite), so the two
    // bucket totals shift arithmetically right away; the list is then marked
    // stale so the server can place the row in the target bucket's window.
    qc.setQueryData<ListTasksCache>(projectP1Key, bucketed([], 3));
    // p2 list loaded; the task was never a member — untouched.
    qc.setQueryData<ListTasksCache>(projectP2Key, bucketed([]));
    qc.setQueryData<InboxItem[]>(inboxKey, [
      {
        id: "inbox-1",
        workspace_id: WS_ID,
        recipient_type: "member",
        recipient_id: "me",
        actor_type: "member",
        actor_id: "bob",
        type: "status_changed",
        severity: "info",
        task_id: "task-1",
        title: "Inbox",
        body: null,
        task_status: "todo",
        read: false,
        archived: false,
        created_at: "2025-01-01T00:00:00Z",
        details: null,
      },
    ]);

    const patch = { status: "in_progress" as const };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    expect(ids(qc, wsKey, "todo")).toEqual([]);
    expect(ids(qc, wsKey, "in_progress")).toEqual(["task-1"]);
    expect(
      qc.getQueryData<InboxItem[]>(inboxKey)?.[0]?.task_status,
    ).toBe("in_progress");

    // Off-window count arithmetic: todo 3 → 2, in_progress 0 → 1, loaded
    // arrays untouched (never hard-insert).
    expect(total(qc, projectP1Key, "todo")).toBe(2);
    expect(total(qc, projectP1Key, "in_progress")).toBe(1);
    expect(ids(qc, projectP1Key, "todo")).toEqual([]);

    const staleHashes = result.staleKeys.map(hashKey);
    // The moved list IS flagged stale: the row now counted in in_progress
    // can only be placed into that bucket's loaded window by the server,
    // and with staleTime: Infinity no other channel triggers that reconcile.
    expect(staleHashes).toContain(hashKey(projectP1Key));
    // Never-a-member p2 and the surgically-rebucketed workspace list have
    // nothing to reconcile — no stale keys.
    expect(staleHashes).not.toContain(hashKey(projectP2Key));
    expect(staleHashes).not.toContain(hashKey(wsKey));
  });

  it("off-window leave: decrements the old status bucket total without a refetch", () => {
    // The card is beyond My-Assigned's loaded window; reassigning it to bob
    // means the list's todo total counted it and must lose one.
    qc.setQueryData<ListTasksCache>(myAssignedKey, bucketed([], 2));

    const patch = { assignee_id: "bob", assignee_type: "member" as const };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    expect(total(qc, myAssignedKey, "todo")).toBe(1);
    expect(result.staleKeys).toEqual([]);
  });

  it("off-window member-to-member reassignment leaves counts and pages untouched", () => {
    // Members tab: bob is still a member, membership holds, status unchanged
    // — nothing about this list can have drifted, so not even a stale key.
    qc.setQueryData<ListTasksCache>(membersKey, bucketed([], 5));

    const patch = { assignee_id: "bob", assignee_type: "member" as const };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    expect(total(qc, membersKey, "todo")).toBe(5);
    expect(result.staleKeys).toEqual([]);
  });

  it("rolls off-window count arithmetic back on failure", () => {
    const snapshot = bucketed([], 3);
    qc.setQueryData<ListTasksCache>(projectP1Key, snapshot);

    const patch = { status: "in_progress" as const };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });
    expect(total(qc, projectP1Key, "todo")).toBe(2);

    rollbackTaskChange(qc, WS_ID, "task-1", result);
    expect(qc.getQueryData<ListTasksCache>(projectP1Key)).toEqual(snapshot);
  });

  it("assignee change me→bob: removes from my-assigned (total decremented), keeps members tab, flags union/involves scopes", () => {
    qc.setQueryData<ListTasksCache>(wsKey, bucketed([task()]));
    qc.setQueryData<ListTasksCache>(myAssignedKey, bucketed([task()]));
    qc.setQueryData<ListTasksCache>(myAllKey, bucketed([task()]));
    qc.setQueryData<ListTasksCache>(involvedKey, bucketed([task()]));
    qc.setQueryData<ListTasksCache>(membersKey, bucketed([task()]));

    const patch = { assignee_id: "bob", assignee_type: "member" as const };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    // The bug this fixes: the card must LEAVE my-assigned immediately —
    // no WS echo, no refetch needed.
    expect(ids(qc, myAssignedKey, "todo")).toEqual([]);
    expect(total(qc, myAssignedKey, "todo")).toBe(0);
    // Workspace board and members tab (bob is still a member) keep the card,
    // with the new assignee patched in.
    expect(ids(qc, wsKey, "todo")).toEqual(["task-1"]);
    expect(ids(qc, membersKey, "todo")).toEqual(["task-1"]);
    expect(
      qc.getQueryData<ListTasksCache>(membersKey)?.byStatus.todo?.tasks[0]
        ?.assignee_id,
    ).toBe("bob");

    // Union (my:all) and involves membership are server knowledge — patched
    // in place but flagged for a deferred refetch.
    const staleHashes = result.staleKeys.map(hashKey);
    expect(staleHashes).toContain(hashKey(myAllKey));
    expect(staleHashes).toContain(hashKey(involvedKey));
    expect(staleHashes).not.toContain(hashKey(myAssignedKey));
    expect(staleHashes).not.toContain(hashKey(membersKey));
  });

  it("member→agent reassignment: leaves the members tab, enters the loaded agents tab via stale key", () => {
    const agentsKey = taskKeys.myListSorted(
      WS_ID,
      "workspace:agents",
      { assignee_types: ["agent", "squad"] },
      sort,
    );
    qc.setQueryData<ListTasksCache>(membersKey, bucketed([task()]));
    qc.setQueryData<ListTasksCache>(agentsKey, bucketed([]));

    const patch = { assignee_id: "agent-1", assignee_type: "agent" as const };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    expect(ids(qc, membersKey, "todo")).toEqual([]);
    // Never hard-insert into the agents tab — the right page/slot is server
    // knowledge; the loaded list is flagged for refetch instead.
    expect(ids(qc, agentsKey, "todo")).toEqual([]);
    expect(result.staleKeys.map(hashKey)).toContain(hashKey(agentsKey));
  });

  it("project move p1→p2: removes from the old project's list, flags the loaded target list, never touches unrelated lists", () => {
    qc.setQueryData<ListTasksCache>(wsKey, bucketed([task()]));
    qc.setQueryData<ListTasksCache>(projectP1Key, bucketed([task()]));
    qc.setQueryData<ListTasksCache>(projectP2Key, bucketed([]));
    qc.setQueryData<ListTasksCache>(myAssignedKey, bucketed([task()]));

    const patch = { project_id: "p2" };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    expect(ids(qc, projectP1Key, "todo")).toEqual([]);
    expect(total(qc, projectP1Key, "todo")).toBe(0);
    expect(ids(qc, wsKey, "todo")).toEqual(["task-1"]);
    // Assignee list membership is untouched by a project move.
    expect(ids(qc, myAssignedKey, "todo")).toEqual(["task-1"]);

    const staleHashes = result.staleKeys.map(hashKey);
    expect(staleHashes).toContain(hashKey(projectP2Key));
    expect(staleHashes).not.toContain(hashKey(myAssignedKey));
    expect(staleHashes).not.toContain(hashKey(wsKey));
  });

  it("degrades to a stale key when no base entity is known for an absent card", () => {
    // The card is beyond the list's loaded window and neither detail nor any
    // list holds it — without a base the client cannot rule out that it WAS
    // a member (and left), so the list must refetch.
    qc.setQueryData<ListTasksCache>(myAssignedKey, bucketed([], 2));

    const patch = { assignee_id: "bob", assignee_type: "member" as const };
    const result = applyTaskChange(qc, WS_ID, "task-99", patch, {
      changed: taskChangedDims(patch),
    });

    expect(result.staleKeys.map(hashKey)).toContain(hashKey(myAssignedKey));
  });

  it("skips absent cards entirely when the change cannot affect the list", () => {
    // Assignee change, project-filtered list, card absent, base known and in
    // another project — nothing about this list can have drifted.
    qc.setQueryData<ListTasksCache>(projectP2Key, bucketed([]));

    const patch = { assignee_id: "bob", assignee_type: "member" as const };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });

    expect(result.staleKeys).toEqual([]);
  });

  it("rollbackTaskChange restores lists, detail, and inbox exactly", () => {
    const listSnapshot = bucketed([task()]);
    qc.setQueryData<ListTasksCache>(myAssignedKey, listSnapshot);
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, "task-1"), task());
    qc.setQueryData<InboxItem[]>(inboxKey, []);

    const patch = { assignee_id: "bob", assignee_type: "member" as const, status: "in_progress" as const };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch, task()),
      baseTask: task(),
    });
    expect(ids(qc, myAssignedKey, "todo")).toEqual([]);

    rollbackTaskChange(qc, WS_ID, "task-1", result);

    expect(qc.getQueryData<ListTasksCache>(myAssignedKey)).toEqual(listSnapshot);
    expect(qc.getQueryData<Task>(taskKeys.detail(WS_ID, "task-1"))).toEqual(task());
    expect(qc.getQueryData<InboxItem[]>(inboxKey)).toEqual([]);
  });

  it("skips grouped caches living under the same key prefixes", () => {
    // myAssigneeGroups lives under the myAll prefix but has no byStatus shape.
    const groupedKey = taskKeys.myAssigneeGroups(WS_ID, "assigned", {});
    const grouped = { groups: [] };
    qc.setQueryData(groupedKey, grouped);

    const patch = { status: "in_progress" as const };
    const result = applyTaskChange(qc, WS_ID, "task-1", patch, {
      changed: taskChangedDims(patch),
    });

    expect(qc.getQueryData(groupedKey)).toBe(grouped);
    expect(result.staleKeys.map(hashKey)).not.toContain(hashKey(groupedKey));
  });
});

describe("invalidateUpdatedAtSortedTaskLists", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
  });

  it("invalidates only the updated_at-sorted board, myList, and flat keys", () => {
    // Two boards, two flat windows: one pair sorted by position, one by
    // updated_at. Only the updated_at pair should be refetched.
    qc.setQueryData<ListTasksCache>(wsKey, bucketed([makeTask(1)]));
    qc.setQueryData<ListTasksCache>(wsUpdatedKey, bucketed([makeTask(1)]));
    qc.setQueryData<ListTasksCache>(myAllUpdatedKey, bucketed([makeTask(1)]));
    qc.setQueryData<TaskFlatCache>(flatKey, {
      pages: [{ tasks: [makeTask(1)], total: 1 }],
      pageParams: [0],
    });
    qc.setQueryData<TaskFlatCache>(flatUpdatedSortKey, {
      pages: [{ tasks: [makeTask(1)], total: 1 }],
      pageParams: [0],
    });

    invalidateUpdatedAtSortedTaskLists(qc, WS_ID);

    expect(qc.getQueryState(wsUpdatedKey)?.isInvalidated).toBe(true);
    expect(qc.getQueryState(myAllUpdatedKey)?.isInvalidated).toBe(true);
    expect(qc.getQueryState(flatUpdatedSortKey)?.isInvalidated).toBe(true);
    expect(qc.getQueryState(wsKey)?.isInvalidated).toBe(false);
    expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(false);
  });

  it("invalidates updated_at-sorted assignee-grouped boards (workspace + My Tasks)", () => {
    // Grouped boards carry the sort inside their filter bag, not a standalone
    // sort key — they must still re-sort under "Updated date".
    qc.setQueryData(assigneeGroupsUpdatedKey, { groups: [] });
    qc.setQueryData(myAssigneeGroupsUpdatedKey, { groups: [] });
    qc.setQueryData(assigneeGroupsPositionKey, { groups: [] });

    invalidateUpdatedAtSortedTaskLists(qc, WS_ID);

    expect(qc.getQueryState(assigneeGroupsUpdatedKey)?.isInvalidated).toBe(true);
    expect(qc.getQueryState(myAssigneeGroupsUpdatedKey)?.isInvalidated).toBe(
      true,
    );
    expect(qc.getQueryState(assigneeGroupsPositionKey)?.isInvalidated).toBe(
      false,
    );
  });

  it("is a no-op when no updated_at-sorted list is loaded", () => {
    qc.setQueryData<ListTasksCache>(wsKey, bucketed([makeTask(1)]));

    invalidateUpdatedAtSortedTaskLists(qc, WS_ID);

    expect(qc.getQueryState(wsKey)?.isInvalidated).toBe(false);
  });
});
