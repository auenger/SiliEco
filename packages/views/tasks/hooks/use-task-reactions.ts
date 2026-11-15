"use client";

import { useCallback, useMemo } from "react";
import { useQuery, useQueryClient, useMutationState } from "@tanstack/react-query";
import type { TaskReaction } from "@silieco/core/types";
import type {
  TaskReactionAddedPayload,
  TaskReactionRemovedPayload,
} from "@silieco/core/types";
import { taskReactionsOptions, taskKeys } from "@silieco/core/tasks/queries";
import { useToggleTaskReaction, type ToggleTaskReactionVars } from "@silieco/core/tasks/mutations";
import { useWSEvent, useWSReconnect } from "@silieco/core/realtime";

export function useTaskReactions(taskId: string, userId?: string) {
  const qc = useQueryClient();
  const { data: serverReactions = [], isLoading: loading } = useQuery(
    taskReactionsOptions(taskId),
  );

  const toggleMutation = useToggleTaskReaction(taskId);

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      qc.invalidateQueries({ queryKey: taskKeys.reactions(taskId) });
    }, [qc, taskId]),
  );

  // --- WS event handlers (update server cache for other users' actions) ---

  useWSEvent(
    "task_reaction:added",
    useCallback(
      (payload: unknown) => {
        const { reaction, task_id } = payload as TaskReactionAddedPayload;
        if (task_id !== taskId) return;
        qc.setQueryData<TaskReaction[]>(
          taskKeys.reactions(taskId),
          (old) => {
            if (!old) return old;
            if (old.some((r) => r.id === reaction.id)) return old;
            return [...old, reaction];
          },
        );
      },
      [qc, taskId],
    ),
  );

  useWSEvent(
    "task_reaction:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as TaskReactionRemovedPayload;
        if (p.task_id !== taskId) return;
        qc.setQueryData<TaskReaction[]>(
          taskKeys.reactions(taskId),
          (old) =>
            old?.filter(
              (r) =>
                !(
                  r.emoji === p.emoji &&
                  r.actor_type === p.actor_type &&
                  r.actor_id === p.actor_id
                ),
            ),
        );
      },
      [qc, taskId],
    ),
  );

  // --- Optimistic UI derivation ---
  // Instead of writing temp data into the cache (which races with WS events),
  // derive optimistic state at render time from pending mutation variables.

  const pendingVars = useMutationState({
    filters: {
      mutationKey: ["toggleTaskReaction", taskId],
      status: "pending",
    },
    select: (m) =>
      m.state.variables as ToggleTaskReactionVars | undefined,
  });

  const reactions = useMemo(() => {
    if (pendingVars.length === 0) return serverReactions;

    let result = [...serverReactions];
    for (const vars of pendingVars) {
      if (!vars) continue;
      if (vars.existing) {
        // Pending removal
        result = result.filter((r) => r.id !== vars.existing!.id);
      } else {
        // Pending add — skip if server already has it (WS arrived first)
        const alreadyExists = result.some(
          (r) =>
            r.emoji === vars.emoji &&
            r.actor_type === "member" &&
            r.actor_id === userId,
        );
        if (!alreadyExists) {
          result = [
            ...result,
            {
              id: `optimistic-${vars.emoji}`,
              task_id: taskId,
              actor_type: "member",
              actor_id: userId ?? "",
              emoji: vars.emoji,
              created_at: "",
            },
          ];
        }
      }
    }
    return result;
  }, [serverReactions, pendingVars, taskId, userId]);

  // --- Mutation ---

  const toggleReaction = useCallback(
    async (emoji: string) => {
      if (!userId) return;
      const existing = serverReactions.find(
        (r) =>
          r.emoji === emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggleMutation.mutate({ emoji, existing });
    },
    [userId, serverReactions, toggleMutation],
  );

  return { reactions, loading, toggleReaction };
}
