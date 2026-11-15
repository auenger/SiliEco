/**
 * Per-project realtime subscriptions. Mounted by the project detail screen
 * with the active project id; cleans up on navigate-away.
 *
 * Filters every event by id match (`project.id === projectId` for project
 * events, `task.project_id === projectId` for task events) so the hook
 * only mutates the caches it owns (apps/mobile/CLAUDE.md "Realtime → Mount
 * strategy").
 *
 * Handles:
 *   - project:updated → replace detail cache (payload is full Project).
 *   - project:deleted → drop detail + resources, then fire `onDeleted` so
 *                       the screen pops back instead of stranding the user
 *                       on a 404 page.
 *   - task:updated/created/deleted → patch the related-tasks cache so
 *                       the list under the project stays in sync.
 *                       Listing-level hooks (use-my-tasks-realtime) only
 *                       patch `taskKeys.myAll(wsId)`; this cache lives
 *                       under `taskKeys.list(wsId)` with a "byProject"
 *                       suffix and isn't covered by them.
 *   - reconnect       → invalidate detail + resources + related-tasks
 *                       (we may have missed events while disconnected).
 *
 * `project:created` is not relevant to the per-record hook (no id match).
 */
import { useQueryClient } from "@tanstack/react-query";
import type { Task } from "@silieco/core/types";
import { taskKeys } from "@/data/queries/task-keys";
import { projectKeys } from "@/data/queries/projects";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  clearProjectDetail,
  patchProjectDetail,
  removeFromProjectsList,
} from "./project-ws-updaters";

export function useProjectRealtime(
  projectId: string | undefined,
  onDeleted?: () => void,
) {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      if (!projectId) return;

      const taskListKey = [
        ...taskKeys.list(wsId),
        "byProject",
        projectId,
      ] as const;

      const invalidateThisProject = () => {
        qc.invalidateQueries({ queryKey: projectKeys.detail(wsId, projectId) });
        qc.invalidateQueries({
          queryKey: projectKeys.resources(wsId, projectId),
        });
        qc.invalidateQueries({ queryKey: taskListKey });
      };

      return [
        // Project-level events
        ws.on("project:updated", (payload) => {
          if (payload.project.id !== projectId) return;
          patchProjectDetail(qc, wsId, payload.project);
        }),
        ws.on("project:deleted", (payload) => {
          if (payload.project_id !== projectId) return;
          clearProjectDetail(qc, wsId, projectId);
          removeFromProjectsList(qc, wsId, projectId);
          onDeleted?.();
        }),

        // Task events for tasks IN this project — patch the byProject
        // cache directly so the list stays fresh without a refetch.
        ws.on("task:updated", (payload) => {
          const task = payload.task;
          // Status / project_id changes both matter:
          //  - if it was in this project and still is: replace in place
          //  - if it just moved INTO this project: append (server is authority on order)
          //  - if it just moved OUT: remove from this list
          const wasInList = (
            qc.getQueryData<Task[]>(taskListKey) ?? []
          ).some((i) => i.id === task.id);
          const nowInProject = task.project_id === projectId;
          if (!wasInList && !nowInProject) return;
          qc.setQueryData<Task[]>(taskListKey, (old) => {
            if (!old) return old;
            if (nowInProject) {
              return old.some((i) => i.id === task.id)
                ? old.map((i) => (i.id === task.id ? task : i))
                : [...old, task];
            }
            return old.filter((i) => i.id !== task.id);
          });
        }),
        ws.on("task:created", (payload) => {
          if (payload.task.project_id !== projectId) return;
          // Server is the authority on list position — invalidate so we
          // refetch with the correct ordering rather than guessing.
          qc.invalidateQueries({ queryKey: taskListKey });
        }),
        ws.on("task:deleted", (payload) => {
          qc.setQueryData<Task[]>(taskListKey, (old) =>
            old ? old.filter((i) => i.id !== payload.task_id) : old,
          );
        }),

        ws.onReconnect(invalidateThisProject),
      ];
    },
    [projectId, qc, onDeleted],
  );
}
