/**
 * View store for the workspace-wide Tasks page (`more/tasks.tsx`).
 *
 * Shape mirrors `useMyTasksViewStore` plus a `scope` field — workspace
 * Tasks has `all / members / agents` scope tabs (see web
 * `packages/views/tasks/components/tasks-page.tsx:32-94`), while
 * My Tasks has its own `assigned / created / agents` scopes.
 *
 * The `scope` filter is **client-side** on `assignee_type` — see
 * `more/tasks.tsx`'s `scopedTasks` derivation. Server param stays unset
 * so the cache key (`taskKeys.list(wsId)`) and WS realtime invalidation
 * (`useTasksRealtime`) don't have to know about scope.
 *
 * `TasksScope` is defined locally rather than imported from
 * `@silieco/core/tasks/stores/tasks-scope-store` — mobile only
 * `import type` from `@silieco/core/types/*` per Sharing Principles, and
 * the union is small enough that a duplicated literal is preferable to a
 * cross-package type import hop.
 *
 * Empty filter array = "show all" (matches web's predicate semantics in
 * packages/views/tasks/utils/filter.ts).
 *
 * No persist middleware — filters are session-scoped. `clearFilters`
 * deliberately does NOT reset `scope` so a workspace switch keeps the
 * user on the same scope tab (web's URL-driven scope reset is incidental
 * to its routing model, not an invariant mobile should mirror).
 */
import { create } from "zustand";
import type { TaskPriority, TaskStatus } from "@silieco/core/types";

export type TasksScope = "all" | "members" | "agents";

interface TasksViewState {
  scope: TasksScope;
  statusFilters: TaskStatus[];
  priorityFilters: TaskPriority[];
  setScope: (scope: TasksScope) => void;
  toggleStatusFilter: (status: TaskStatus) => void;
  togglePriorityFilter: (priority: TaskPriority) => void;
  clearFilters: () => void;
}

export const useTasksViewStore = create<TasksViewState>((set) => ({
  scope: "all",
  statusFilters: [],
  priorityFilters: [],
  setScope: (scope) => set({ scope }),
  toggleStatusFilter: (status) =>
    set((state) => ({
      statusFilters: state.statusFilters.includes(status)
        ? state.statusFilters.filter((s) => s !== status)
        : [...state.statusFilters, status],
    })),
  togglePriorityFilter: (priority) =>
    set((state) => ({
      priorityFilters: state.priorityFilters.includes(priority)
        ? state.priorityFilters.filter((p) => p !== priority)
        : [...state.priorityFilters, priority],
    })),
  clearFilters: () => set({ statusFilters: [], priorityFilters: [] }),
}));
