"use client";

import { createContext, useContext, type ReactNode } from "react";
import type { UpdateTaskRequest } from "@silieco/core/types";
import type { TaskCreateDefaults } from "./types";

export type TaskSurfaceMutationOptions = {
  errorMessage?: string;
  onSuccess?: () => void;
  onError?: (err: unknown) => void;
  onSettled?: () => void;
};

export interface TaskSurfaceActions {
  isPending: boolean;
  createTask: (defaults?: TaskCreateDefaults) => void;
  updateTask: (
    taskId: string,
    updates: Partial<UpdateTaskRequest>,
    options?: TaskSurfaceMutationOptions,
  ) => void;
  moveTask: (
    taskId: string,
    updates: Partial<UpdateTaskRequest>,
    options?: TaskSurfaceMutationOptions,
  ) => void;
  batchUpdate: (
    taskIds: string[],
    updates: Partial<UpdateTaskRequest>,
  ) => Promise<void>;
  batchDelete: (taskIds: string[]) => Promise<void>;
}

const TaskSurfaceActionsContext = createContext<TaskSurfaceActions | null>(
  null,
);

export function TaskSurfaceActionsProvider({
  actions,
  children,
}: {
  actions: TaskSurfaceActions;
  children: ReactNode;
}) {
  return (
    <TaskSurfaceActionsContext.Provider value={actions}>
      {children}
    </TaskSurfaceActionsContext.Provider>
  );
}

export function useTaskSurfaceActionsOptional() {
  return useContext(TaskSurfaceActionsContext);
}
