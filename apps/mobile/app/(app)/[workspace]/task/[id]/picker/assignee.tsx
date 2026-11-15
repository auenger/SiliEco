/**
 * Assignee picker route for an existing task. Uses the native iOS Stack
 * header + UISearchController (registered in ../_layout.tsx with
 * `headerShown: true` + title); the search bar wiring is encapsulated in
 * `useNativeSearchBar`.
 */
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { AssigneePickerBody } from "@/components/task/pickers/assignee-picker-body";
import { taskDetailOptions } from "@/data/queries/tasks";
import { useUpdateTask } from "@/data/mutations/tasks";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

export default function TaskAssigneePickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: task } = useQuery(taskDetailOptions(wsId, id));
  const updateTask = useUpdateTask(id);
  const query = useNativeSearchBar("Search people", { autoFocus: true });

  const value =
    task?.assignee_type && task?.assignee_id
      ? { type: task.assignee_type, id: task.assignee_id }
      : null;

  return (
    <AssigneePickerBody
      value={value}
      query={query}
      onChange={(next) => {
        if (next === null) {
          updateTask.mutate({ assignee_type: null, assignee_id: null });
        } else {
          updateTask.mutate({
            assignee_type: next.type,
            assignee_id: next.id,
          });
        }
        router.back();
      }}
    />
  );
}
