import type { TaskAssigneeType } from "../../types";

export type WorkspaceTaskActorKind = "all" | "members" | "agents";

export type TaskScope =
  | { type: "workspace"; actorKind?: WorkspaceTaskActorKind }
  | {
      type: "my";
      relation: "all" | "assigned" | "created" | "involved";
      userId: string;
    }
  | { type: "project"; projectId: string }
  | {
      type: "actor";
      actorType: Extract<TaskAssigneeType, "member" | "agent">;
      actorId: string;
      relation: "assigned" | "created";
    }
  | { type: "team"; teamId: string };

export class UnsupportedTaskScopeError extends Error {
  constructor(scope: TaskScope, operation: string) {
    super(`Task scope "${taskScopeKey(scope)}" is not supported for ${operation}.`);
    this.name = "UnsupportedTaskScopeError";
  }
}

export function taskScopeKey(scope: TaskScope): string {
  switch (scope.type) {
    case "workspace":
      return `workspace:${scope.actorKind ?? "all"}`;
    case "my":
      return `my:${scope.userId}:${scope.relation}`;
    case "project":
      return `project:${scope.projectId}`;
    case "actor":
      return `actor:${scope.actorType}:${scope.actorId}:${scope.relation}`;
    case "team":
      return `team:${scope.teamId}`;
  }
}
