import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

/**
 * How task mention links inside content (task descriptions, comments,
 * chat markdown) open on plain click.
 *
 * `openInNewTab: true` (default) opens the linked task in a new tab — a
 * browser tab on web, an app tab on desktop — so the reader keeps their
 * place in the task they came from. `false` navigates in place. It's a
 * personal reading-ergonomics preference (like theme), so it persists
 * globally via `defaultStorage` rather than per-workspace storage.
 */
interface TaskLinkStore {
  openInNewTab: boolean;
  setOpenInNewTab: (open: boolean) => void;
}

export const useTaskLinkStore = create<TaskLinkStore>()(
  persist(
    (set) => ({
      openInNewTab: true,
      setOpenInNewTab: (open) => set({ openInNewTab: open }),
    }),
    {
      name: "silieco_tr1_task_link",
      storage: createJSONStorage(() => defaultStorage),
    },
  ),
);
