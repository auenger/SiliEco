import type { TaskScope } from "@silieco/core/tasks/surface/scope";
import type { CreateTaskRequest } from "@silieco/core/types";
import type { ViewMode } from "@silieco/core/tasks/stores/view-store";

export type TaskCreateDefaults = Partial<
  Omit<
    CreateTaskRequest,
    "assignee_type" | "assignee_id" | "parent_task_id" | "project_id"
  >
> & {
  assignee_type?: CreateTaskRequest["assignee_type"] | null;
  assignee_id?: string | null;
  parent_task_id?: string | null;
  /** Display-only context for the create dialog while the parent query loads. */
  parent_task_identifier?: string;
  project_id?: string | null;
};

export type TaskSurfaceMode = Extract<
  ViewMode,
  "board" | "list" | "table" | "swimlane" | "gantt"
>;

export interface TaskSurfaceProps {
  scope: TaskScope;
  modes: TaskSurfaceMode[];
  surfaceKey?: string;
  createDefaults?: TaskCreateDefaults;
  /** Server-owned membership search shared by non-Table task surfaces. */
  search?: string;
}
