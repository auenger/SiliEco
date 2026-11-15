/**
 * Priority picker route for an existing task. See ./status.tsx for the
 * self-contained-route rationale.
 */
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { PriorityPickerBody } from "@/components/task/pickers/priority-picker-body";
import { taskDetailOptions } from "@/data/queries/tasks";
import { useUpdateTask } from "@/data/mutations/tasks";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function TaskPriorityPickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: task } = useQuery(taskDetailOptions(wsId, id));
  const updateTask = useUpdateTask(id);

  return (
    <PriorityPickerBody
      value={task?.priority ?? "none"}
      onChange={(next) => {
        updateTask.mutate({ priority: next });
        router.back();
      }}
    />
  );
}
