/**
 * Centralised TanStack Query keys for task-domain queries on mobile.
 *
 * Prefix shape mirrors web's `packages/core/tasks/queries.ts` so the same
 * WS invalidation surface (e.g. `invalidateQueries({ queryKey: taskKeys.myAll(wsId) })`)
 * eventually drives both clients. Keys are workspace-scoped — switching
 * workspace flips wsId and the cache moves automatically (root CLAUDE.md
 * "Workspace-scoped queries must key on wsId").
 */
import type { ListTasksParams } from "@silieco/core/types";

export type MyTasksScope = "assigned" | "created" | "agents";

export type MyTasksFilter = Pick<
  ListTasksParams,
  "assignee_id" | "assignee_ids" | "creator_id" | "involves_user_id"
>;

export const taskKeys = {
  all: (wsId: string | null) => ["tasks", wsId] as const,
  list: (wsId: string | null) => [...taskKeys.all(wsId), "list"] as const,
  myAll: (wsId: string | null) => [...taskKeys.all(wsId), "my"] as const,
  myList: (
    wsId: string | null,
    scope: MyTasksScope,
    filter: MyTasksFilter,
  ) => [...taskKeys.myAll(wsId), scope, filter] as const,
  detail: (wsId: string | null, id: string) =>
    [...taskKeys.all(wsId), "detail", id] as const,
  timeline: (wsId: string | null, id: string) =>
    [...taskKeys.all(wsId), "timeline", id] as const,
  // File attachments hooked to a task (and its comments). Used by the
  // markdown renderer to resolve `mc://file/<id>` URIs to download_url.
  attachments: (wsId: string | null, id: string) =>
    [...taskKeys.all(wsId), "attachments", id] as const,
};
