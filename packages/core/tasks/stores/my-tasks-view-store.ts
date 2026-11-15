"use client";

import { createStore, type StoreApi } from "zustand/vanilla";
import { persist } from "zustand/middleware";
import {
  type TaskViewState,
  viewStoreSlice,
  viewStorePersistOptions,
  mergeViewStatePersisted,
} from "./view-store";
import { registerForWorkspaceRehydration } from "../../platform/workspace-storage";

export type MyTasksScope = "all" | "assigned" | "created" | "agents";
export type MyTasksRelation = "all" | "assigned" | "created" | "involved";

// "agents" is the user-facing scope label; the task APIs call the same
// ownership/squad-membership relation "involved".
export function myTasksRelationFromScope(
  scope: MyTasksScope,
): MyTasksRelation {
  if (scope === "agents") return "involved";
  return scope;
}

export interface MyTasksViewState extends TaskViewState {
  scope: MyTasksScope;
  setScope: (scope: MyTasksScope) => void;
}

const basePersist = viewStorePersistOptions("silieco_tr1_my_tasks_view");

const _myTasksViewStore = createStore<MyTasksViewState>()(
  persist(
    (set) => ({
      ...viewStoreSlice(set as unknown as StoreApi<TaskViewState>["setState"]),
      scope: "assigned" as MyTasksScope,
      setScope: (scope: MyTasksScope) => set({ scope }),
    }),
    {
      name: basePersist.name,
      storage: basePersist.storage,
      partialize: (state: MyTasksViewState) => ({
        ...basePersist.partialize(state),
        scope: state.scope,
      }),
      // Reuse the same deep-merge as the base view store so newly added
      // cardProperties toggles inherit defaults for existing users. Without
      // this, the my-tasks page renders no labels because the persisted
      // snapshot predates the `labels` key and shallow-merge wins.
      merge: mergeViewStatePersisted<MyTasksViewState>,
    },
  ),
);

export const myTasksViewStore: StoreApi<MyTasksViewState> = _myTasksViewStore;

registerForWorkspaceRehydration(() => _myTasksViewStore.persist.rehydrate());
