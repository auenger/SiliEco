/**
 * Status picker route for the in-progress new-task draft. Reads/writes
 * `useNewTaskDraftStore` — the new-task.tsx modal owns the draft and
 * reads from the same store. See ../new-task.tsx for the lifecycle.
 */
import { router } from "expo-router";
import { StatusPickerBody } from "@/components/task/pickers/status-picker-body";
import { useNewTaskDraftStore } from "@/data/stores/new-task-draft-store";

export default function NewTaskStatusPickerRoute() {
  const status = useNewTaskDraftStore((s) => s.status);
  const setStatus = useNewTaskDraftStore((s) => s.setStatus);

  return (
    <StatusPickerBody
      value={status}
      onChange={(next) => {
        setStatus(next);
        router.back();
      }}
    />
  );
}
