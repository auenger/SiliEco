import { agentRunsKeys } from "../agent-runs/queries";
import { beforeEach, describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { agentActivityKeys, agentRunCountsKeys, agentRunSnapshotKeys } from "../agents/queries";
import {
  onTaskCreated,
  onTaskDeleted,
  onTaskLabelsChanged,
  onTaskMetadataChanged,
  onTaskPropertiesChanged,
  onTaskUpdated,
  patchTaskLabels,
  patchTaskProperties,
} from "./ws-updaters";
import { taskKeys } from "./queries";
import { labelKeys } from "../labels/queries";
import { projectKeys } from "../projects/queries";
import type {
  AgentActivityBucket,
  AgentRunCount,
  AgentRun,
  Attachment,
  Task,
  TaskReaction,
  TaskLabelsResponse,
  TaskTableRowsResponse,
  TaskSubscriber,
  TaskUsageSummary,
  Label,
  ListTasksCache,
  TimelineEntry,
} from "../types";

const WS_ID = "ws-1";
const TASK_ID = "task-1";
const OTHER_TASK_ID = "task-2";
const PARENT_TASK_ID = "parent-1";
const AGENT_ID = "agent-1";
const PROJECT_ID = "project-1";

const labelA: Label = {
  id: "label-a",
  workspace_id: WS_ID,
  name: "bug",
  color: "#ef4444",
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-01T00:00:00Z",
};

const labelB: Label = {
  id: "label-b",
  workspace_id: WS_ID,
  name: "feature",
  color: "#22c55e",
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-01T00:00:00Z",
};

const baseTask: Task = {
  id: TASK_ID,
  workspace_id: WS_ID,
  number: 1,
  identifier: "SILI-1",
  title: "Test",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_task_id: null,
  project_id: null,
  position: 0,
  stage: null,
  start_date: null,
  due_date: null,
  metadata: {},
  properties: {},
  labels: [labelA],
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-01T00:00:00Z",
};

const parentedTask: Task = {
  ...baseTask,
  parent_task_id: PARENT_TASK_ID,
};

const otherTask: Task = {
  ...baseTask,
  id: OTHER_TASK_ID,
  identifier: "SILI-2",
  title: "Other",
};

function makeListCache(...tasks: Task[]): ListTasksCache {
  return {
    byStatus: {
      todo: { tasks, total: tasks.length },
    },
  };
}

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

function seedTableRow(qc: QueryClient, task = baseTask) {
  qc.setQueryData<TaskTableRowsResponse>(tableRowKey, {
    query_fingerprint: "sha256:table",
    group_key: null,
    parent_id: null,
    total: 1,
    rows: [{ task, direct_child_count: 0 }],
    branch_total: 1,
    next_cursor: null,
  });
}

function makeTask(taskId = TASK_ID): AgentRun {
  return {
    id: `task-${taskId}`,
    agent_id: AGENT_ID,
    runtime_id: "runtime-1",
    task_id: taskId,
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: "2025-01-01T00:00:00Z",
    completed_at: "2025-01-01T00:01:00Z",
    result: null,
    error: null,
    created_at: "2025-01-01T00:00:00Z",
  };
}

function expectInvalidated(qc: QueryClient, queryKey: readonly unknown[]) {
  expect(qc.getQueryState(queryKey)?.isInvalidated).toBe(true);
}

describe("onTaskLabelsChanged", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it("patches the per-task label cache when present (LabelPicker source)", () => {
    qc.setQueryData<TaskLabelsResponse>(labelKeys.byTask(WS_ID, TASK_ID), {
      labels: [labelA],
    });

    onTaskLabelsChanged(qc, WS_ID, TASK_ID, [labelB]);

    expect(
      qc.getQueryData<TaskLabelsResponse>(labelKeys.byTask(WS_ID, TASK_ID)),
    ).toEqual({ labels: [labelB] });
  });

  it("leaves the per-task label cache untouched when the picker has not fetched", () => {
    onTaskLabelsChanged(qc, WS_ID, TASK_ID, [labelB]);

    expect(qc.getQueryData(labelKeys.byTask(WS_ID, TASK_ID))).toBeUndefined();
  });

  it("still patches the list, Table row, and detail caches", () => {
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), {
      byStatus: { todo: { tasks: [baseTask], total: 1 } },
    });
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), baseTask);
    seedTableRow(qc);

    onTaskLabelsChanged(qc, WS_ID, TASK_ID, [labelB]);

    const list = qc.getQueryData<ListTasksCache>(taskKeys.list(WS_ID));
    expect(list?.byStatus.todo?.tasks[0]?.labels).toEqual([labelB]);

    const detail = qc.getQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID));
    expect(detail?.labels).toEqual([labelB]);
    expect(
      qc.getQueryData<TaskTableRowsResponse>(tableRowKey)?.rows[0]?.task
        .labels,
    ).toEqual([labelB]);
  });

  it("patches the Project Gantt cache so label filters react in place", () => {
    const PROJECT_ID = "project-1";
    qc.setQueryData<Task[]>(taskKeys.projectGantt(WS_ID, PROJECT_ID), [
      baseTask,
      otherTask,
    ]);

    onTaskLabelsChanged(qc, WS_ID, TASK_ID, [labelB]);

    const gantt = qc.getQueryData<Task[]>(
      taskKeys.projectGantt(WS_ID, PROJECT_ID),
    );
    expect(gantt?.find((i) => i.id === TASK_ID)?.labels).toEqual([labelB]);
    // Other tasks in the same cache must not have their labels mutated.
    expect(gantt?.find((i) => i.id === OTHER_TASK_ID)?.labels).toEqual([
      labelA,
    ]);
  });

  it("defers label-filtered flat-window invalidation until commit", () => {
    const flatKey = taskKeys.flat(
      WS_ID,
      "workspace:all",
      { label_ids: [labelB.id] },
      { sort_by: "position" },
    );
    qc.setQueryData(flatKey, {
      pages: [{ tasks: [baseTask], total: 1 }],
      pageParams: [0],
    });

    patchTaskLabels(qc, WS_ID, TASK_ID, [labelB]);
    expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(false);

    onTaskLabelsChanged(qc, WS_ID, TASK_ID, [labelB]);
    expectInvalidated(qc, flatKey);
  });

  it("patches the parent's children cache so the sub-tasks panel stays fresh", () => {
    const child = { ...baseTask, parent_task_id: PARENT_TASK_ID };
    const childrenKey = taskKeys.children(WS_ID, PARENT_TASK_ID);
    qc.setQueryData<Task[]>(childrenKey, [
      child,
      otherTask,
    ]);
    // A children cache that does NOT hold the task must keep its reference
    // (no pointless rerender of unrelated sub-task panels).
    const unrelated = [otherTask];
    qc.setQueryData<Task[]>(taskKeys.children(WS_ID, "parent-9"), unrelated);

    onTaskLabelsChanged(qc, WS_ID, TASK_ID, [labelB]);

    const children = qc.getQueryData<Task[]>(
      taskKeys.children(WS_ID, PARENT_TASK_ID),
    );
    expect(children?.find((i) => i.id === TASK_ID)?.labels).toEqual([labelB]);
    expect(children?.find((i) => i.id === OTHER_TASK_ID)?.labels).toEqual([
      labelA,
    ]);
    expect(qc.getQueryData<Task[]>(taskKeys.children(WS_ID, "parent-9"))).toBe(
      unrelated,
    );
    // The committed WS/mutation snapshot also marks active children queries
    // stale, preventing an older in-flight response from overwriting the patch.
    expectInvalidated(qc, childrenKey);
  });

  it("invalidates batched children caches (Map-shaped, not patchable)", () => {
    const batchedKey = taskKeys.childrenByParents(WS_ID, [PARENT_TASK_ID]);
    qc.setQueryData(batchedKey, new Map([[PARENT_TASK_ID, [baseTask]]]));

    onTaskLabelsChanged(qc, WS_ID, TASK_ID, [labelB]);

    expectInvalidated(qc, batchedKey);
  });
});

describe("onTaskMetadataChanged", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it("replaces metadata in both detail and list caches (no merge)", () => {
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), {
      ...baseTask,
      metadata: { pr_number: 1, stale: "yes" },
    });
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), {
      byStatus: {
        todo: {
          tasks: [{ ...baseTask, metadata: { pr_number: 1 } }],
          total: 1,
        },
      },
    });
    seedTableRow(qc, {
      ...baseTask,
      metadata: { pr_number: 1, stale: "yes" },
    });

    onTaskMetadataChanged(qc, WS_ID, TASK_ID, { pr_number: 2 });

    const detail = qc.getQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID));
    expect(detail?.metadata).toEqual({ pr_number: 2 });
    const list = qc.getQueryData<ListTasksCache>(taskKeys.list(WS_ID));
    expect(list?.byStatus.todo?.tasks[0]?.metadata).toEqual({ pr_number: 2 });
    expect(
      qc.getQueryData<TaskTableRowsResponse>(tableRowKey)?.rows[0]?.task
        .metadata,
    ).toEqual({ pr_number: 2 });
  });

  it("leaves untouched caches as undefined (no spurious writes)", () => {
    onTaskMetadataChanged(qc, WS_ID, TASK_ID, { foo: "bar" });

    expect(qc.getQueryData(taskKeys.detail(WS_ID, TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(taskKeys.list(WS_ID))).toBeUndefined();
  });

  it("re-sorts an updated_at-sorted board but not a position-sorted one", () => {
    // A metadata write bumps updated_at server-side (SILI-5016), so a board
    // sorted by "Updated date" must refetch; a position-sorted board must not.
    const boardUpdatedKey = taskKeys.listSorted(WS_ID, {
      sort_by: "updated_at",
      sort_direction: "desc",
    });
    const boardPositionKey = taskKeys.listSorted(WS_ID, { sort_by: "position" });
    qc.setQueryData<ListTasksCache>(boardUpdatedKey, makeListCache(baseTask));
    qc.setQueryData<ListTasksCache>(boardPositionKey, makeListCache(baseTask));

    onTaskMetadataChanged(qc, WS_ID, TASK_ID, { foo: "bar" });

    expectInvalidated(qc, boardUpdatedKey);
    expect(qc.getQueryState(boardPositionKey)?.isInvalidated).toBe(false);
  });
});

describe("task property snapshots", () => {
  it("patches per-parent children and invalidates every children projection on commit", () => {
    const qc = new QueryClient();
    const childrenKey = taskKeys.children(WS_ID, PARENT_TASK_ID);
    const unrelatedKey = taskKeys.children(WS_ID, "parent-9");
    const batchedKey = taskKeys.childrenByParents(WS_ID, [PARENT_TASK_ID]);
    const child = {
      ...parentedTask,
      properties: { estimate: 1, environment: "staging" },
    };
    const unrelated = [otherTask];
    qc.setQueryData<Task[]>(childrenKey, [child, otherTask]);
    qc.setQueryData<Task[]>(unrelatedKey, unrelated);
    qc.setQueryData(batchedKey, new Map([[PARENT_TASK_ID, [child]]]));

    // The optimistic leg is deterministic: patch immediately without a
    // premature refetch that could still return the pre-mutation value.
    patchTaskProperties(qc, WS_ID, TASK_ID, {
      estimate: 2,
      environment: "staging",
    });

    expect(
      qc.getQueryData<Task[]>(childrenKey)?.find((candidate) => candidate.id === TASK_ID)
        ?.properties,
    ).toEqual({ estimate: 2, environment: "staging" });
    expect(qc.getQueryState(childrenKey)?.isInvalidated).toBe(false);
    expect(qc.getQueryData<Task[]>(unrelatedKey)).toBe(unrelated);

    // The committed response/event keeps the immediate patch, then marks both
    // per-parent and batched projections stale for authoritative convergence.
    onTaskPropertiesChanged(qc, WS_ID, TASK_ID, {
      estimate: 3,
      environment: "staging",
    });

    expect(
      qc.getQueryData<Task[]>(childrenKey)?.find((candidate) => candidate.id === TASK_ID)
        ?.properties,
    ).toEqual({ estimate: 3, environment: "staging" });
    expectInvalidated(qc, childrenKey);
    expectInvalidated(qc, batchedKey);
    expect(qc.getQueryData<Task[]>(unrelatedKey)).toBe(unrelated);
  });

  it("keeps optimistic patches local, then invalidates property windows after commit", () => {
    const qc = new QueryClient();
    const flatKey = taskKeys.flat(
      WS_ID,
      "workspace:all",
      {},
      { sort_by: "property:estimate", properties: { estimate: ["3"] } },
    );
    qc.setQueryData(flatKey, {
      pages: [{ tasks: [baseTask], total: 1 }],
      pageParams: [0],
    });
    seedTableRow(qc);

    patchTaskProperties(qc, WS_ID, TASK_ID, { estimate: 3 });

    expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(false);
    expect(
      qc.getQueryData<{ pages: { tasks: Task[] }[] }>(flatKey)?.pages[0]
        ?.tasks[0]?.properties,
    ).toEqual({ estimate: 3 });
    expect(
      qc.getQueryData<TaskTableRowsResponse>(tableRowKey)?.rows[0]?.task
        .properties,
    ).toEqual({ estimate: 3 });

    onTaskPropertiesChanged(qc, WS_ID, TASK_ID, { estimate: 4 });

    expectInvalidated(qc, flatKey);
  });

  it("re-sorts an updated_at-sorted board but not a position-sorted one after commit", () => {
    // A property write also bumps updated_at server-side (SILI-5016), so a board
    // sorted by "Updated date" (no property param) must refetch on commit while
    // a position-sorted board stays put.
    const qc = new QueryClient();
    const boardUpdatedKey = taskKeys.listSorted(WS_ID, {
      sort_by: "updated_at",
      sort_direction: "desc",
    });
    const boardPositionKey = taskKeys.listSorted(WS_ID, { sort_by: "position" });
    qc.setQueryData<ListTasksCache>(boardUpdatedKey, makeListCache(baseTask));
    qc.setQueryData<ListTasksCache>(boardPositionKey, makeListCache(baseTask));

    // Optimistic leg patches only — no premature refetch of either board.
    patchTaskProperties(qc, WS_ID, TASK_ID, { estimate: 3 });
    expect(qc.getQueryState(boardUpdatedKey)?.isInvalidated).toBe(false);

    onTaskPropertiesChanged(qc, WS_ID, TASK_ID, { estimate: 4 });

    expectInvalidated(qc, boardUpdatedKey);
    expect(qc.getQueryState(boardPositionKey)?.isInvalidated).toBe(false);
  });
});

describe("project progress invalidation", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
    qc.setQueryData(projectKeys.list(WS_ID), [
      {
        id: PROJECT_ID,
        workspace_id: WS_ID,
        title: "Project",
        description: null,
        icon: null,
        status: "in_progress",
        priority: "none",
        lead_type: null,
        lead_id: null,
        task_count: 1,
        done_count: 0,
        resource_count: 0,
        created_at: "2025-01-01T00:00:00Z",
        updated_at: "2025-01-01T00:00:00Z",
      },
    ]);
  });

  it("invalidates project queries when an task status changes", () => {
    onTaskUpdated(qc, WS_ID, {
      id: TASK_ID,
      status: "done",
    });

    expectInvalidated(qc, projectKeys.list(WS_ID));
  });

  it("invalidates project queries when a project task is created", () => {
    onTaskCreated(qc, WS_ID, {
      ...baseTask,
      project_id: PROJECT_ID,
    });

    expectInvalidated(qc, projectKeys.list(WS_ID));
  });
});

describe("onTaskCreated — carries the label snapshot into list cache", () => {
  it("keeps the created task's labels so members other than the creator render it already labeled", () => {
    // The backend now attaches labels in the create transaction and echoes
    // them on the task:created event. Guard that the cache insert doesn't
    // strip them — otherwise online members would see the new task blank
    // until a refetch (staleTime: Infinity means no self-heal).
    const qc = new QueryClient();
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), makeListCache());

    onTaskCreated(qc, WS_ID, { ...baseTask, labels: [labelA, labelB] });

    const cache = qc.getQueryData<ListTasksCache>(taskKeys.list(WS_ID));
    const cached = cache?.byStatus.todo?.tasks.find((i) => i.id === TASK_ID);
    expect(cached?.labels).toEqual([labelA, labelB]);
  });
});

describe("onTaskUpdated — position move is surgical, not a list refetch", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  const taskA: Task = { ...baseTask, id: "task-1", position: 0 };
  const taskB: Task = { ...baseTask, id: "task-2", position: 10 };

  it("reorders the moved card in place and does NOT invalidate the workspace list", () => {
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), makeListCache(taskA, taskB));

    // task-1 moves below task-2 (position 0 -> 20) — a remote/echoed drag.
    onTaskUpdated(qc, WS_ID, { ...taskA, position: 20 });

    const list = qc.getQueryData<ListTasksCache>(taskKeys.list(WS_ID));
    // Surgically reordered into its new slot: proof the patch alone suffices.
    expect(list?.byStatus.todo?.tasks.map((i) => i.id)).toEqual(["task-2", "task-1"]);
    // The old redundant `position -> invalidate(list)` is gone — no full-board
    // refetch on top of the surgical patch (that was the flicker source).
    expect(qc.getQueryState(taskKeys.list(WS_ID))?.isInvalidated).toBe(false);
  });

  it("surgically patches the filtered myAll lists on a non-membership change (no refetch)", () => {
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), makeListCache(taskA, taskB));
    qc.setQueryData<ListTasksCache>(taskKeys.myAll(WS_ID), makeListCache(taskA, taskB));

    // Pure position move: membership cannot change, so myAll is patched in place.
    onTaskUpdated(qc, WS_ID, { ...taskA, position: 20 });

    const my = qc.getQueryData<ListTasksCache>(taskKeys.myAll(WS_ID));
    expect(my?.byStatus.todo?.tasks.map((i) => i.id)).toEqual(["task-2", "task-1"]);
    // Reconciled in place — no full-list refetch on My Tasks (that was the
    // remaining drag flicker on filtered boards).
    expect(qc.getQueryState(taskKeys.myAll(WS_ID))?.isInvalidated).toBe(false);
  });

  it("removes the card from an assignee-filtered list when the assignee changes (membership-aware, no blanket refetch)", () => {
    const assignedKey = taskKeys.myListSorted(
      WS_ID,
      "assigned",
      { assignee_id: "user-1" },
      undefined,
    );
    const mine: Task = { ...taskA, assignee_type: "member", assignee_id: "user-1" };
    qc.setQueryData<ListTasksCache>(assignedKey, makeListCache(mine));

    onTaskUpdated(
      qc,
      WS_ID,
      { ...mine, assignee_type: "member", assignee_id: "user-2" },
      { assigneeChanged: true },
    );

    // The card LEAVES the loaded list surgically — the fix for the residue
    // that used to wait on a refetch (and on the mutation path never came).
    const list = qc.getQueryData<ListTasksCache>(assignedKey);
    expect(list?.byStatus.todo?.tasks).toEqual([]);
    expect(list?.byStatus.todo?.total).toBe(0);
    expect(qc.getQueryState(assignedKey)?.isInvalidated).toBe(false);
  });

  it("flags union-scope (my:all) lists stale on an assignee change instead of guessing membership", () => {
    const myAllListKey = taskKeys.myListSorted(WS_ID, "all", {}, undefined);
    const mine: Task = { ...taskA, assignee_type: "member", assignee_id: "user-1" };
    qc.setQueryData<ListTasksCache>(myAllListKey, makeListCache(mine));

    onTaskUpdated(
      qc,
      WS_ID,
      { ...mine, assignee_type: "member", assignee_id: "user-2" },
      { assigneeChanged: true },
    );

    // Union membership (assigned ∪ created ∪ involved) is server knowledge:
    // the card is patched in place and the list refetches to reconcile.
    const list = qc.getQueryData<ListTasksCache>(myAllListKey);
    expect(list?.byStatus.todo?.tasks[0]?.assignee_id).toBe("user-2");
    expectInvalidated(qc, myAllListKey);
  });

  it("moves the card out of the old project's list and flags the loaded target list (legacy diff fallback, no server flag)", () => {
    // taskA.project_id is null; moving it into project-9 must reconcile both
    // ends. No server flag here — this exercises the legacy cache-diff
    // fallback that keeps a new frontend working against an older backend.
    const targetKey = taskKeys.myListSorted(
      WS_ID,
      "project:project-9",
      { project_id: "project-9" },
      undefined,
    );
    qc.setQueryData<ListTasksCache>(targetKey, makeListCache());

    onTaskUpdated(qc, WS_ID, { ...taskA, project_id: "project-9" });

    // Never hard-inserted (its page/slot is server knowledge) — the loaded
    // target list is refetched instead.
    expect(
      qc.getQueryData<ListTasksCache>(targetKey)?.byStatus.todo?.tasks,
    ).toEqual([]);
    expectInvalidated(qc, targetKey);
  });

  it("drops the card from the old project's list on a server project_changed flag even when the cached project_id already matches", () => {
    // Reproduces the drift state behind SILI-3669: the detail cache already
    // carries the NEW project (e.g. a local optimistic write), so a cache
    // diff would compute projectChanged=false — the authoritative server
    // flag must still drive the membership reconcile for any list where the
    // card lingers.
    const moved: Task = { ...taskA, project_id: "project-9" };
    const oldProjectKey = taskKeys.myListSorted(
      WS_ID,
      "project:project-1",
      { project_id: "project-1" },
      undefined,
    );
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, moved.id), moved);
    qc.setQueryData<ListTasksCache>(oldProjectKey, makeListCache(moved));

    onTaskUpdated(qc, WS_ID, moved, { projectChanged: true });

    expect(
      qc.getQueryData<ListTasksCache>(oldProjectKey)?.byStatus.todo?.tasks,
    ).toEqual([]);
  });

  it("does NOT touch project lists when the server flag says project_changed=false (flag overrides the legacy diff)", () => {
    // No detail/list cache for the task, so the legacy diff would resolve
    // oldProjectId=null and fire on the non-null incoming project_id. An explicit
    // false flag from the server is authoritative and must suppress that.
    const projectKey = taskKeys.myListSorted(
      WS_ID,
      "project:project-9",
      { project_id: "project-9" },
      undefined,
    );
    qc.setQueryData<ListTasksCache>(projectKey, makeListCache());

    onTaskUpdated(
      qc,
      WS_ID,
      { ...taskA, project_id: "project-9" },
      { projectChanged: false },
    );

    expect(qc.getQueryState(projectKey)?.isInvalidated).toBe(false);
  });
});

// A board column header shows `byStatus[status].total`. On a status change the
// surgical patch shifts both bucket totals — but only if it can find the card in
// a loaded page. A paginated column loads just its first page, so an off-screen
// task (very common when an agent flips the status of something the viewer
// never scrolled to) is absent: patchTaskInBuckets no-ops and the count would
// silently drift, with no refetch to recover it. The status-changed no-op has to
// fall back to a single-list refetch.
describe("onTaskUpdated — off-screen status change reconciles column counts", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it("refetches the workspace list when a status-changed task is not in the loaded page", () => {
    // First page only: the totals say these columns have items, but the tasks
    // arrays are the loaded window — the moved task lives beyond it.
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), {
      byStatus: {
        in_review: { tasks: [], total: 1 },
        done: { tasks: [], total: 60 },
      },
    });

    onTaskUpdated(
      qc,
      WS_ID,
      { id: "off-screen", status: "done" },
      { statusChanged: true },
    );

    expectInvalidated(qc, taskKeys.list(WS_ID));
  });

  it("refetches the filtered myAll list under the same condition", () => {
    qc.setQueryData<ListTasksCache>(taskKeys.myAll(WS_ID), {
      byStatus: { done: { tasks: [], total: 60 } },
    });

    onTaskUpdated(
      qc,
      WS_ID,
      { id: "off-screen", status: "done" },
      { statusChanged: true },
    );

    expectInvalidated(qc, taskKeys.myAll(WS_ID));
  });

  it("moves the bucket counts AND refetches when the off-screen task's base entity is known", () => {
    // The detail cache knows the pre-change entity, so the coordinator moves
    // one unit of total between the buckets instantly — but the row itself
    // can only be placed into done's loaded window by the server, and with
    // staleTime: Infinity this invalidation is the only reconcile channel.
    // (Before the fix this branch moved counts with no stale key: an open
    // board showed "done 61" with 60 visible rows until something else
    // happened to invalidate the list.)
    const offScreen: Task = { ...baseTask, id: "off-screen", status: "in_review" };
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, "off-screen"), offScreen);
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), {
      byStatus: {
        in_review: { tasks: [], total: 1 },
        done: { tasks: [], total: 60 },
      },
    });

    onTaskUpdated(
      qc,
      WS_ID,
      { ...offScreen, status: "done" },
      { statusChanged: true },
    );

    const list = qc.getQueryData<ListTasksCache>(taskKeys.list(WS_ID));
    expect(list?.byStatus.in_review?.total).toBe(0);
    expect(list?.byStatus.done?.total).toBe(61);
    expectInvalidated(qc, taskKeys.list(WS_ID));
  });

  it("does NOT refetch when the status-changed task is loaded (surgical patch suffices)", () => {
    const loaded: Task = { ...baseTask, id: "loaded", status: "in_review" };
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), {
      byStatus: {
        in_review: { tasks: [loaded], total: 1 },
        done: { tasks: [], total: 60 },
      },
    });

    onTaskUpdated(
      qc,
      WS_ID,
      { ...loaded, status: "done" },
      { statusChanged: true },
    );

    const list = qc.getQueryData<ListTasksCache>(taskKeys.list(WS_ID));
    expect(list?.byStatus.in_review?.total).toBe(0);
    expect(list?.byStatus.done?.total).toBe(61);
    // Reconciled in place — the no-flicker fast path from #4415 must hold.
    expect(qc.getQueryState(taskKeys.list(WS_ID))?.isInvalidated).toBe(false);
  });

  it("does NOT refetch an absent task when the status did not change", () => {
    // A title/label edit of an off-screen task cannot affect any count, so it
    // must not trigger a fallback refetch.
    qc.setQueryData<ListTasksCache>(taskKeys.list(WS_ID), {
      byStatus: { done: { tasks: [], total: 60 } },
    });

    onTaskUpdated(qc, WS_ID, { id: "off-screen", title: "renamed" });

    expect(qc.getQueryState(taskKeys.list(WS_ID))?.isInvalidated).toBe(false);
  });
});

describe("onTaskDeleted", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  it("removes every cache entry scoped directly to the deleted task", () => {
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), baseTask);
    qc.setQueryData<TimelineEntry[]>(taskKeys.timeline(TASK_ID), [
      {
        type: "activity",
        id: "activity-1",
        actor_type: "member",
        actor_id: "user-1",
        action: "created",
        created_at: "2025-01-01T00:00:00Z",
      },
    ]);
    qc.setQueryData<TaskReaction[]>(taskKeys.reactions(TASK_ID), [
      {
        id: "reaction-1",
        task_id: TASK_ID,
        actor_type: "member",
        actor_id: "user-1",
        emoji: "+1",
        created_at: "2025-01-01T00:00:00Z",
      },
    ]);
    qc.setQueryData<TaskSubscriber[]>(taskKeys.subscribers(TASK_ID), [
      {
        task_id: TASK_ID,
        user_type: "member",
        user_id: "user-1",
        reason: "manual",
        created_at: "2025-01-01T00:00:00Z",
      },
    ]);
    qc.setQueryData<TaskUsageSummary>(taskKeys.usage(TASK_ID), {
      total_input_tokens: 10,
      total_output_tokens: 20,
      total_cache_read_tokens: 0,
      total_cache_write_tokens: 0,
      run_count: 1,
    });
    qc.setQueryData<Attachment[]>(taskKeys.attachments(TASK_ID), [
      {
        id: "attachment-1",
        workspace_id: WS_ID,
        task_id: TASK_ID,
        comment_id: null,
        chat_session_id: null,
        chat_message_id: null,
        uploader_type: "member",
        uploader_id: "user-1",
        filename: "evidence.png",
        url: "s3://bucket/evidence.png",
        download_url: "https://example.test/evidence.png",
        markdown_url: "https://example.test/api/attachments/att-1/download",
        content_type: "image/png",
        size_bytes: 1,
        created_at: "2025-01-01T00:00:00Z",
      },
    ]);
    qc.setQueryData<AgentRun[]>(agentRunsKeys.forTask(WS_ID, TASK_ID), [makeTask()]);
    qc.setQueryData<Task[]>(taskKeys.children(WS_ID, TASK_ID), [otherTask]);
    qc.setQueryData<TaskLabelsResponse>(labelKeys.byTask(WS_ID, TASK_ID), {
      labels: [labelA],
    });

    qc.setQueryData<Task>(taskKeys.detail(WS_ID, OTHER_TASK_ID), otherTask);
    qc.setQueryData<TimelineEntry[]>(taskKeys.timeline(OTHER_TASK_ID), []);
    qc.setQueryData<TaskLabelsResponse>(
      labelKeys.byTask(WS_ID, OTHER_TASK_ID),
      { labels: [labelB] },
    );

    onTaskDeleted(qc, WS_ID, TASK_ID);

    expect(qc.getQueryData(taskKeys.detail(WS_ID, TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(taskKeys.timeline(TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(taskKeys.reactions(TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(taskKeys.subscribers(TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(taskKeys.usage(TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(taskKeys.attachments(TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(agentRunsKeys.forTask(WS_ID, TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(taskKeys.children(WS_ID, TASK_ID))).toBeUndefined();
    expect(qc.getQueryData(labelKeys.byTask(WS_ID, TASK_ID))).toBeUndefined();

    expect(qc.getQueryData(taskKeys.detail(WS_ID, OTHER_TASK_ID))).toEqual(
      otherTask,
    );
    expect(qc.getQueryData(taskKeys.timeline(OTHER_TASK_ID))).toEqual([]);
    expect(qc.getQueryData(labelKeys.byTask(WS_ID, OTHER_TASK_ID))).toEqual({
      labels: [labelB],
    });
  });

  it("removes the deleted task from workspace and my-tasks list caches immediately", () => {
    const myFilter = { assignee_id: AGENT_ID };
    qc.setQueryData<ListTasksCache>(
      taskKeys.list(WS_ID),
      makeListCache(baseTask, otherTask),
    );
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "assigned", myFilter),
      makeListCache(baseTask, otherTask),
    );

    onTaskDeleted(qc, WS_ID, TASK_ID);

    const list = qc.getQueryData<ListTasksCache>(taskKeys.list(WS_ID));
    const myList = qc.getQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "assigned", myFilter),
    );
    expect(list?.byStatus.todo?.tasks.map((i) => i.id)).toEqual([
      OTHER_TASK_ID,
    ]);
    expect(list?.byStatus.todo?.total).toBe(1);
    expect(myList?.byStatus.todo?.tasks.map((i) => i.id)).toEqual([
      OTHER_TASK_ID,
    ]);
    expect(myList?.byStatus.todo?.total).toBe(1);
    expectInvalidated(qc, taskKeys.list(WS_ID));
    expectInvalidated(qc, taskKeys.myList(WS_ID, "assigned", myFilter));
  });

  it("invalidates parent progress when the parent id only exists in detail cache", () => {
    qc.setQueryData<Task>(
      taskKeys.detail(WS_ID, TASK_ID),
      parentedTask,
    );
    qc.setQueryData<Task[]>(taskKeys.children(WS_ID, PARENT_TASK_ID), [
      parentedTask,
      otherTask,
    ]);
    qc.setQueryData(taskKeys.childProgress(WS_ID), new Map());

    onTaskDeleted(qc, WS_ID, TASK_ID);

    const parentChildren = qc.getQueryData<Task[]>(
      taskKeys.children(WS_ID, PARENT_TASK_ID),
    );
    expect(parentChildren?.map((i) => i.id)).toEqual([OTHER_TASK_ID]);
    expectInvalidated(qc, taskKeys.children(WS_ID, PARENT_TASK_ID));
    expectInvalidated(qc, taskKeys.childProgress(WS_ID));
  });

  it("invalidates parent progress when the deleted task is only present in a children cache", () => {
    qc.setQueryData<Task[]>(taskKeys.children(WS_ID, PARENT_TASK_ID), [
      parentedTask,
      otherTask,
    ]);
    qc.setQueryData(taskKeys.childProgress(WS_ID), new Map());

    onTaskDeleted(qc, WS_ID, TASK_ID);

    const parentChildren = qc.getQueryData<Task[]>(
      taskKeys.children(WS_ID, PARENT_TASK_ID),
    );
    expect(parentChildren?.map((i) => i.id)).toEqual([OTHER_TASK_ID]);
    expectInvalidated(qc, taskKeys.children(WS_ID, PARENT_TASK_ID));
    expectInvalidated(qc, taskKeys.childProgress(WS_ID));
  });

  it("invalidates parent progress when the parent id only exists in a my-tasks cache", () => {
    const myFilter = { assignee_id: AGENT_ID };
    qc.setQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "assigned", myFilter),
      makeListCache(parentedTask, otherTask),
    );
    qc.setQueryData<Task[]>(taskKeys.children(WS_ID, PARENT_TASK_ID), [
      otherTask,
    ]);
    qc.setQueryData(taskKeys.childProgress(WS_ID), new Map());

    onTaskDeleted(qc, WS_ID, TASK_ID);

    const myList = qc.getQueryData<ListTasksCache>(
      taskKeys.myList(WS_ID, "assigned", myFilter),
    );
    expect(myList?.byStatus.todo?.tasks.map((i) => i.id)).toEqual([
      OTHER_TASK_ID,
    ]);
    expectInvalidated(qc, taskKeys.children(WS_ID, PARENT_TASK_ID));
    expectInvalidated(qc, taskKeys.childProgress(WS_ID));
  });

  it("invalidates child progress when the deleted task is itself a parent", () => {
    qc.setQueryData<Task>(taskKeys.detail(WS_ID, TASK_ID), baseTask);
    qc.setQueryData<Task[]>(taskKeys.children(WS_ID, TASK_ID), [
      {
        ...otherTask,
        parent_task_id: TASK_ID,
      },
    ]);
    qc.setQueryData(
      taskKeys.childProgress(WS_ID),
      new Map([[TASK_ID, { done: 0, total: 1 }]]),
    );

    onTaskDeleted(qc, WS_ID, TASK_ID);

    expect(qc.getQueryData(taskKeys.children(WS_ID, TASK_ID))).toBeUndefined();
    expectInvalidated(qc, taskKeys.childProgress(WS_ID));
  });

  it("invalidates agent task and activity caches that can reference the deleted task", () => {
    qc.setQueryData<AgentRun[]>(
      agentRunSnapshotKeys.list(WS_ID),
      [makeTask()],
    );
    qc.setQueryData<AgentActivityBucket[]>(
      agentActivityKeys.last30d(WS_ID),
      [
        {
          agent_id: AGENT_ID,
          bucket_at: "2025-01-01T00:00:00Z",
          run_count: 1,
          failed_count: 0,
        },
      ],
    );
    qc.setQueryData<AgentRunCount[]>(agentRunCountsKeys.last30d(WS_ID), [
      { agent_id: AGENT_ID, run_count: 1 },
    ]);
    qc.setQueryData<AgentRun[]>(agentRunsKeys.detail(WS_ID, AGENT_ID), [
      makeTask(),
    ]);
    qc.setQueryData<AgentRun[]>(agentRunsKeys.forTask(WS_ID, TASK_ID), [makeTask()]);

    onTaskDeleted(qc, WS_ID, TASK_ID);

    expectInvalidated(qc, agentRunSnapshotKeys.list(WS_ID));
    expectInvalidated(qc, agentActivityKeys.last30d(WS_ID));
    expectInvalidated(qc, agentRunCountsKeys.last30d(WS_ID));
    expectInvalidated(qc, agentRunsKeys.detail(WS_ID, AGENT_ID));
    expect(qc.getQueryData(agentRunsKeys.forTask(WS_ID, TASK_ID))).toBeUndefined();
  });
});

// Regression coverage for the Project Gantt cache. The Gantt view rides its
// own dedicated cache (server-filtered to `scheduled=true`); every WS-driven
// path that can shift Gantt membership has to invalidate the prefix or the
// timeline goes stale.
describe("project gantt cache invalidation", () => {
  const PROJECT_ID = "project-1";
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
    qc.setQueryData<Task[]>(
      taskKeys.projectGantt(WS_ID, PROJECT_ID),
      [baseTask],
    );
  });

  it("invalidates the project Gantt cache on task:created", () => {
    onTaskCreated(qc, WS_ID, otherTask);
    expectInvalidated(qc, taskKeys.projectGantt(WS_ID, PROJECT_ID));
  });

  it("invalidates the project Gantt cache on task:updated", () => {
    onTaskUpdated(qc, WS_ID, {
      id: TASK_ID,
      start_date: "2026-01-01T00:00:00Z",
    });
    expectInvalidated(qc, taskKeys.projectGantt(WS_ID, PROJECT_ID));
  });

  it("invalidates the project Gantt cache on task:deleted", () => {
    onTaskDeleted(qc, WS_ID, TASK_ID);
    expectInvalidated(qc, taskKeys.projectGantt(WS_ID, PROJECT_ID));
  });
});
