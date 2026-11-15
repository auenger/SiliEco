"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useTaskSelectionStore } from "@silieco/core/tasks/stores/selection-store";

export interface TaskSurfaceSelection {
  selectedIds: Set<string>;
  toggle: (id: string) => void;
  select: (ids: string[]) => void;
  deselect: (ids: string[]) => void;
  clear: () => void;
}

const TaskSurfaceSelectionContext =
  createContext<TaskSurfaceSelection | null>(null);

export function useCreateTaskSurfaceSelection(
  surfaceKey: string,
  resetKey = surfaceKey,
): TaskSurfaceSelection {
  const [selectedIds, setSelectedIds] = useState(() => new Set<string>());
  const [committedResetKey, setCommittedResetKey] = useState(resetKey);

  // Render-phase reset (the React "adjusting state when props change"
  // pattern), NOT an effect: an effect runs after commit, so one frame would
  // ship the NEW membership (filters/search) with the OLD selection — batch
  // consumers could act on rows the window no longer contains during that
  // gap. Setting state during render re-renders synchronously before commit,
  // so no such frame ever becomes visible. The size guard keeps the common
  // nothing-selected key change from allocating a new Set and re-rendering
  // the surface.
  if (committedResetKey !== resetKey) {
    setCommittedResetKey(resetKey);
    if (selectedIds.size > 0) setSelectedIds(new Set());
  }

  const toggle = useCallback((id: string) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const select = useCallback((ids: string[]) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      for (const id of ids) next.add(id);
      return next;
    });
  }, []);

  const deselect = useCallback((ids: string[]) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      for (const id of ids) next.delete(id);
      return next;
    });
  }, []);

  const clear = useCallback(() => {
    setSelectedIds(new Set());
  }, []);

  return useMemo(
    () => ({ selectedIds, toggle, select, deselect, clear }),
    [clear, deselect, select, selectedIds, toggle],
  );
}

export function TaskSurfaceSelectionProvider({
  selection,
  children,
}: {
  selection: TaskSurfaceSelection;
  children: ReactNode;
}) {
  return (
    <TaskSurfaceSelectionContext.Provider value={selection}>
      {children}
    </TaskSurfaceSelectionContext.Provider>
  );
}

export function useTaskSurfaceSelectionOptional() {
  return useContext(TaskSurfaceSelectionContext);
}

export function useTaskSurfaceSelection(): TaskSurfaceSelection {
  const surfaceSelection = useTaskSurfaceSelectionOptional();
  const globalSelectedIds = useTaskSelectionStore((s) => s.selectedIds);
  const globalToggle = useTaskSelectionStore((s) => s.toggle);
  const globalSelect = useTaskSelectionStore((s) => s.select);
  const globalDeselect = useTaskSelectionStore((s) => s.deselect);
  const globalClear = useTaskSelectionStore((s) => s.clear);

  return (
    surfaceSelection ?? {
      selectedIds: globalSelectedIds,
      toggle: globalToggle,
      select: globalSelect,
      deselect: globalDeselect,
      clear: globalClear,
    }
  );
}
