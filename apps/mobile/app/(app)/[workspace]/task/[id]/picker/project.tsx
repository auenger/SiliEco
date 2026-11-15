/**
 * Project picker route for an existing task. Uses native iOS Stack header
 * + UISearchController via `useNativeSearchBar` (search bar registered in
 * ../_layout.tsx).
 */
import { useMemo } from "react";
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { ProjectPickerBody } from "@/components/task/pickers/project-picker-body";
import { taskDetailOptions } from "@/data/queries/tasks";
import { findProject, projectListOptions } from "@/data/queries/projects";
import { useUpdateTask } from "@/data/mutations/tasks";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

export default function TaskProjectPickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: task } = useQuery(taskDetailOptions(wsId, id));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const updateTask = useUpdateTask(id);
  const query = useNativeSearchBar("Search projects", { autoFocus: true });

  const project = useMemo(
    () => findProject(projects, task?.project_id ?? null),
    [projects, task?.project_id],
  );

  return (
    <ProjectPickerBody
      value={project ?? null}
      query={query}
      onChange={(next) => {
        updateTask.mutate({ project_id: next?.id ?? null });
        router.back();
      }}
    />
  );
}
