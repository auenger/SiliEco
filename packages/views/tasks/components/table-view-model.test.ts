import { describe, expect, it } from "vitest";
import type { Task } from "@silieco/core/types";
import {
  buildTaskTableCsv,
  calculateTaskTableColumn,
  getTaskTableSelectionRange,
  refreshFrozenTableRows,
  type TaskTableDisplayRow,
} from "./table-view-model";

function makeTask(id: string, overrides: Partial<Task> = {}): Task {
  const number = Number(id.replace(/\D/g, "")) || 1;
  return {
    id,
    workspace_id: "ws-1",
    number,
    identifier: `SILI-${number}`,
    title: `Task ${id}`,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_task_id: null,
    project_id: null,
    position: number,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("getTaskTableSelectionRange", () => {
  const taskIds = ["task-1", "task-2", "task-3", "task-4"];

  it("returns an inclusive range in either direction", () => {
    expect(
      getTaskTableSelectionRange(taskIds, "task-1", "task-4"),
    ).toEqual(taskIds);
    expect(
      getTaskTableSelectionRange(taskIds, "task-4", "task-2"),
    ).toEqual(["task-2", "task-3", "task-4"]);
  });

  it("returns null when the anchor or target is not visible", () => {
    expect(getTaskTableSelectionRange(taskIds, null, "task-2")).toBeNull();
    expect(
      getTaskTableSelectionRange(taskIds, "missing", "task-2"),
    ).toBeNull();
    expect(
      getTaskTableSelectionRange(taskIds, "task-1", "missing"),
    ).toBeNull();
  });
});

describe("refreshFrozenTableRows", () => {
  const groupRow: TaskTableDisplayRow = {
    kind: "group",
    key: "status:todo",
    label: "Todo",
    count: 2,
    collapsed: false,
  };

  it("keeps structure and keys while swapping in live task objects", () => {
    const staleA = makeTask("task-1", { title: "Stale A" });
    const staleB = makeTask("task-2", { title: "Stale B" });
    const snapshot: TaskTableDisplayRow[] = [
      groupRow,
      {
        kind: "task",
        key: staleA.id,
        task: staleA,
        depth: 0,
        hasChildren: true,
        collapsed: false,
      },
      {
        kind: "task",
        key: staleB.id,
        task: staleB,
        depth: 1,
        hasChildren: false,
        collapsed: false,
      },
    ];
    const liveA = makeTask("task-1", { title: "Live A" });

    const refreshed = refreshFrozenTableRows(
      snapshot,
      new Map([
        [liveA.id, liveA],
        [staleB.id, staleB],
      ]),
    );

    expect(refreshed.map((row) => row.key)).toEqual([
      "status:todo",
      "task-1",
      "task-2",
    ]);
    expect(refreshed[1]).toMatchObject({
      task: liveA,
      depth: 0,
      hasChildren: true,
    });
    expect(refreshed[2]).toBe(snapshot[2]);
    expect(refreshed[0]).toBe(groupRow);
  });

  it("keeps the stale task when it vanished from the live window", () => {
    const stale = makeTask("task-9", { title: "Deleted remotely" });
    const snapshot: TaskTableDisplayRow[] = [
      {
        kind: "task",
        key: stale.id,
        task: stale,
        depth: 0,
        hasChildren: false,
        collapsed: false,
      },
    ];

    const refreshed = refreshFrozenTableRows(snapshot, new Map());

    expect(refreshed[0]).toBe(snapshot[0]);
  });
});

describe("table calculations and CSV", () => {
  it("calculates numeric custom-property sums, averages, and counts", () => {
    const tasks = [
      makeTask("task-1", { properties: { estimate: 3 } }),
      makeTask("task-2", { properties: { estimate: 5 } }),
      makeTask("task-3"),
    ];

    expect(
      calculateTaskTableColumn(tasks, "property:estimate", "sum"),
    ).toBe(8);
    expect(
      calculateTaskTableColumn(tasks, "property:estimate", "average"),
    ).toBe(4);
    expect(
      calculateTaskTableColumn(tasks, "property:estimate", "count"),
    ).toBe(2);
  });

  it("escapes commas, quotes, and newlines in CSV output", () => {
    expect(
      buildTaskTableCsv(
        ["Identifier", "Title"],
        [["SILI-1", 'Ship, "verify"\nnext']],
      ),
    ).toBe('Identifier,Title\r\nSILI-1,"Ship, ""verify""\nnext"');
  });

  it("neutralizes spreadsheet formulas in headers and string cells", () => {
    expect(
      buildTaskTableCsv(
        ["=Injected", "Value"],
        [["+SUM(A1:A2)", -42], ["\tcmd", "@remote"]],
      ),
    ).toBe(
      "'=Injected,Value\r\n'+SUM(A1:A2),-42\r\n'\tcmd,'@remote",
    );
  });
});
