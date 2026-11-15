/**
 * Project picker route for the in-progress new-task draft. Uses the same
 * native iOS Stack header + UISearchController pattern as
 * `task/[id]/picker/project.tsx`.
 */
import { router } from "expo-router";
import { ProjectPickerBody } from "@/components/task/pickers/project-picker-body";
import { useNewTaskDraftStore } from "@/data/stores/new-task-draft-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

export default function NewTaskProjectPickerRoute() {
  const project = useNewTaskDraftStore((s) => s.project);
  const setProject = useNewTaskDraftStore((s) => s.setProject);
  const query = useNativeSearchBar("Search projects", { autoFocus: true });

  return (
    <ProjectPickerBody
      value={project}
      query={query}
      onChange={(next) => {
        setProject(next);
        router.back();
      }}
    />
  );
}
