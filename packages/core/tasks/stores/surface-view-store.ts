"use client";

import { createStore, type StoreApi } from "zustand/vanilla";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import {
  type TaskViewState,
  mergeViewStatePersisted,
  viewStorePersistOptions,
  viewStoreSlice,
} from "./view-store";

export const TASK_SURFACE_VIEW_STORAGE_KEY = "silieco_tr1_task_surface_views";

type PersistedTaskViewState = ReturnType<
  ReturnType<typeof viewStorePersistOptions>["partialize"]
>;

interface TaskSurfaceViewEntry {
  state: PersistedTaskViewState;
  updatedAt: string;
}

interface TaskSurfaceViewRegistryState {
  surfaces: Record<string, TaskSurfaceViewEntry>;
  setSurfaceState: (surfaceKey: string, state: TaskViewState) => void;
  clearSurfaceState: (surfaceKey: string) => void;
  pruneSurfaceStates: (validSurfaceKeys: Iterable<string>) => void;
}

const basePersist = viewStorePersistOptions(TASK_SURFACE_VIEW_STORAGE_KEY);
const surfaceStores = new Map<string, StoreApi<TaskViewState>>();
const suppressSurfacePersist = new Set<string>();

function persistedTaskViewState(state: TaskViewState): PersistedTaskViewState {
  return basePersist.partialize(state);
}

const taskSurfaceViewRegistryStore = createStore<TaskSurfaceViewRegistryState>()(
  persist(
    (set) => ({
      surfaces: {},
      setSurfaceState: (surfaceKey, state) =>
        set((current) => ({
          surfaces: {
            ...current.surfaces,
            [surfaceKey]: {
              state: persistedTaskViewState(state),
              updatedAt: new Date().toISOString(),
            },
          },
        })),
      clearSurfaceState: (surfaceKey) =>
        set((current) => {
          if (!current.surfaces[surfaceKey]) return current;
          const { [surfaceKey]: _removed, ...surfaces } = current.surfaces;
          return { surfaces };
        }),
      pruneSurfaceStates: (validSurfaceKeys) =>
        set((current) => {
          const valid = new Set(validSurfaceKeys);
          const surfaces = Object.fromEntries(
            Object.entries(current.surfaces).filter(([key]) => valid.has(key)),
          );
          if (Object.keys(surfaces).length === Object.keys(current.surfaces).length) {
            return current;
          }
          return { surfaces };
        }),
    }),
    {
      name: TASK_SURFACE_VIEW_STORAGE_KEY,
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      partialize: (state) => ({ surfaces: state.surfaces }),
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<TaskSurfaceViewRegistryState>;
        return {
          ...current,
          surfaces: p.surfaces ?? {},
        };
      },
    },
  ),
);

function resetStoreFromRegistry(surfaceKey: string, store: StoreApi<TaskViewState>) {
  const persisted =
    taskSurfaceViewRegistryStore.getState().surfaces[surfaceKey]?.state;
  const defaults = viewStoreSlice(store.setState);
  suppressSurfacePersist.add(surfaceKey);
  try {
    store.setState(mergeViewStatePersisted(persisted, defaults), true);
  } finally {
    suppressSurfacePersist.delete(surfaceKey);
  }
}

function resetAllSurfaceStoresFromRegistry() {
  for (const [surfaceKey, store] of surfaceStores) {
    resetStoreFromRegistry(surfaceKey, store);
  }
}

registerForWorkspaceRehydration(() => {
  void Promise.resolve(taskSurfaceViewRegistryStore.persist.rehydrate()).then(
    resetAllSurfaceStoresFromRegistry,
  );
});

export function getTaskSurfaceViewStore(
  surfaceKey: string,
): StoreApi<TaskViewState> {
  const existing = surfaceStores.get(surfaceKey);
  if (existing) return existing;

  const store = createStore<TaskViewState>()((set) => viewStoreSlice(set));
  resetStoreFromRegistry(surfaceKey, store);
  store.subscribe((state) => {
    if (suppressSurfacePersist.has(surfaceKey)) return;
    taskSurfaceViewRegistryStore.getState().setSurfaceState(surfaceKey, state);
  });
  surfaceStores.set(surfaceKey, store);
  return store;
}

export function clearTaskSurfaceViewState(surfaceKey: string) {
  taskSurfaceViewRegistryStore.getState().clearSurfaceState(surfaceKey);
  const store = surfaceStores.get(surfaceKey);
  if (store) resetStoreFromRegistry(surfaceKey, store);
}

export function pruneTaskSurfaceViewStates(validSurfaceKeys: Iterable<string>) {
  const valid = new Set(validSurfaceKeys);
  taskSurfaceViewRegistryStore.getState().pruneSurfaceStates(valid);
  for (const [surfaceKey, store] of surfaceStores) {
    if (!valid.has(surfaceKey)) resetStoreFromRegistry(surfaceKey, store);
  }
}

export function getTaskSurfaceViewStateRegistrySnapshot() {
  return taskSurfaceViewRegistryStore.getState().surfaces;
}
