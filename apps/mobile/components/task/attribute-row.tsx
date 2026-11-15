/**
 * Task-detail attribute chip row. Linear iOS-inspired layout: each
 * editable attribute renders as a tappable chip; tapping pushes a
 * formSheet picker route. The route reads the task from the TanStack
 * Query detail cache and fires its own mutation — no onChange callback
 * round-trip back to AttributeRow.
 *
 * Picker route map (every entry is registered in `_layout.tsx` with
 * shared SHEET_OPTIONS — formSheet + iOS native grabber + explicit
 * numeric detents):
 *   status    →  task/[id]/picker/status
 *   priority  →  task/[id]/picker/priority
 *   assignee  →  task/[id]/picker/assignee
 *   labels    →  task/[id]/picker/label   (multi-select, stays open)
 *   project   →  task/[id]/picker/project
 *   due_date  →  task/[id]/picker/due-date
 */
import { useMemo } from "react";
import { View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import type {
  Task,
  TaskPriority,
} from "@silieco/core/types";
import { formatDateOnly } from "@silieco/core/tasks/date";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { ProjectIcon } from "@/components/ui/project-icon";
import { AttributeChip } from "./attribute-chip";
import { useActorLookup } from "@/data/use-actor-name";
import { findProject, projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  STATUS_LABEL,
  PRIORITY_LABEL as PRIORITY_FULL_LABEL,
} from "@/lib/task-status";

// Chip placeholder shortens `none` from "No priority" → "Priority" so the
// unset chip reads as a placeholder, not as a confusing assigned value.
const PRIORITY_CHIP_LABEL: Record<TaskPriority, string> = {
  ...PRIORITY_FULL_LABEL,
  none: "Priority",
};

/**
 * The picker fields the task-detail attribute row can open. Bound to a
 * map of typed Expo Router pathnames so typos become compile errors
 * (previously the call site used `as never` on a template string, which
 * silently accepted anything).
 */
type TaskPickerField =
  | "status"
  | "priority"
  | "assignee"
  | "label"
  | "project"
  | "due-date";

const TASK_PICKER_PATHNAMES = {
  status: "/[workspace]/task/[id]/picker/status",
  priority: "/[workspace]/task/[id]/picker/priority",
  assignee: "/[workspace]/task/[id]/picker/assignee",
  label: "/[workspace]/task/[id]/picker/label",
  project: "/[workspace]/task/[id]/picker/project",
  "due-date": "/[workspace]/task/[id]/picker/due-date",
} as const satisfies Record<TaskPickerField, string>;

// due_date is a calendar day — format timezone-safely so the day never shifts
// with the viewer's offset. Mirrors web's formatDate in list-row/board-card.
function formatDueDate(iso: string | null): string | null {
  if (!iso) return null;
  return formatDateOnly(iso, { month: "short", day: "numeric" }, "en-US") || null;
}

export function AttributeRow({ task }: { task: Task }) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { getName } = useActorLookup();

  // Project read-only — fetch list to look up the title + icon. Cheap
  // (cached after first task-detail visit).
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const project = useMemo(
    () => findProject(projects, task.project_id),
    [projects, task.project_id],
  );

  const labels = task.labels ?? [];

  const assigneeValue =
    task.assignee_type && task.assignee_id
      ? { type: task.assignee_type, id: task.assignee_id }
      : null;

  const assigneeName = assigneeValue
    ? getName(assigneeValue.type, assigneeValue.id)
    : null;
  const dueLabel = formatDueDate(task.due_date);

  const openPicker = (field: TaskPickerField) => {
    if (!wsSlug) return;
    router.push({
      pathname: TASK_PICKER_PATHNAMES[field],
      params: { workspace: wsSlug, id: task.id },
    });
  };

  return (
    <View className="flex-row flex-wrap gap-2">
      {/* Status — always shown */}
      <AttributeChip
        icon={<StatusIcon status={task.status} size={14} />}
        label={STATUS_LABEL[task.status]}
        variant="filled"
        onPress={() => openPicker("status")}
      />

      {/* Priority */}
      <AttributeChip
        icon={<PriorityIcon priority={task.priority} size={14} />}
        label={PRIORITY_CHIP_LABEL[task.priority]}
        variant={task.priority === "none" ? "dimmed" : "filled"}
        onPress={() => openPicker("priority")}
      />

      {/* Assignee */}
      {assigneeValue ? (
        <AttributeChip
          icon={
            <ActorAvatar
              type={assigneeValue.type}
              id={assigneeValue.id}
              size={16}
              showPresence
            />
          }
          label={assigneeName ?? "Unknown"}
          variant="filled"
          onPress={() => openPicker("assignee")}
        />
      ) : (
        <AttributeChip
          icon={
            <View className="size-4 rounded-full border border-dashed border-muted-foreground/40" />
          }
          label="Assignee"
          variant="dimmed"
          onPress={() => openPicker("assignee")}
        />
      )}

      {/* Each existing label renders as its own chip. Tap opens the
          label picker (multi-select toggle). No quick-detach gesture
          on the chip itself in v1 — Linear iOS uses long-press for
          that, deferred until requested. */}
      {labels.map((label) => (
        <AttributeChip
          key={label.id}
          icon={
            <View
              className="size-2.5 rounded-full"
              style={{ backgroundColor: label.color }}
            />
          }
          label={label.name}
          variant="filled"
          onPress={() => openPicker("label")}
        />
      ))}
      {labels.length === 0 ? (
        <AttributeChip
          icon={<Text className="text-xs text-muted-foreground/70">◯</Text>}
          label="Label"
          variant="dimmed"
          onPress={() => openPicker("label")}
        />
      ) : null}

      {/* Project */}
      {project ? (
        <AttributeChip
          icon={<ProjectIcon icon={project.icon} size="sm" />}
          label={project.title}
          variant="filled"
          onPress={() => openPicker("project")}
        />
      ) : (
        <AttributeChip
          icon={
            <View className="size-3.5 rounded-sm border border-dashed border-muted-foreground/40" />
          }
          label="Project"
          variant="dimmed"
          onPress={() => openPicker("project")}
        />
      )}

      {/* Due date */}
      <AttributeChip
        icon={<Text className="text-xs text-muted-foreground/80">📅</Text>}
        label={dueLabel ?? "Due date"}
        variant={dueLabel ? "filled" : "dimmed"}
        onPress={() => openPicker("due-date")}
      />
    </View>
  );
}
