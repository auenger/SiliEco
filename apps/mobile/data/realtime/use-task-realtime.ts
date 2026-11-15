/**
 * Per-task realtime subscriptions. Mounted by the task detail screen
 * with the active task id; cleans up on navigate-away.
 *
 * Handles:
 *   - task:updated / task:deleted / task_labels:changed → detail cache
 *   - comment:created / comment:updated / comment:deleted → timeline
 *   - activity:created → timeline
 *   - reaction:added / reaction:removed → comment reactions on timeline
 *   - task_reaction:added / task_reaction:removed → task-level reactions on detail
 *   - run:queued / run:dispatch / run:progress / run:completed /
 *     run:failed / run:cancelled → invalidate timeline + detail (task
 *     completion can flip a task's status server-side without firing
 *     task:updated, so we refetch the authoritative detail too)
 *   - reconnect → invalidate detail + timeline (we might've missed events
 *     while disconnected; server has no replay buffer for this client)
 *
 * Mobile pattern (per the realtime plan, see
 * /Users/qingnaiyuan/.claude/plans/plan-api-indexed-waffle.md):
 *   - Patch over invalidate where the payload contains the full object
 *   - Event always wins on optimistic-update conflicts; brief flicker
 *     is acceptable, correctness wins.
 *   - All handlers self-gate on `task_id === taskId` so we only react
 *     to events for the currently-viewed task.
 */
import { useQueryClient } from "@tanstack/react-query";
import type {
  RunCancelledPayload,
  RunCompletedPayload,
  RunDispatchPayload,
  RunFailedPayload,
  RunProgressPayload,
  AgentRunMessagePayload,
  RunQueuedPayload,
} from "@silieco/core/types";
import { agentRunsKeys } from "@/data/queries/agent-runs";
import { taskKeys } from "@/data/queries/task-keys";
import { useWSSubscriptions } from "@/lib/use-ws-subscriptions";
import {
  addCommentReaction,
  addTaskReaction,
  appendTimelineEntry,
  clearTaskDetail,
  commentToTimelineEntry,
  patchTaskDetail,
  patchTaskLabels,
  patchMyTasksList,
  patchTimelineEntry,
  removeCommentCascade,
  removeCommentReaction,
  removeFromMyTasksList,
  removeTaskReaction,
} from "./task-ws-updaters";

type RunEventPayload =
  | RunQueuedPayload
  | RunDispatchPayload
  | RunCompletedPayload
  | RunFailedPayload
  | RunCancelledPayload
  | RunProgressPayload
  | AgentRunMessagePayload;

export function useTaskRealtime(
  taskId: string | undefined,
  onDeleted?: () => void,
) {
  const qc = useQueryClient();

  useWSSubscriptions(
    (ws, wsId) => {
      if (!taskId) return;

      const invalidateThisTask = () => {
        qc.invalidateQueries({ queryKey: taskKeys.detail(wsId, taskId) });
        qc.invalidateQueries({ queryKey: taskKeys.timeline(wsId, taskId) });
      };

      // Run-query invalidation — separate from detail/timeline so the
      // AgentActivityRow + RunsSheet can refresh without forcing a full
      // timeline rebuild. WS run payloads only carry { run_id, agent_id,
      // task_id, status } — not the full AgentRun object — so per
      // apps/mobile/CLAUDE.md "Patch over invalidate" rule #1 (payload is
      // just an id), invalidate is the correct primitive.
      const invalidateRunQueries = () => {
        qc.invalidateQueries({ queryKey: agentRunsKeys.activeForTask(wsId, taskId) });
        qc.invalidateQueries({ queryKey: agentRunsKeys.historyForTask(wsId, taskId) });
      };

      // Business identity scopes this view; chat-only runs have no task_id.
      const onRunEvent = (p: RunEventPayload) => {
        if (p.task_id !== taskId) return;
        invalidateThisTask();
        invalidateRunQueries();
      };

      return [
        // ----- Task-level -----
        ws.on("task:updated", (payload) => {
          if (payload.task.id !== taskId) return;
          patchTaskDetail(qc, wsId, payload.task);
          patchMyTasksList(qc, wsId, payload.task);
        }),
        ws.on("task:deleted", (payload) => {
          if (payload.task_id !== taskId) return;
          clearTaskDetail(qc, wsId, taskId);
          removeFromMyTasksList(qc, wsId, taskId);
          onDeleted?.();
        }),
        ws.on("task_labels:changed", (payload) => {
          if (payload.task_id !== taskId) return;
          patchTaskLabels(qc, wsId, taskId, payload.labels);
        }),

        // ----- Comments / activity -----
        ws.on("comment:created", (payload) => {
          if (payload.comment.task_id !== taskId) return;
          appendTimelineEntry(
            qc,
            wsId,
            taskId,
            commentToTimelineEntry(payload.comment),
          );
        }),
        ws.on("comment:updated", (payload) => {
          if (payload.comment.task_id !== taskId) return;
          const entry = commentToTimelineEntry(payload.comment);
          patchTimelineEntry(
            qc,
            wsId,
            taskId,
            (e) => e.type === "comment" && e.id === payload.comment.id,
            () => entry,
          );
        }),
        // Resolve / unresolve broadcast from any client. Payload carries the
        // full Comment with the new resolved_at/resolved_by_* fields, so we
        // can in-place-replace the entry via commentToTimelineEntry — no
        // refetch. Without these handlers the resolved state only updated
        // via the local mutation or via reconnect invalidate (the second
        // costs a full timeline refetch and busts every CommentCard memo).
        ws.on("comment:resolved", (payload) => {
          if (payload.comment.task_id !== taskId) return;
          const entry = commentToTimelineEntry(payload.comment);
          patchTimelineEntry(
            qc,
            wsId,
            taskId,
            (e) => e.type === "comment" && e.id === payload.comment.id,
            () => entry,
          );
        }),
        ws.on("comment:unresolved", (payload) => {
          if (payload.comment.task_id !== taskId) return;
          const entry = commentToTimelineEntry(payload.comment);
          patchTimelineEntry(
            qc,
            wsId,
            taskId,
            (e) => e.type === "comment" && e.id === payload.comment.id,
            () => entry,
          );
        }),
        ws.on("comment:deleted", (payload) => {
          if (payload.task_id !== taskId) return;
          // Cascade: descendant replies must come out alongside the parent,
          // otherwise buildTimelineRows promotes them to top-level rows and
          // the user sees ghost replies after another client deletes the
          // thread. Server already cascades; this mirrors it in the cache.
          removeCommentCascade(qc, wsId, taskId, payload.comment_id);
        }),
        ws.on("activity:created", (payload) => {
          if (payload.task_id !== taskId) return;
          appendTimelineEntry(qc, wsId, taskId, payload.entry);
        }),

        // ----- Comment reactions -----
        ws.on("reaction:added", (payload) => {
          if (payload.task_id !== taskId) return;
          addCommentReaction(
            qc,
            wsId,
            taskId,
            payload.reaction.comment_id,
            payload.reaction,
          );
        }),
        ws.on("reaction:removed", (payload) => {
          if (payload.task_id !== taskId) return;
          removeCommentReaction(
            qc,
            wsId,
            taskId,
            payload.comment_id,
            payload.emoji,
            payload.actor_id,
          );
        }),

        // ----- Task-level reactions -----
        ws.on("task_reaction:added", (payload) => {
          if (payload.task_id !== taskId) return;
          addTaskReaction(qc, wsId, taskId, payload.reaction);
        }),
        ws.on("task_reaction:removed", (payload) => {
          if (payload.task_id !== taskId) return;
          removeTaskReaction(
            qc,
            wsId,
            taskId,
            payload.emoji,
            payload.actor_id,
          );
        }),

        // ----- Agent task progress -----
        ws.on("run:queued", onRunEvent),
        ws.on("run:dispatch", onRunEvent),
        ws.on("run:progress", onRunEvent),
        ws.on("run:completed", onRunEvent),
        ws.on("run:failed", onRunEvent),
        ws.on("run:cancelled", onRunEvent),

        // ----- Reconnect -----
        ws.onReconnect(() => {
          invalidateThisTask();
          invalidateRunQueries();
        }),
      ];
    },
    [taskId, qc, onDeleted],
  );
}
