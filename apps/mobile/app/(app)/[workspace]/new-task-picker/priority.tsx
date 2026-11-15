/**
 * Priority picker route for the in-progress new-task draft. See ./status.tsx.
 */
import { router } from "expo-router";
import { PriorityPickerBody } from "@/components/task/pickers/priority-picker-body";
import { useNewTaskDraftStore } from "@/data/stores/new-task-draft-store";

export default function NewTaskPriorityPickerRoute() {
  const priority = useNewTaskDraftStore((s) => s.priority);
  const setPriority = useNewTaskDraftStore((s) => s.setPriority);

  return (
    <PriorityPickerBody
      value={priority}
      onChange={(next) => {
        setPriority(next);
        router.back();
      }}
    />
  );
}
