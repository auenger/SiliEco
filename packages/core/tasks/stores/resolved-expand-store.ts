import { create } from "zustand";

/**
 * Tracks which resolved threads are temporarily expanded, keyed by task ID.
 * Only expanded thread-root IDs are stored — folded-to-a-bar is the default.
 *
 * Deliberately NOT persisted (matches Linear): a reload folds every resolved
 * thread back to its summary bar. It lives in a store rather than task-detail
 * component state so the command palette's fold/unfold-all-comments commands
 * can drive it from outside the task page.
 */
interface ResolvedExpandStore {
  expandedByTask: Record<string, ReadonlySet<string>>;
  setExpanded: (taskId: string, commentId: string, expand: boolean) => void;
  /** Expand every thread root in `commentIds` at once (unfold-all). */
  expandAll: (taskId: string, commentIds: readonly string[]) => void;
  /** Fold every expanded thread back to its bar (fold-all). */
  collapseAll: (taskId: string) => void;
}

const EMPTY_EXPANDED: ReadonlySet<string> = new Set();

function withoutTask(
  expandedByTask: Record<string, ReadonlySet<string>>,
  taskId: string,
) {
  const { [taskId]: _, ...rest } = expandedByTask;
  return rest;
}

export const useResolvedExpandStore = create<ResolvedExpandStore>()((set) => ({
  expandedByTask: {},
  setExpanded: (taskId, commentId, expand) =>
    set((s) => {
      const current = s.expandedByTask[taskId] ?? EMPTY_EXPANDED;
      if (current.has(commentId) === expand) return s;
      const next = new Set(current);
      if (expand) next.add(commentId);
      else next.delete(commentId);
      if (next.size === 0) {
        return { expandedByTask: withoutTask(s.expandedByTask, taskId) };
      }
      return { expandedByTask: { ...s.expandedByTask, [taskId]: next } };
    }),
  expandAll: (taskId, commentIds) =>
    set((s) => {
      if (commentIds.length === 0) return s;
      const next = new Set(s.expandedByTask[taskId] ?? EMPTY_EXPANDED);
      for (const id of commentIds) next.add(id);
      return { expandedByTask: { ...s.expandedByTask, [taskId]: next } };
    }),
  collapseAll: (taskId) =>
    set((s) => {
      if (!(taskId in s.expandedByTask)) return s;
      return { expandedByTask: withoutTask(s.expandedByTask, taskId) };
    }),
}));

/**
 * Stable-reference selector for one task's expanded set (falls back to a
 * shared frozen empty set so unrelated store writes don't re-render readers).
 */
export function selectExpandedResolved(taskId: string) {
  return (s: ResolvedExpandStore): ReadonlySet<string> =>
    s.expandedByTask[taskId] ?? EMPTY_EXPANDED;
}
