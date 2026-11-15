"use client";

import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import type { TaskSubscriber } from "@silieco/core/types";
import type {
  SubscriberAddedPayload,
  SubscriberRemovedPayload,
} from "@silieco/core/types";
import { taskSubscribersOptions, taskKeys } from "@silieco/core/tasks/queries";
import { useToggleTaskSubscriber } from "@silieco/core/tasks/mutations";
import { useWSEvent, useWSReconnect } from "@silieco/core/realtime";

export function useTaskSubscribers(taskId: string, userId?: string) {
  const qc = useQueryClient();
  const { data: subscribers = [], isLoading: loading } = useQuery(
    taskSubscribersOptions(taskId),
  );

  const toggleMutation = useToggleTaskSubscriber(taskId);

  // Reconnect recovery
  useWSReconnect(
    useCallback(() => {
      qc.invalidateQueries({ queryKey: taskKeys.subscribers(taskId) });
    }, [qc, taskId]),
  );

  // --- WS event handlers ---

  useWSEvent(
    "subscriber:added",
    useCallback(
      (payload: unknown) => {
        const p = payload as SubscriberAddedPayload;
        if (p.task_id !== taskId) return;
        qc.setQueryData<TaskSubscriber[]>(
          taskKeys.subscribers(taskId),
          (old) => {
            if (!old) return old;
            if (
              old.some(
                (s) =>
                  s.user_id === p.user_id && s.user_type === p.user_type,
              )
            )
              return old;
            return [
              ...old,
              {
                task_id: p.task_id,
                user_type: p.user_type as "member" | "agent",
                user_id: p.user_id,
                reason: p.reason as TaskSubscriber["reason"],
                created_at: new Date().toISOString(),
              },
            ];
          },
        );
      },
      [qc, taskId],
    ),
  );

  useWSEvent(
    "subscriber:removed",
    useCallback(
      (payload: unknown) => {
        const p = payload as SubscriberRemovedPayload;
        if (p.task_id !== taskId) return;
        qc.setQueryData<TaskSubscriber[]>(
          taskKeys.subscribers(taskId),
          (old) =>
            old?.filter(
              (s) =>
                !(s.user_id === p.user_id && s.user_type === p.user_type),
            ),
        );
      },
      [qc, taskId],
    ),
  );

  // --- Mutations ---

  const isSubscribed = subscribers.some(
    (s) => s.user_type === "member" && s.user_id === userId,
  );

  const toggleSubscriber = useCallback(
    async (
      subUserId: string,
      userType: "member" | "agent",
      currentlySubscribed: boolean,
    ) => {
      toggleMutation.mutate({
        userId: subUserId,
        userType,
        subscribed: currentlySubscribed,
      });
    },
    [toggleMutation],
  );

  const toggleSubscribe = useCallback(() => {
    if (userId) toggleSubscriber(userId, "member", isSubscribed);
  }, [userId, isSubscribed, toggleSubscriber]);

  return {
    subscribers,
    loading,
    isSubscribed,
    toggleSubscribe,
    toggleSubscriber,
  };
}
