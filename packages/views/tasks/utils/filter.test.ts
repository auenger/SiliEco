import { describe, it, expect } from "vitest";
import type { Task, TaskAssigneeGroup } from "@silieco/core/types";
import {
  applyTaskFilters,
  filterAssigneeGroups,
  filterTasks,
  type TaskFilters,
} from "./filter";

const NO_FILTER: TaskFilters = {
  statusFilters: [],
  priorityFilters: [],
  assigneeFilters: [],
  includeNoAssignee: false,
  creatorFilters: [],
  projectFilters: [],
  includeNoProject: false,
  labelFilters: [],
};

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: "i-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "SILI-1",
    title: "Test",
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "u-1",
    parent_task_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    ...overrides,
  };
}

const tasks: Task[] = [
  makeTask({ id: "1", status: "todo", priority: "high", assignee_type: "member", assignee_id: "u-1", creator_type: "member", creator_id: "u-1", project_id: "p-1" }),
  makeTask({ id: "2", status: "in_progress", priority: "medium", assignee_type: "agent", assignee_id: "a-1", creator_type: "agent", creator_id: "a-1", project_id: "p-2" }),
  makeTask({ id: "3", status: "done", priority: "low", assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "u-2", project_id: null }),
  makeTask({ id: "4", status: "todo", priority: "urgent", assignee_type: "member", assignee_id: "u-2", creator_type: "member", creator_id: "u-1", project_id: "p-1" }),
];

describe("filterTasks", () => {
  it("returns all tasks when no filters are active", () => {
    expect(filterTasks(tasks, NO_FILTER)).toHaveLength(4);
  });

  // --- Status ---
  it("filters by status", () => {
    const result = filterTasks(tasks, { ...NO_FILTER, statusFilters: ["todo"] });
    expect(result.map((i) => i.id)).toEqual(["1", "4"]);
  });

  // --- Priority ---
  it("filters by priority", () => {
    const result = filterTasks(tasks, { ...NO_FILTER, priorityFilters: ["high", "urgent"] });
    expect(result.map((i) => i.id)).toEqual(["1", "4"]);
  });

  // --- Assignee ---
  it("filters by specific assignee", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      assigneeFilters: [{ type: "member", id: "u-1" }],
    });
    expect(result.map((i) => i.id)).toEqual(["1"]);
  });

  it("filters by 'No assignee' only", () => {
    const result = filterTasks(tasks, { ...NO_FILTER, includeNoAssignee: true });
    expect(result.map((i) => i.id)).toEqual(["3"]);
  });

  it("filters by assignee + No assignee combined", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      assigneeFilters: [{ type: "agent", id: "a-1" }],
      includeNoAssignee: true,
    });
    expect(result.map((i) => i.id)).toEqual(["2", "3"]);
  });

  it("treats an explicitly active empty assignee predicate as match-none", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      assigneeFilterActive: true,
    });
    expect(result).toEqual([]);
  });

  it("hides assigned tasks when only 'No assignee' is selected", () => {
    const result = filterTasks(tasks, { ...NO_FILTER, includeNoAssignee: true });
    expect(result.every((i) => !i.assignee_id)).toBe(true);
  });

  // --- Creator ---
  it("filters by creator", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      creatorFilters: [{ type: "agent", id: "a-1" }],
    });
    expect(result.map((i) => i.id)).toEqual(["2"]);
  });

  // --- Combinations ---
  it("applies status + assignee filters together", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      statusFilters: ["todo"],
      assigneeFilters: [{ type: "member", id: "u-1" }],
    });
    expect(result.map((i) => i.id)).toEqual(["1"]);
  });

  it("applies status + priority + creator filters together", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      statusFilters: ["todo"],
      priorityFilters: ["urgent"],
      creatorFilters: [{ type: "member", id: "u-1" }],
    });
    expect(result.map((i) => i.id)).toEqual(["4"]);
  });

  // --- Project ---
  it("filters by specific project", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      projectFilters: ["p-1"],
    });
    expect(result.map((i) => i.id)).toEqual(["1", "4"]);
  });

  it("filters by multiple projects", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      projectFilters: ["p-1", "p-2"],
    });
    expect(result.map((i) => i.id)).toEqual(["1", "2", "4"]);
  });

  it("filters by 'No project' only", () => {
    const result = filterTasks(tasks, { ...NO_FILTER, includeNoProject: true });
    expect(result.map((i) => i.id)).toEqual(["3"]);
  });

  it("filters by project + No project combined", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      projectFilters: ["p-2"],
      includeNoProject: true,
    });
    expect(result.map((i) => i.id)).toEqual(["2", "3"]);
  });

  it("hides project tasks when only 'No project' is selected", () => {
    const result = filterTasks(tasks, { ...NO_FILTER, includeNoProject: true });
    expect(result.every((i) => !i.project_id)).toBe(true);
  });

  it("applies status + project filters together", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      statusFilters: ["todo"],
      projectFilters: ["p-1"],
    });
    expect(result.map((i) => i.id)).toEqual(["1", "4"]);
  });

  // --- Label ---
  // Build a separate fixture for label tests so we can sprinkle labels onto
  // specific rows without polluting the assignee/project test cases above.
  const makeLabel = (id: string, name: string, color: string) => ({
    id,
    name,
    color,
    workspace_id: "ws-1",
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  });
  const labelBug = makeLabel("lab-bug", "bug", "#ff0000");
  const labelFeat = makeLabel("lab-feat", "feature", "#00ff00");
  const labelP0 = makeLabel("lab-p0", "p0", "#0000ff");
  const labeledTasks: Task[] = [
    makeTask({ id: "L1", labels: [labelBug] }),
    makeTask({ id: "L2", labels: [labelFeat] }),
    makeTask({ id: "L3", labels: [labelBug, labelP0] }),
    makeTask({ id: "L4", labels: [] }),
    makeTask({ id: "L5" }), // labels field absent
  ];

  it("filters by a single label", () => {
    const result = filterTasks(labeledTasks, { ...NO_FILTER, labelFilters: ["lab-bug"] });
    expect(result.map((i) => i.id)).toEqual(["L1", "L3"]);
  });

  it("filters by multiple labels with OR semantics", () => {
    const result = filterTasks(labeledTasks, {
      ...NO_FILTER,
      labelFilters: ["lab-bug", "lab-feat"],
    });
    expect(result.map((i) => i.id)).toEqual(["L1", "L2", "L3"]);
  });

  it("excludes tasks with no labels when a label filter is active", () => {
    const result = filterTasks(labeledTasks, { ...NO_FILTER, labelFilters: ["lab-bug"] });
    // L4 (empty labels) and L5 (missing labels field) must both be filtered out.
    expect(result.map((i) => i.id)).not.toContain("L4");
    expect(result.map((i) => i.id)).not.toContain("L5");
  });

  // --- Agent running quick filter ---
  it("keeps only running tasks when agentRunningFilter is on", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      agentRunningFilter: true,
      runningTaskIds: new Set(["2", "4"]),
    });
    expect(result.map((i) => i.id)).toEqual(["2", "4"]);
  });

  it("hides everything when agentRunningFilter is on but no ids running", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      agentRunningFilter: true,
      runningTaskIds: new Set(),
    });
    expect(result).toHaveLength(0);
  });

  it("ignores runningTaskIds when agentRunningFilter is off", () => {
    // The set is irrelevant unless the toggle is true — this guards against
    // a future refactor accidentally applying the set as an implicit
    // pre-filter when the user hasn't asked for it.
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      runningTaskIds: new Set(["2"]),
    });
    expect(result).toHaveLength(4);
  });

  it("composes agentRunningFilter with other filters (AND semantics)", () => {
    const result = filterTasks(tasks, {
      ...NO_FILTER,
      statusFilters: ["todo"],
      agentRunningFilter: true,
      runningTaskIds: new Set(["1", "2"]),
    });
    // Task 2 is in_progress (filtered out by status), task 1 is todo and
    // in the running set → only "1" survives.
    expect(result.map((i) => i.id)).toEqual(["1"]);
  });

  it("applies workingOnly from activity context without treating queued tasks as working", () => {
    const result = applyTaskFilters(
      tasks,
      {
        ...NO_FILTER,
        workingOnly: true,
      },
      {
        activityByTaskId: new Map([
          ["1", { isWorking: true, isQueued: false, runningTasks: [], queuedTasks: [] }],
          ["2", { isWorking: false, isQueued: true, runningTasks: [], queuedTasks: [] }],
        ]),
      },
    );

    expect(result.map((i) => i.id)).toEqual(["1"]);
  });

  // --- Show sub-tasks display toggle ---
  const parentChildTasks: Task[] = [
    makeTask({ id: "P1", parent_task_id: null }),
    makeTask({ id: "C1", parent_task_id: "P1" }),
    makeTask({ id: "P2", parent_task_id: null }),
    makeTask({ id: "C2", parent_task_id: "P2" }),
  ];

  it("hides sub-tasks when showSubTasks is false", () => {
    const result = filterTasks(parentChildTasks, {
      ...NO_FILTER,
      showSubTasks: false,
    });
    expect(result.map((i) => i.id)).toEqual(["P1", "P2"]);
  });

  it("keeps sub-tasks when showSubTasks is true or omitted", () => {
    expect(
      filterTasks(parentChildTasks, { ...NO_FILTER, showSubTasks: true }),
    ).toHaveLength(4);
    // Omitting the flag entirely must preserve the show-all default.
    expect(filterTasks(parentChildTasks, NO_FILTER)).toHaveLength(4);
  });

  it("composes showSubTasks with other filters (AND semantics)", () => {
    const mixed: Task[] = [
      makeTask({ id: "P", status: "todo", parent_task_id: null }),
      makeTask({ id: "C", status: "todo", parent_task_id: "P" }),
      makeTask({ id: "PD", status: "done", parent_task_id: null }),
    ];
    const result = filterTasks(mixed, {
      ...NO_FILTER,
      statusFilters: ["todo"],
      showSubTasks: false,
    });
    // "C" is a sub-task (dropped), "PD" is done (dropped) → only "P" survives.
    expect(result.map((i) => i.id)).toEqual(["P"]);
  });
});

describe("filterAssigneeGroups", () => {
  const group = (id: string, groupTasks: Task[]): TaskAssigneeGroup => ({
    id,
    assignee_type: id === "none" ? null : "agent",
    assignee_id: id === "none" ? null : id,
    tasks: groupTasks,
    total: groupTasks.length,
  });

  it("returns the same reference when no client-side filter is active", () => {
    const groups = [group("a1", [makeTask({ id: "1" })])];
    expect(filterAssigneeGroups(groups, {})).toBe(groups);
    expect(filterAssigneeGroups(groups, { showSubTasks: true })).toBe(groups);
    expect(filterAssigneeGroups(groups, { agentRunningFilter: false })).toBe(groups);
  });

  it("passes undefined through untouched", () => {
    expect(filterAssigneeGroups(undefined, { showSubTasks: false })).toBeUndefined();
  });

  it("hides sub-tasks, recomputes total, and drops emptied groups", () => {
    const groups = [
      group("a1", [
        makeTask({ id: "P1", parent_task_id: null }),
        makeTask({ id: "C1", parent_task_id: "P1" }),
      ]),
      // Every task in this group is a sub-task → group is removed entirely.
      group("a2", [makeTask({ id: "C2", parent_task_id: "P2" })]),
    ];
    const result = filterAssigneeGroups(groups, { showSubTasks: false });
    expect(
      result!.map((g) => ({ id: g.id, ids: g.tasks.map((i) => i.id), total: g.total })),
    ).toEqual([{ id: "a1", ids: ["P1"], total: 1 }]);
  });

  it("keeps only running tasks when agentRunningFilter is on", () => {
    const groups = [
      group("a1", [makeTask({ id: "1" }), makeTask({ id: "2" })]),
      group("a2", [makeTask({ id: "3" })]),
      group("none", [makeTask({ id: "4" })]),
    ];
    const result = filterAssigneeGroups(groups, {
      agentRunningFilter: true,
      runningTaskIds: new Set(["2", "4"]),
    });
    expect(
      result!.map((g) => ({ id: g.id, ids: g.tasks.map((i) => i.id), total: g.total })),
    ).toEqual([
      { id: "a1", ids: ["2"], total: 1 },
      { id: "none", ids: ["4"], total: 1 },
    ]);
  });

  it("composes showSubTasks and agentRunningFilter (AND semantics)", () => {
    const groups = [
      group("a1", [
        makeTask({ id: "P", parent_task_id: null }),
        makeTask({ id: "C", parent_task_id: "P" }),
      ]),
    ];
    // "C" is running but is a sub-task; "P" is a top-level task but not
    // running → both dropped, group removed.
    const result = filterAssigneeGroups(groups, {
      showSubTasks: false,
      agentRunningFilter: true,
      runningTaskIds: new Set(["C"]),
    });
    expect(result).toEqual([]);
  });
});

describe("property filters", () => {
  const sevId = "prop-severity";
  const platId = "prop-platforms";
  const doneId = "prop-done";
  const critical = makeTask({ id: "P1", properties: { [sevId]: "opt-critical" } });
  const minor = makeTask({ id: "P2", properties: { [sevId]: "opt-minor", [platId]: ["opt-ios", "opt-web"] } });
  const unset = makeTask({ id: "P3" });
  const checked = makeTask({ id: "P4", properties: { [doneId]: true } });

  it("select values match by option id (OR within the definition)", () => {
    const result = filterTasks([critical, minor, unset], {
      ...NO_FILTER,
      propertyFilters: { [sevId]: ["opt-critical", "opt-minor"] },
    });
    expect(result.map((i) => i.id)).toEqual(["P1", "P2"]);
  });

  it("tasks without a value never match a filtered definition", () => {
    const result = filterTasks([critical, unset], {
      ...NO_FILTER,
      propertyFilters: { [sevId]: ["opt-critical"] },
    });
    expect(result.map((i) => i.id)).toEqual(["P1"]);
  });

  it("multi_select matches on intersection", () => {
    const result = filterTasks([critical, minor], {
      ...NO_FILTER,
      propertyFilters: { [platId]: ["opt-web"] },
    });
    expect(result.map((i) => i.id)).toEqual(["P2"]);
  });

  it("checkbox values match the true/false pseudo-options", () => {
    const result = filterTasks([checked, unset], {
      ...NO_FILTER,
      propertyFilters: { [doneId]: ["true"] },
    });
    expect(result.map((i) => i.id)).toEqual(["P4"]);
  });

  it("ANDs across definitions", () => {
    const result = filterTasks([critical, minor], {
      ...NO_FILTER,
      propertyFilters: { [sevId]: ["opt-minor"], [platId]: ["opt-ios"] },
    });
    expect(result.map((i) => i.id)).toEqual(["P2"]);
  });

  it("empty selections are inert", () => {
    const result = filterTasks([critical, minor, unset], {
      ...NO_FILTER,
      propertyFilters: { [sevId]: [] },
    });
    expect(result).toHaveLength(3);
  });

  it("filterAssigneeGroups applies property filters per group", () => {
    const groups: TaskAssigneeGroup[] = [
      { id: "assignee:member:u-1", assignee_type: "member", assignee_id: "u-1", tasks: [critical, minor], total: 2 },
    ];
    const result = filterAssigneeGroups(groups, {
      propertyFilters: { [sevId]: ["opt-critical"] },
    });
    expect(result?.[0]?.tasks.map((i) => i.id)).toEqual(["P1"]);
    expect(result?.[0]?.total).toBe(1);
  });
});
