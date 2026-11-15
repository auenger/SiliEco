/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@silieco/core/api";
import type { ApiClient } from "@silieco/core/api/client";
import type {
  Task,
  TaskStatus,
  TaskTableQuerySpec,
  TaskTableRowsRequest,
} from "@silieco/core/types";
import { useTaskStatusBranches } from "./use-task-status-branches";

function makeTask(id: string): Task {
  return {
    id,
    workspace_id: "ws-1",
    number: id === "task-1" ? 1 : 2,
    identifier: id === "task-1" ? "SILI-1" : "SILI-2",
    title: id,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_task_id: null,
    project_id: null,
    position: id === "task-1" ? 1 : 2,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

const query: TaskTableQuerySpec = {
  scope: { kind: "workspace" },
  filters: { include_sub_tasks: true },
  sort: { field: "position", direction: "asc" },
};

function wrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        {children}
      </QueryClientProvider>
    );
  };
}

describe("useTaskStatusBranches", () => {
  afterEach(() => cleanup());

  it("uses one cursor chain per active status and exact facet totals", async () => {
    const first = makeTask("task-1");
    const second = makeTask("task-2");
    const listTaskTableRows = vi.fn(
      async (request: TaskTableRowsRequest) => {
        const continuation = request.page?.cursor === "cursor-2";
        const task = continuation ? second : first;
        return {
          query_fingerprint: "test",
          group_key: request.group_key,
          parent_id: null,
          total: 0,
          rows: [{ task, direct_child_count: 0 }],
          branch_total: 1,
          next_cursor: continuation ? null : "cursor-2",
        };
      },
    );
    setApiInstance({ listTaskTableRows } as unknown as ApiClient);
    // Mirror the production client (createQueryClient): row pages stay fresh
    // until explicitly invalidated, so re-expanding a collapsed section reuses
    // settled cursor pages instead of refetching them on observer reattach.
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });

    const { result, rerender } = renderHook(
      ({ statuses }: { statuses: TaskStatus[] }) =>
        useTaskStatusBranches({
          wsId: "ws-1",
          query,
          statuses,
          facets: {
            query_fingerprint: "test",
            total: 2,
            facets: [
              {
                kind: "status",
                values: [{ key: "todo", count: 2 }],
              },
            ],
          },
          facetsPending: false,
          facetsFetching: false,
          enabled: true,
        }),
      {
        initialProps: { statuses: ["todo"] },
        wrapper: wrapper(queryClient),
      },
    );

    await waitFor(() =>
      expect(result.current.tasks.map((task) => task.id)).toEqual([
        "task-1",
      ]),
    );
    expect(result.current.pagination.todo.total).toBe(2);
    expect(result.current.pagination.todo.hasMore).toBe(true);
    expect(result.current.total).toBe(2);
    expect(result.current.isTotalKnown).toBe(true);

    act(() => result.current.pagination.todo.loadMore());
    await waitFor(() =>
      expect(result.current.tasks.map((task) => task.id)).toEqual([
        "task-1",
        "task-2",
      ]),
    );
    expect(listTaskTableRows).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        group_key: "status:todo",
        page: { limit: 50, cursor: "cursor-2" },
      }),
    );

    // Collapsing a List section removes its active observers. Re-expanding
    // reuses the settled cursor pages instead of restarting another chain.
    rerender({ statuses: [] });
    expect(result.current.tasks).toEqual([]);
    rerender({ statuses: ["todo"] });
    await waitFor(() => expect(result.current.tasks).toHaveLength(2));
    expect(listTaskTableRows).toHaveBeenCalledTimes(2);

    queryClient.clear();
  });
});
