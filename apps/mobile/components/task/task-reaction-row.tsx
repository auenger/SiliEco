/**
 * Task-level reaction row. Sits right under the description, mirroring
 * web's `task-detail.tsx:785` placement.
 *
 * Reads task.reactions from the detail cache passed by the parent. No
 * separate query — single source of truth on the detail object.
 *
 * Renders nothing when there are no reactions. Adding a reaction is
 * deferred to a long-press affordance on the task body (TODO).
 */
import { useCallback } from "react";
import { View } from "react-native";
import type { Task, TaskReaction } from "@silieco/core/types";
import { ReactionBar } from "./reaction-bar";
import { useToggleTaskReaction } from "@/data/mutations/tasks";
import { useAuthStore } from "@/data/auth-store";

export function TaskReactionRow({ task }: { task: Task }) {
  const userId = useAuthStore((s) => s.user?.id);
  const reactions: TaskReaction[] = task.reactions ?? [];
  const toggle = useToggleTaskReaction(task.id);

  const onToggle = useCallback(
    (emoji: string) => {
      const existing = reactions.find(
        (r) =>
          r.emoji === emoji &&
          r.actor_type === "member" &&
          r.actor_id === userId,
      );
      toggle.mutate({ emoji, existing });
    },
    [reactions, userId, toggle],
  );

  if (reactions.length === 0) return null;

  return (
    <View className="px-4 pb-3">
      <ReactionBar
        reactions={reactions}
        currentUserId={userId}
        onToggle={onToggle}
      />
    </View>
  );
}
