import { describe, expect, it } from "vitest";
import {
  UnsupportedTaskScopeError,
  taskScopeKey,
} from "./scope";
import { buildTaskSurfaceQueryPlan } from "./query-plan";

describe("task surface scope", () => {
  it("builds stable surface keys", () => {
    expect(taskScopeKey({ type: "workspace" })).toBe("workspace:all");
    expect(taskScopeKey({ type: "workspace", actorKind: "agents" })).toBe(
      "workspace:agents",
    );
    expect(
      taskScopeKey({ type: "my", relation: "assigned", userId: "u1" }),
    ).toBe("my:u1:assigned");
    expect(taskScopeKey({ type: "project", projectId: "p1" })).toBe(
      "project:p1",
    );
    expect(
      taskScopeKey({
        type: "actor",
        actorType: "agent",
        actorId: "a1",
        relation: "created",
      }),
    ).toBe("actor:agent:a1:created");
    expect(taskScopeKey({ type: "team", teamId: "t1" })).toBe("team:t1");
  });

  it("builds the workspace query plan", () => {
    // The unfiltered tab shares the workspace list cache.
    expect(buildTaskSurfaceQueryPlan({ type: "workspace" })).toMatchObject({
      kind: "workspace",
      scopeKey: "workspace:all",
      queryFilter: {},
      groupedScopeFilter: {},
      createDefaults: {},
    });

    // Members/Agents tabs are server-filtered scoped lists: assignee_types
    // rides the list + grouped requests, and each tab owns its cache entry
    // (correct totals + load-more, no client post-filtering).
    expect(
      buildTaskSurfaceQueryPlan({ type: "workspace", actorKind: "agents" }),
    ).toMatchObject({
      kind: "scoped",
      scopeKey: "workspace:agents",
      queryScope: "workspace:agents",
      queryFilter: { assignee_types: ["agent", "squad"] },
      groupedScopeFilter: { assignee_types: ["agent", "squad"] },
      loadMoreScope: "workspace:agents",
      loadMoreFilter: { assignee_types: ["agent", "squad"] },
      createDefaults: {},
    });
    expect(
      buildTaskSurfaceQueryPlan({ type: "workspace", actorKind: "members" }),
    ).toMatchObject({
      kind: "scoped",
      scopeKey: "workspace:members",
      queryFilter: { assignee_types: ["member"] },
      groupedScopeFilter: { assignee_types: ["member"] },
      createDefaults: {},
    });
  });

  it("builds personal task query plans using the existing query contracts", () => {
    expect(
      buildTaskSurfaceQueryPlan({
        type: "my",
        relation: "assigned",
        userId: "u1",
      }),
    ).toMatchObject({
      scopeKey: "my:u1:assigned",
      queryScope: "assigned",
      queryFilter: { assignee_id: "u1" },
      groupedScopeFilter: { assignee_id: "u1" },
      loadMoreScope: "assigned",
      loadMoreFilter: { assignee_id: "u1" },
      userId: undefined,
      createDefaults: { assignee_type: "member", assignee_id: "u1" },
    });
    expect(
      buildTaskSurfaceQueryPlan({
        type: "my",
        relation: "created",
        userId: "u1",
      }),
    ).toMatchObject({
      queryScope: "created",
      queryFilter: { creator_id: "u1" },
      createDefaults: {},
    });
    expect(
      buildTaskSurfaceQueryPlan({
        type: "my",
        relation: "involved",
        userId: "u1",
      }),
    ).toMatchObject({
      queryScope: "agents",
      queryFilter: { involves_user_id: "u1" },
      createDefaults: {},
    });
    expect(
      buildTaskSurfaceQueryPlan({
        type: "my",
        relation: "all",
        userId: "u1",
      }),
    ).toMatchObject({
      queryScope: "all",
      queryFilter: {},
      groupedScopeFilter: {},
      userId: "u1",
      createDefaults: {},
    });
  });

  it("builds project and actor query plans", () => {
    expect(
      buildTaskSurfaceQueryPlan({ type: "project", projectId: "p1" }),
    ).toMatchObject({
      scopeKey: "project:p1",
      queryScope: "project:p1",
      queryFilter: { project_id: "p1" },
      groupedScopeFilter: { project_id: "p1" },
      createDefaults: { project_id: "p1" },
    });

    expect(
      buildTaskSurfaceQueryPlan({
        type: "actor",
        actorType: "agent",
        actorId: "a1",
        relation: "assigned",
      }),
    ).toMatchObject({
      scopeKey: "actor:agent:a1:assigned",
      queryScope: "actor:agent:a1:assigned",
      queryFilter: { assignee_id: "a1" },
      groupedScopeFilter: { assignee_id: "a1" },
      createDefaults: { assignee_type: "agent", assignee_id: "a1" },
    });
  });

  it("throws for team until the task API has a team filter", () => {
    const scope = { type: "team" as const, teamId: "t1" };
    expect(() => buildTaskSurfaceQueryPlan(scope)).toThrow(
      UnsupportedTaskScopeError,
    );
  });
});
