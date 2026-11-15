/**
 * "My Tasks" list, server-filtered by scope. Mirrors the three scopes web
 * exposes in `packages/views/my-tasks/components/my-tasks-page.tsx:48-65`:
 *   - assigned: tasks where assignee_id = me
 *   - created:  tasks where creator_id  = me
 *   - agents:   tasks where the assignee is an *indirect* extension of me —
 *               an owned agent, OR a squad I'm a human member of, lead, or
 *               have an owned agent inside. Driven server-side by the
 *               `involves_user_id` predicate (see SILI-2397, 2026-05-19).
 *               Direct member assignment is intentionally EXCLUDED — that's
 *               the `assigned` scope's meaning.
 *
 * Cache key shape is `taskKeys.myList(wsId, scope, filter)` — same prefix
 * as web's `packages/core/tasks/queries.ts` so a future WS handler can
 * invalidate `taskKeys.myAll(wsId)` and reach both clients.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";
import {
  taskKeys,
  type MyTasksFilter,
  type MyTasksScope,
} from "./task-keys";

export function buildMyTasksFilter(
  scope: MyTasksScope,
  userId: string,
): MyTasksFilter {
  switch (scope) {
    case "assigned":
      return { assignee_id: userId };
    case "created":
      return { creator_id: userId };
    case "agents":
      return { involves_user_id: userId };
  }
}

export const myTaskListOptions = (
  wsId: string | null,
  scope: MyTasksScope,
  filter: MyTasksFilter,
) =>
  queryOptions({
    queryKey: taskKeys.myList(wsId, scope, filter),
    queryFn: async ({ signal }) => {
      const res = await api.listTasks(filter, { signal });
      return res.tasks;
    },
    enabled: !!wsId,
  });
