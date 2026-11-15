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

export type ActorTasksScope = "assigned" | "created";

export interface ActorTasksViewState extends TaskViewState {
  scope: ActorTasksScope;
  setScope: (scope: ActorTasksScope) => void;
}

const basePersist = viewStorePersistOptions("silieco_tr1_actor_tasks_view");

const _actorTasksViewStore = createStore<ActorTasksViewState>()(
  persist(
    (set) => ({
      ...viewStoreSlice(set as unknown as StoreApi<TaskViewState>["setState"]),
      // Actor tasks panel is list-only; override the slice's "board" default.
      viewMode: "list",
      scope: "assigned" as ActorTasksScope,
      setScope: (scope: ActorTasksScope) => set({ scope }),
    }),
    {
      name: basePersist.name,
      storage: basePersist.storage,
      partialize: (state: ActorTasksViewState) => ({
        ...basePersist.partialize(state),
        scope: state.scope,
      }),
      merge: mergeViewStatePersisted<ActorTasksViewState>,
    },
  ),
);

export const actorTasksViewStore: StoreApi<ActorTasksViewState> =
  _actorTasksViewStore;

registerForWorkspaceRehydration(() =>
  _actorTasksViewStore.persist.rehydrate(),
);
