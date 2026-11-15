/**
 * Assignee picker route for the in-progress new-task draft. See ./status.tsx.
 * Uses the same iOS-native nav header + UISearchController pattern as
 * `task/[id]/picker/assignee.tsx`, with the search bar wiring encapsulated
 * in `useNativeSearchBar`.
 */
import { router } from "expo-router";
import { AssigneePickerBody } from "@/components/task/pickers/assignee-picker-body";
import { useNewTaskDraftStore } from "@/data/stores/new-task-draft-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

export default function NewTaskAssigneePickerRoute() {
  const assignee = useNewTaskDraftStore((s) => s.assignee);
  const setAssignee = useNewTaskDraftStore((s) => s.setAssignee);
  const query = useNativeSearchBar("Search people", { autoFocus: true });

  return (
    <AssigneePickerBody
      value={assignee}
      query={query}
      onChange={(next) => {
        setAssignee(next);
        router.back();
      }}
    />
  );
}
