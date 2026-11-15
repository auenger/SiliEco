/**
 * Status picker route for an existing task — presented as a formSheet
 * (UISheetPresentationController) by the parent Stack.
 *
 * Self-contained: reads the task from the TanStack Query detail cache,
 * calls `useUpdateTask` directly on selection, then `router.back()`s. No
 * onChange callback to a parent.
 *
 * If the cache is cold (rare — the user reaches this screen by tapping
 * a chip on the task-detail page that already populated it), the picker
 * still renders against the current value of `todo` and the optimistic
 * mutation patches the cache when the user picks.
 */
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { StatusPickerBody } from "@/components/task/pickers/status-picker-body";
import { taskDetailOptions } from "@/data/queries/tasks";
import { useUpdateTask } from "@/data/mutations/tasks";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function TaskStatusPickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: task } = useQuery(taskDetailOptions(wsId, id));
  const updateTask = useUpdateTask(id);

  return (
    <StatusPickerBody
      value={task?.status ?? "todo"}
      onChange={(next) => {
        updateTask.mutate({ status: next });
        router.back();
      }}
    />
  );
}
