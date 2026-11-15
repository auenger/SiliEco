"use client";

import { useCallback, useMemo } from "react";
import { toast } from "sonner";
import type { UpdateTaskRequest } from "@silieco/core/types";
import {
  useBatchDeleteTasks,
  useBatchUpdateTasks,
  useUpdateTask,
} from "@silieco/core/tasks/mutations";
import { useModalStore } from "@silieco/core/modals";
import { isRevisionConflictError } from "@silieco/core/revisions";
import {
  type TaskSurfaceActions,
  type TaskSurfaceMutationOptions,
} from "./actions-context";
import type { TaskCreateDefaults } from "./types";
import { useT } from "../../i18n";

export type MoveTaskUpdates = Pick<
  UpdateTaskRequest,
  | "status"
  | "assignee_type"
  | "assignee_id"
  | "position"
  | "parent_task_id"
  | "project_id"
> & {
  before_id: string | null;
  after_id: string | null;
};

export interface TaskSurfaceActionController {
  actions: TaskSurfaceActions;
  openCreateTask: (defaults?: TaskCreateDefaults) => void;
  moveTask: (
    taskId: string,
    updates: MoveTaskUpdates,
    onSettled?: () => void,
  ) => void;
}

export function useTaskSurfaceActions({
  createDefaults,
}: {
  createDefaults: TaskCreateDefaults;
}): TaskSurfaceActionController {
  const { t } = useT("projects");
  const updateTaskMutation = useUpdateTask();
  const batchUpdateMutation = useBatchUpdateTasks();
  const batchDeleteMutation = useBatchDeleteTasks();

  const updateTask = useCallback(
    (
      taskId: string,
      updates: Partial<UpdateTaskRequest>,
      options?: TaskSurfaceMutationOptions,
    ) => {
      updateTaskMutation.mutate(
        { id: taskId, ...updates },
        {
          onSuccess: () => options?.onSuccess?.(),
          onError: (err) => {
            if (!isRevisionConflictError(err)) {
              toast.error(
                options?.errorMessage ??
                  t(($) => $.detail.toast_move_task_failed),
              );
            }
            options?.onError?.(err);
          },
          onSettled: () => options?.onSettled?.(),
        },
      );
    },
    [t, updateTaskMutation],
  );

  const moveTask = useCallback(
    (
      taskId: string,
      updates: MoveTaskUpdates,
      onSettled?: () => void,
    ) => {
      const { before_id, after_id, ...optimisticUpdates } = updates;
      updateTaskMutation.mutate(
        {
          id: taskId,
          ...optimisticUpdates,
          move_intent: { before_id, after_id },
        },
        {
          onError: () => {
            toast.error(t(($) => $.detail.toast_move_task_failed));
          },
          onSettled,
        },
      );
    },
    [t, updateTaskMutation],
  );

  const openCreateTask = useCallback(
    (defaults?: TaskCreateDefaults) => {
      useModalStore
        .getState()
        .open("create-task", { ...createDefaults, ...defaults });
    },
    [createDefaults],
  );

  const actions = useMemo<TaskSurfaceActions>(
    () => ({
      isPending:
        updateTaskMutation.isPending ||
        batchUpdateMutation.isPending ||
        batchDeleteMutation.isPending,
      createTask: openCreateTask,
      updateTask,
      moveTask: (taskId, updates, options) =>
        updateTask(taskId, updates, {
          errorMessage: t(($) => $.detail.toast_move_task_failed),
          ...options,
        }),
      batchUpdate: async (taskIds, updates) => {
        await batchUpdateMutation.mutateAsync({ ids: taskIds, updates });
      },
      batchDelete: async (taskIds) => {
        await batchDeleteMutation.mutateAsync(taskIds);
      },
    }),
    [
      batchDeleteMutation,
      batchUpdateMutation,
      openCreateTask,
      t,
      updateTask,
      updateTaskMutation.isPending,
    ],
  );

  return { actions, openCreateTask, moveTask };
}
