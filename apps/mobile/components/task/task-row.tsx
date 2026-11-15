/**
 * Shared task row used by every list-style task surface on mobile —
 * (tabs)/my-tasks, more/tasks (workspace-wide), and project detail's
 * related-tasks bucket.
 *
 * Layout mirrors web's `packages/views/tasks/components/list-row.tsx`:
 *   [status?]  priority  identifier  title  …  assignee
 *
 * `showStatus` is opt-in because the my-tasks SectionList already groups
 * by status (rendering it again per-row would be visual noise). The
 * project-related-tasks view doesn't section by status, so it asks for
 * the inline status icon. New callers should default to false unless they
 * mix multiple statuses inside a single ungrouped list.
 *
 * Behavioral parity:
 *   - Same `Task` type, same `assignee_type`/`assignee_id` semantics
 *     (root CLAUDE.md "Data identity must agree").
 *   - Mirrors web `packages/views/tasks/components/list-row.tsx:52`:
 *     render the assignee whenever `assignee_type && assignee_id` are both
 *     truthy — `ActorAvatar` itself handles member / agent / squad rendering
 *     (rounded square + people glyph or `squad.avatar_url` for squads). A
 *     future fourth enum value falls through to ActorAvatar's initials
 *     fallback, which is the real "enum drift downgrades, not crashes"
 *     behavior — earlier whitelist (member/agent only) silently dropped
 *     squad assignees instead.
 */
import { Pressable, View } from "react-native";
import type { Task } from "@silieco/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { StatusIcon } from "@/components/ui/status-icon";

interface Props {
  task: Task;
  onPress: () => void;
  /** Render the status icon inline at the start of the row. Default: false. */
  showStatus?: boolean;
}

export function TaskRow({ task, onPress, showStatus = false }: Props) {
  return (
    <Pressable onPress={onPress} className="active:bg-secondary px-4 py-3">
      <View className="flex-row items-center gap-3">
        {showStatus ? <StatusIcon status={task.status} size={14} /> : null}
        <PriorityIcon priority={task.priority} size={14} />
        <Text className="text-xs text-muted-foreground shrink-0 w-16">
          {task.identifier}
        </Text>
        <Text className="flex-1 text-sm text-foreground" numberOfLines={1}>
          {task.title}
        </Text>
        {task.assignee_type && task.assignee_id ? (
          <ActorAvatar
            type={task.assignee_type}
            id={task.assignee_id}
            size={20}
            showPresence
          />
        ) : null}
      </View>
    </Pressable>
  );
}
