import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";

/**
 * Tracks which comments are collapsed, keyed by task ID.
 * Only collapsed comment IDs are stored — expanded is the default state.
 */
interface CommentCollapseStore {
  collapsedByTask: Record<string, string[]>;
  isCollapsed: (taskId: string, commentId: string) => boolean;
  toggle: (taskId: string, commentId: string) => void;
  /** Replace the task's collapsed set with `commentIds` (fold-all). */
  collapseAll: (taskId: string, commentIds: readonly string[]) => void;
  /** Clear the task's collapsed set (unfold-all). */
  expandAll: (taskId: string) => void;
}

export const useCommentCollapseStore = create<CommentCollapseStore>()(
  persist(
    (set, get) => ({
      collapsedByTask: {},
      isCollapsed: (taskId, commentId) => {
        const ids = get().collapsedByTask[taskId];
        return ids ? ids.includes(commentId) : false;
      },
      toggle: (taskId, commentId) =>
        set((s) => {
          const current = s.collapsedByTask[taskId] ?? [];
          const isCurrentlyCollapsed = current.includes(commentId);
          if (isCurrentlyCollapsed) {
            const next = current.filter((id) => id !== commentId);
            if (next.length === 0) {
              const { [taskId]: _, ...rest } = s.collapsedByTask;
              return { collapsedByTask: rest };
            }
            return { collapsedByTask: { ...s.collapsedByTask, [taskId]: next } };
          }
          return { collapsedByTask: { ...s.collapsedByTask, [taskId]: [...current, commentId] } };
        }),
      collapseAll: (taskId, commentIds) =>
        set((s) => {
          if (commentIds.length === 0) {
            if (!(taskId in s.collapsedByTask)) return s;
            const { [taskId]: _, ...rest } = s.collapsedByTask;
            return { collapsedByTask: rest };
          }
          return { collapsedByTask: { ...s.collapsedByTask, [taskId]: [...new Set(commentIds)] } };
        }),
      expandAll: (taskId) =>
        set((s) => {
          if (!(taskId in s.collapsedByTask)) return s;
          const { [taskId]: _, ...rest } = s.collapsedByTask;
          return { collapsedByTask: rest };
        }),
    }),
    {
      name: "silieco_tr1_comment_collapse",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useCommentCollapseStore.persist.rehydrate());
