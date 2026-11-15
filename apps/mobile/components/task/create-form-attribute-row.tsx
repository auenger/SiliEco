/**
 * Bottom chip row for the new-task form. Mirrors `attribute-row.tsx`'s
 * visual pattern but operates on the `useNewTaskDraftStore` instead of an
 * `task` object + mutation. Tapping a chip pushes a formSheet picker
 * route under `new-task-picker/<field>` — the route reads/writes the same
 * draft store, so the chip rehydrates automatically when the sheet
 * dismisses.
 *
 * Why a draft store: the picker routes are siblings of new-task.tsx in
 * the Stack — they can't reach into the new-task screen's local state.
 * The draft store is the cross-screen channel.
 */
import { View } from "react-native";
import { router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { AttributeChip } from "@/components/task/attribute-chip";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ProjectIcon } from "@/components/ui/project-icon";
import { StatusIcon } from "@/components/ui/status-icon";
import { formatDateOnly } from "@silieco/core/tasks/date";
import { useActorLookup } from "@/data/use-actor-name";
import { useNewTaskDraftStore } from "@/data/stores/new-task-draft-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { PRIORITY_LABEL, STATUS_LABEL } from "@/lib/task-status";

/**
 * Picker fields the new-task draft form can open. Bound to a typed map
 * of Expo Router pathnames so typos become compile errors (previously
 * the call site used `as never` on a template string).
 */
type NewTaskPickerField =
  | "status"
  | "priority"
  | "assignee"
  | "project"
  | "due-date";

const NEW_TASK_PICKER_PATHNAMES = {
  status: "/[workspace]/new-task-picker/status",
  priority: "/[workspace]/new-task-picker/priority",
  assignee: "/[workspace]/new-task-picker/assignee",
  project: "/[workspace]/new-task-picker/project",
  "due-date": "/[workspace]/new-task-picker/due-date",
} as const satisfies Record<NewTaskPickerField, string>;

export function CreateFormAttributeRow() {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const status = useNewTaskDraftStore((s) => s.status);
  const priority = useNewTaskDraftStore((s) => s.priority);
  const assignee = useNewTaskDraftStore((s) => s.assignee);
  const dueDate = useNewTaskDraftStore((s) => s.dueDate);
  const project = useNewTaskDraftStore((s) => s.project);

  const { getName } = useActorLookup();
  const assigneeLabel = assignee
    ? getName(assignee.type, assignee.id)
    : "Assignee";
  const priorityLabel =
    priority === "none" ? "Priority" : PRIORITY_LABEL[priority];

  const open = (field: NewTaskPickerField) => {
    if (!wsSlug) return;
    router.push({
      pathname: NEW_TASK_PICKER_PATHNAMES[field],
      params: { workspace: wsSlug },
    });
  };

  return (
    <View>
      <View className="flex-row flex-wrap gap-2">
        <AttributeChip
          icon={<StatusIcon status={status} size={12} />}
          label={STATUS_LABEL[status]}
          variant="filled"
          onPress={() => open("status")}
        />
        <AttributeChip
          icon={<PriorityIcon priority={priority} />}
          label={priorityLabel}
          variant={priority === "none" ? "dimmed" : "filled"}
          onPress={() => open("priority")}
        />
        <AttributeChip
          icon={
            assignee ? (
              <ActorAvatar
                type={assignee.type}
                id={assignee.id}
                size={16}
                showPresence
              />
            ) : (
              <Ionicons
                name="person-circle-outline"
                size={16}
                color="#a1a1aa"
              />
            )
          }
          label={assigneeLabel}
          variant={assignee ? "filled" : "dimmed"}
          onPress={() => open("assignee")}
        />
        <AttributeChip
          icon={
            <Ionicons
              name="calendar-outline"
              size={14}
              color={dueDate ? undefined : "#a1a1aa"}
            />
          }
          label={dueDate ? formatDueDate(dueDate) : "Due date"}
          variant={dueDate ? "filled" : "dimmed"}
          onPress={() => open("due-date")}
        />
        <AttributeChip
          icon={
            project ? (
              <ProjectIcon icon={project.icon} size="sm" />
            ) : (
              <Ionicons name="folder-outline" size={14} color="#a1a1aa" />
            )
          }
          label={project?.title ?? "Project"}
          variant={project ? "filled" : "dimmed"}
          onPress={() => open("project")}
        />
      </View>
    </View>
  );
}

// due_date is a calendar day — format timezone-safely (no offset day shift).
function formatDueDate(iso: string): string {
  return formatDateOnly(iso, { month: "short", day: "numeric" }) || "Due date";
}
