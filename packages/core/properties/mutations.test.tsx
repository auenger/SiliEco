/**
 * @vitest-environment jsdom
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { Task, TaskPropertiesResponse } from "../types";
import { taskKeys } from "../tasks/queries";
import { useSetTaskProperty } from "./mutations";

vi.mock("../hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const task: Task = {
  id: "task-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "SILI-1",
  title: "Estimate",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "member-1",
  parent_task_id: null,
  project_id: null,
  position: 1,
  stage: null,
  start_date: null,
  due_date: null,
  labels: [],
  metadata: {},
  properties: { estimate: 1 },
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

function wrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("useSetTaskProperty", () => {
  afterEach(() => vi.restoreAllMocks());

  it("keeps an optimistic children-cache patch when an older fetch resolves late", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const parentId = "parent-1";
    const childKey = taskKeys.children("ws-1", parentId);
    const child = {
      ...task,
      parent_task_id: parentId,
      properties: { estimate: 1, environment: "staging" },
    };
    qc.setQueryData<Task[]>(childKey, [child]);

    const staleChildren = deferred<Task[]>();
    const staleFetch = qc
      .fetchQuery({
        queryKey: childKey,
        queryFn: () => staleChildren.promise,
      })
      .catch(() => undefined);
    await waitFor(() =>
      expect(qc.getQueryState(childKey)?.fetchStatus).toBe("fetching"),
    );

    const write = deferred<TaskPropertiesResponse>();
    setApiInstance({
      setTaskProperty: vi.fn(() => write.promise),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useSetTaskProperty(), {
      wrapper: wrapper(qc),
    });
    act(() => {
      result.current.mutate({
        taskId: task.id,
        propertyId: "estimate",
        value: 2,
      });
    });

    await waitFor(() =>
      expect(qc.getQueryData<Task[]>(childKey)?.[0]?.properties).toEqual({
        estimate: 2,
        environment: "staging",
      }),
    );

    // Resolve the response captured before the mutation. cancelQueries must
    // keep it from restoring estimate=1 while the write is still pending.
    staleChildren.resolve([child]);
    await staleFetch;
    expect(qc.getQueryData<Task[]>(childKey)?.[0]?.properties).toEqual({
      estimate: 2,
      environment: "staging",
    });

    await act(async () => {
      write.resolve({
        properties: { estimate: 2, environment: "staging" },
      });
    });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    qc.clear();
  });

  it("does not refetch a property window before the mutation commits", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const flatKey = taskKeys.flat(
      "ws-1",
      "workspace:all",
      {},
      { sort_by: "property:estimate", properties: { estimate: ["2"] } },
    );
    qc.setQueryData(flatKey, {
      pages: [{ tasks: [task], total: 1 }],
      pageParams: [0],
    });

    let resolveWrite!: (value: TaskPropertiesResponse) => void;
    const setTaskProperty = vi.fn(
      () =>
        new Promise<TaskPropertiesResponse>((resolve) => {
          resolveWrite = resolve;
        }),
    );
    setApiInstance({ setTaskProperty } as unknown as ApiClient);

    const { result } = renderHook(() => useSetTaskProperty(), {
      wrapper: wrapper(qc),
    });
    act(() => {
      result.current.mutate({
        taskId: task.id,
        propertyId: "estimate",
        value: 2,
      });
    });

    await waitFor(() =>
      expect(
        qc.getQueryData<{ pages: { tasks: Task[] }[] }>(flatKey)?.pages[0]
          ?.tasks[0]?.properties.estimate,
      ).toBe(2),
    );
    expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(false);

    await act(async () => {
      resolveWrite({ properties: { estimate: 2 } });
    });
    await waitFor(() =>
      expect(qc.getQueryState(flatKey)?.isInvalidated).toBe(true),
    );
    qc.clear();
  });
});
