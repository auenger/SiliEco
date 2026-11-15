import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * Which properties the task-detail sub-tasks rows display.
 *
 * This is a personal reading preference (like the comment composer's sticky
 * toggle), not a per-view setting: every task's sub-tasks panel shares the
 * one configuration, persisted globally via `defaultStorage`. Defaults match
 * the panel's built-in row layout so the preference is invisible until the
 * user changes it.
 *
 * `rowPropertyIds` holds workspace custom property definition ids. The list
 * is global while property ids are workspace-scoped — renderers resolve ids
 * against the current workspace's property list, so ids from another
 * workspace are simply inert there (same tolerance as the view store's
 * `cardPropertyIds`).
 */
export const SUB_TASK_ROW_PROPERTY_KEYS = [
  "priority",
  "labels",
  "childProgress",
  "dueDate",
  "assignee",
] as const;
export type SubTaskRowPropertyKey =
  (typeof SUB_TASK_ROW_PROPERTY_KEYS)[number];
export type SubTaskRowProperties = Record<SubTaskRowPropertyKey, boolean>;

export const DEFAULT_SUB_TASK_ROW_PROPERTIES: SubTaskRowProperties = {
  priority: true,
  labels: true,
  childProgress: true,
  dueDate: true,
  assignee: true,
};

interface SubTaskDisplayStore {
  rowProperties: SubTaskRowProperties;
  rowPropertyIds: string[];
  toggleRowProperty: (key: SubTaskRowPropertyKey) => void;
  toggleRowPropertyId: (propertyId: string) => void;
}

export const useSubTaskDisplayStore = create<SubTaskDisplayStore>()(
  persist(
    (set) => ({
      rowProperties: { ...DEFAULT_SUB_TASK_ROW_PROPERTIES },
      rowPropertyIds: [],
      toggleRowProperty: (key) =>
        set((s) => ({
          rowProperties: { ...s.rowProperties, [key]: !s.rowProperties[key] },
        })),
      toggleRowPropertyId: (propertyId) =>
        set((s) => ({
          rowPropertyIds: s.rowPropertyIds.includes(propertyId)
            ? s.rowPropertyIds.filter((id) => id !== propertyId)
            : [...s.rowPropertyIds, propertyId],
        })),
    }),
    {
      name: "silieco_tr1_sub_task_display",
      storage: createJSONStorage(() => defaultStorage),
      // Deep-merge rowProperties so a key added in a future release defaults
      // to visible instead of undefined (persist's default shallow merge
      // would replace the whole object with the stale persisted one).
      merge: (persisted, current) => {
        const p = (persisted ?? {}) as Partial<SubTaskDisplayStore>;
        return {
          ...current,
          ...p,
          rowProperties: {
            ...current.rowProperties,
            ...(p.rowProperties ?? {}),
          },
        };
      },
    },
  ),
);
