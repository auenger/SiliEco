import { describe, expect, it } from "vitest";
import type { Task } from "@silieco/core/types";
import {
  getTaskGroupId,
  getMoveAnchors,
  getMoveUpdates,
  insertIdByPosition,
  taskMatchesGroup,
  propertyGroupId,
} from "./drag-utils";

function mk(id: string, position: number): Task {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `SILI-${id}`,
    title: id,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_task_id: null,
    project_id: null,
    position,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    labels: [],
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  };
}

function mapOf(...tasks: Task[]): Map<string, Task> {
  return new Map(tasks.map((i) => [i.id, i]));
}

describe("getMoveAnchors", () => {
  it("derives relative neighbors from the optimistic order", () => {
    expect(getMoveAnchors(["a", "moving", "b"], "moving")).toEqual({
      before_id: "a",
      after_id: "b",
    });
    expect(getMoveAnchors(["moving"], "moving")).toEqual({
      before_id: null,
      after_id: null,
    });
  });
});

describe("insertIdByPosition", () => {
  it("inserts the id at its position-sorted slot", () => {
    const map = mapOf(mk("a", 1), mk("c", 3), mk("b", 2));
    expect(insertIdByPosition(["a", "c"], "b", 2, map)).toEqual([
      "a",
      "b",
      "c",
    ]);
  });

  it("appends when the position is the largest", () => {
    const map = mapOf(mk("a", 1), mk("z", 9));
    expect(insertIdByPosition(["a"], "z", 9, map)).toEqual(["a", "z"]);
  });

  it("prepends when the position is the smallest", () => {
    const map = mapOf(mk("b", 2), mk("a", 1));
    expect(insertIdByPosition(["b"], "a", 1, map)).toEqual(["a", "b"]);
  });

  it("appends into an empty target column", () => {
    const map = mapOf(mk("a", 5));
    expect(insertIdByPosition([], "a", 5, map)).toEqual(["a"]);
  });

  it("matches insertByPosition ordering so the settle rebuild is a no-op", () => {
    // Same scenario the board's optimistic drop and the cache patch both apply:
    // landing a card between two neighbours must produce the same order in the
    // id list (board) and the task list (cache).
    const map = mapOf(mk("x", 1), mk("y", 3), mk("moved", 2));
    expect(insertIdByPosition(["x", "y"], "moved", 2, map)).toEqual([
      "x",
      "moved",
      "y",
    ]);
  });
});

describe("property grouping", () => {
  const propertyId = "prop-env";
  const withValue = { id: "A", properties: { [propertyId]: "opt-staging" } } as unknown as Task;
  const withoutValue = { id: "B", properties: {} } as unknown as Task;

  it("getTaskGroupId buckets by option id, no-value tasks into the none column", () => {
    expect(getTaskGroupId(withValue, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, "opt-staging"),
    );
    expect(getTaskGroupId(withoutValue, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, null),
    );
  });

  it("taskMatchesGroup distinguishes option and no-value columns", () => {
    const optionColumn = { id: "c1", title: "Staging", propertyId, propertyOptionId: "opt-staging" };
    const noneColumn = { id: "c2", title: "No value", propertyId, propertyOptionId: null };
    expect(taskMatchesGroup(withValue, optionColumn)).toBe(true);
    expect(taskMatchesGroup(withValue, noneColumn)).toBe(false);
    expect(taskMatchesGroup(withoutValue, noneColumn)).toBe(true);
  });

  it("unknown option values bucket into the none column when the catalog is known", () => {
    const stale = { id: "C", properties: { [propertyId]: "opt-deleted" } } as unknown as Task;
    const known = new Set(["opt-staging"]);
    expect(getTaskGroupId(stale, `property:${propertyId}`, known)).toBe(
      propertyGroupId(propertyId, null),
    );
    // Without the catalog, the raw bucket is preserved (caller may still map it).
    expect(getTaskGroupId(stale, `property:${propertyId}`)).toBe(
      propertyGroupId(propertyId, "opt-deleted"),
    );
  });

  it("getMoveUpdates for property columns only carries position", () => {
    expect(getMoveUpdates({ id: "c1", title: "Staging", propertyId, propertyOptionId: "opt-staging" }, 5)).toEqual({ position: 5 });
  });
});
