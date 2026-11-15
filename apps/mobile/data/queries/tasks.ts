/**
 * Task queries — workspace-wide list, single-task detail, timeline.
 * Mobile-owned; mirrors a strict subset of packages/core/tasks/queries.ts.
 *
 * Query keys live in ./task-keys so detail / timeline / list / myList all
 * sit under the `tasks/<wsId>` prefix — WS handlers can invalidate the
 * whole subtree with one call when needed.
 */
import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";
import { taskKeys } from "./task-keys";

export { taskKeys } from "./task-keys";

/**
 * Workspace-wide task list. Backend filters by `X-Workspace-Slug` header
 * (root CLAUDE.md "All queries filter by workspace_id"), so we pass an
 * empty params object — server returns every task the user is allowed to
 * see in the current workspace.
 *
 * Cache shape: flat `Task[]` (we strip `.tasks` from the response) so
 * the WS updaters can patch this list with the same shape as
 * myTaskListOptions. Pagination is deferred — web's `TasksPage` also
 * fetches all in one shot today (`packages/views/tasks/components/
 * tasks-page.tsx:30`).
 */
export const taskListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: taskKeys.list(wsId),
    queryFn: async ({ signal }) => {
      const res = await api.listTasks({}, { signal });
      return res.tasks;
    },
    enabled: !!wsId,
  });

export const taskDetailOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: taskKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getTask(id, { signal }),
    enabled: !!wsId && !!id,
  });

/**
 * Single query over the full task timeline (ASC, oldest first). Mirrors
 * web's `taskTimelineOptions` post-#2322 — server returns the whole list
 * in one shot, client-side pagination was deleted.
 */
export const taskTimelineOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: taskKeys.timeline(wsId, id),
    queryFn: ({ signal }) => api.listTimeline(id, { signal }),
    enabled: !!wsId && !!id,
  });

/**
 * File attachments uploaded to this task or any of its comments. The
 * mobile markdown renderer reads this list to resolve `mc://file/<id>`
 * URIs in image markdown to a real HTTPS `download_url` that iOS can
 * actually load — see `lib/markdown/markdown-image.tsx`.
 *
 * TanStack Query dedupes the request across concurrent callers, so it's
 * safe for both TaskDescription and CommentCard to fetch the same
 * task's attachments — only one network request fires.
 */
export const taskAttachmentsOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: taskKeys.attachments(wsId, id),
    queryFn: ({ signal }) => api.listAttachments(id, { signal }),
    enabled: !!wsId && !!id,
  });
