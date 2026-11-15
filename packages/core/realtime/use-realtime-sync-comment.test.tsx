/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import type { WSClient } from "../api/ws-client";
import { taskKeys, type TaskSortParam } from "../tasks/queries";
import type { ListTasksCache } from "../types";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => "ws-1",
  getCurrentSlug: () => "test-ws",
  // Draft stores are now loaded transitively (storage-cleanup → register-all-drafts)
  // so their persist wiring must resolve against this mock.
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));

vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

// Records every ws.on handler by event name so a test can fire one directly.
function createRecordingWs(): {
  ws: WSClient;
  handlers: Record<string, (p: unknown) => void>;
} {
  const handlers: Record<string, (p: unknown) => void> = {};
  const ws = {
    on: vi.fn((event: string, handler: (p: unknown) => void) => {
      handlers[event] = handler;
      return () => {};
    }),
    onAny: vi.fn(() => () => {}),
    onReconnect: vi.fn(() => () => {}),
  } as unknown as WSClient;
  return { ws, handlers };
}

function createStores(): RealtimeSyncStores {
  return {
    authStore: Object.assign(() => ({}), {
      getState: () => ({ user: { id: "u1" } }),
      subscribe: () => () => {},
      setState: () => {},
      destroy: () => {},
    }),
  } as unknown as RealtimeSyncStores;
}

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

const updatedSort: TaskSortParam = {
  sort_by: "updated_at",
  sort_direction: "desc",
};
const positionSort: TaskSortParam = { sort_by: "position" };
const updatedBoardKey = taskKeys.listSorted("ws-1", updatedSort);
const positionBoardKey = taskKeys.listSorted("ws-1", positionSort);

function bucketed(): ListTasksCache {
  return { byStatus: { todo: { tasks: [], total: 1 } } };
}

describe("useRealtimeSync — comment:created re-sorts Updated date lists", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
    vi.clearAllMocks();
  });

  it("invalidates the updated_at-sorted board but leaves the position board", () => {
    qc.setQueryData<ListTasksCache>(updatedBoardKey, bucketed());
    qc.setQueryData<ListTasksCache>(positionBoardKey, bucketed());
    qc.setQueryData(taskKeys.timeline("task-1"), []);

    const { ws, handlers } = createRecordingWs();
    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });

    handlers["comment:created"]?.({
      comment: { id: "c1", task_id: "task-1" },
    });

    expect(qc.getQueryState(updatedBoardKey)?.isInvalidated).toBe(true);
    expect(qc.getQueryState(positionBoardKey)?.isInvalidated).toBe(false);
    // The per-task timeline is still invalidated as before.
    expect(
      qc.getQueryState(taskKeys.timeline("task-1"))?.isInvalidated,
    ).toBe(true);
  });

  it("ignores a comment event with no task_id", () => {
    qc.setQueryData<ListTasksCache>(updatedBoardKey, bucketed());

    const { handlers } = (() => {
      const rec = createRecordingWs();
      renderHook(() => useRealtimeSync(rec.ws, createStores()), {
        wrapper: createWrapper(qc),
      });
      return rec;
    })();

    handlers["comment:created"]?.({ comment: { id: "c1" } });

    expect(qc.getQueryState(updatedBoardKey)?.isInvalidated).toBe(false);
  });
});
