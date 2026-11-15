"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

export type TasksScope = "all" | "members" | "agents";

interface TasksScopeState {
  scope: TasksScope;
  setScope: (scope: TasksScope) => void;
}

export const useTasksScopeStore = create<TasksScopeState>()(
  persist(
    (set) => ({
      scope: "all",
      setScope: (scope) => set({ scope }),
    }),
    {
      name: "silieco_tr1_tasks_scope",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useTasksScopeStore.persist.rehydrate());
