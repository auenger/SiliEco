import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Task } from "@silieco/core/types";

vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const mockOpenModal = vi.fn();
vi.mock("@silieco/core/modals", () => ({
  useModalStore: Object.assign(
    (selector?: any) => {
      const state = { open: mockOpenModal };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ open: mockOpenModal }) },
  ),
}));

const mockAuthState = { user: { id: "user-1" }, isAuthenticated: true };
vi.mock("@silieco/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: any) => (selector ? selector(mockAuthState) : mockAuthState),
    { getState: () => mockAuthState },
  ),
  registerAuthStore: vi.fn(),
}));

// Mutable so individual tests can seed the pin list.
const pinListRef: { value: Array<{ item_type: string; item_id: string }> } = {
  value: [],
};
const mockCreatePinMutate = vi.fn();
const mockDeletePinMutate = vi.fn();
vi.mock("@silieco/core/pins", () => ({
  pinListOptions: () => ({
    queryKey: ["pins", "ws-1", "user-1"],
    queryFn: () => Promise.resolve(pinListRef.value),
  }),
  useCreatePin: () => ({ mutate: mockCreatePinMutate }),
  useDeletePin: () => ({ mutate: mockDeletePinMutate }),
}));

const mockUpdateMutate = vi.fn();
vi.mock("@silieco/core/tasks/mutations", () => ({
  useUpdateTask: () => ({ mutate: mockUpdateMutate }),
}));

vi.mock("@silieco/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@silieco/core/paths")>(
    "@silieco/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Test", slug: "test" }),
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

vi.mock("../../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    pathname: "/test/tasks/task-1",
    searchParams: new URLSearchParams(),
    back: vi.fn(),
    replace: vi.fn(),
    getShareableUrl: (p: string) => `https://app.silieco.com${p}`,
  }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

// Import AFTER mocks are registered.
import { toast } from "sonner";
import { useTaskActions } from "../use-task-actions";

const mockTask: Task = {
  id: "task-1",
  workspace_id: "ws-1",
  number: 1,
  identifier: "TES-1",
  title: "Example",
  description: null,
  status: "todo",
  priority: "medium",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_task_id: null,
  start_date: null,
  due_date: null,
  project_id: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
} as Task;

function wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  mockOpenModal.mockReset();
  mockUpdateMutate.mockReset();
  mockCreatePinMutate.mockReset();
  mockDeletePinMutate.mockReset();
  vi.mocked(toast.success).mockReset();
  vi.mocked(toast.error).mockReset();
  pinListRef.value = [];
  localStorage.clear();
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText: vi.fn().mockResolvedValue(undefined) },
  });
});

describe("useTaskActions", () => {
  it("updateField dispatches useUpdateTask.mutate with the correct payload", () => {
    const { result } = renderHook(() => useTaskActions(mockTask), { wrapper });

    act(() => {
      result.current.updateField({ status: "done" });
    });

    expect(mockUpdateMutate).toHaveBeenCalledWith(
      { id: "task-1", status: "done" },
      expect.any(Object),
    );
  });

  it("assigning an agent routes through the run-confirm modal instead of mutating directly", () => {
    const { result } = renderHook(() => useTaskActions(mockTask), { wrapper });

    act(() => {
      result.current.updateField({
        assignee_type: "agent",
        assignee_id: "agent-1",
      });
    });

    expect(mockOpenModal).toHaveBeenCalledWith("task-run-confirm", {
      taskIds: ["task-1"],
      mode: "assign",
      assigneeType: "agent",
      assigneeId: "agent-1",
    });
    expect(mockUpdateMutate).not.toHaveBeenCalled();
  });

  it("assigning an agent to a backlog task applies directly — backlog never starts a run", () => {
    const backlogTask = { ...mockTask, status: "backlog" } as Task;
    const { result } = renderHook(() => useTaskActions(backlogTask), { wrapper });

    act(() => {
      result.current.updateField({
        assignee_type: "agent",
        assignee_id: "agent-1",
      });
    });

    expect(mockUpdateMutate).toHaveBeenCalledWith(
      { id: "task-1", assignee_type: "agent", assignee_id: "agent-1" },
      expect.any(Object),
    );
    expect(mockOpenModal).not.toHaveBeenCalled();
  });

  it("assigning a member applies directly without the run-confirm modal", () => {
    const { result } = renderHook(() => useTaskActions(mockTask), { wrapper });

    act(() => {
      result.current.updateField({
        assignee_type: "member",
        assignee_id: "user-1",
      });
    });

    expect(mockUpdateMutate).toHaveBeenCalledWith(
      { id: "task-1", assignee_type: "member", assignee_id: "user-1" },
      expect.any(Object),
    );
    expect(mockOpenModal).not.toHaveBeenCalled();
  });

  it("copyLink writes the task's shareable URL to the clipboard", async () => {
    const { result } = renderHook(() => useTaskActions(mockTask), { wrapper });

    await act(async () => {
      await result.current.copyLink();
    });

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
      "https://app.silieco.com/test/tasks/task-1",
    );
  });

  it("openSetParent / openAddChild / openDeleteConfirm / openCreateSubTask open the correct modal with payload", () => {
    const { result } = renderHook(() => useTaskActions(mockTask), { wrapper });

    act(() => {
      result.current.openSetParent();
    });
    expect(mockOpenModal).toHaveBeenLastCalledWith("task-set-parent", {
      taskId: "task-1",
    });

    act(() => {
      result.current.openAddChild();
    });
    expect(mockOpenModal).toHaveBeenLastCalledWith("task-add-child", {
      taskId: "task-1",
    });

    act(() => {
      result.current.openCreateSubTask();
    });
    expect(mockOpenModal).toHaveBeenLastCalledWith("create-task", {
      parent_task_id: "task-1",
      parent_task_identifier: "TES-1",
    });

    act(() => {
      result.current.openDeleteConfirm({ onDeletedFallbackPath: "/test/tasks" });
    });
    expect(mockOpenModal).toHaveBeenLastCalledWith("task-delete-confirm", {
      taskId: "task-1",
      identifier: "TES-1",
      onDeletedFallbackPath: "/test/tasks",
    });
  });

  it("removeParent clears parent_task_id and stage in one write, never via the run-confirm modal", () => {
    const childTask = {
      ...mockTask,
      parent_task_id: "parent-1",
      stage: 2,
    } as Task;
    const { result } = renderHook(() => useTaskActions(childTask), { wrapper });

    act(() => {
      result.current.removeParent();
    });

    expect(mockUpdateMutate).toHaveBeenCalledWith(
      { id: "task-1", parent_task_id: null, stage: null },
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
      }),
    );
    // Detaching never routes through the run-confirm modal.
    expect(mockOpenModal).not.toHaveBeenCalled();
  });

  it("removeParent toasts success only from onSuccess — not eagerly, and not on failure", () => {
    const childTask = {
      ...mockTask,
      parent_task_id: "parent-1",
    } as Task;
    const { result } = renderHook(() => useTaskActions(childTask), { wrapper });

    act(() => {
      result.current.removeParent();
    });

    // mutate() is fire-and-forget here (mocked), so nothing is confirmed yet.
    expect(toast.success).not.toHaveBeenCalled();
    expect(mockUpdateMutate).toHaveBeenCalledTimes(1);

    const opts = mockUpdateMutate.mock.calls[0]![1] as {
      onSuccess: () => void;
      onError: (err: unknown) => void;
    };

    // A failed write surfaces the error, never a false "removed" confirmation.
    act(() => {
      opts.onError(new Error("forbidden"));
    });
    expect(toast.error).toHaveBeenCalledWith("forbidden");
    expect(toast.success).not.toHaveBeenCalled();

    // Only the server-confirmed success toasts.
    act(() => {
      opts.onSuccess();
    });
    expect(toast.success).toHaveBeenCalledTimes(1);
  });

  it("togglePin calls createPin when not pinned and deletePin when pinned", async () => {
    pinListRef.value = [];
    const { result: r1 } = renderHook(() => useTaskActions(mockTask), { wrapper });
    await waitFor(() => {
      expect(r1.current.isPinned).toBe(false);
    });
    act(() => {
      r1.current.togglePin();
    });
    expect(mockCreatePinMutate).toHaveBeenCalledWith({
      item_type: "task",
      item_id: "task-1",
    });
    expect(mockDeletePinMutate).not.toHaveBeenCalled();

    mockCreatePinMutate.mockReset();
    mockDeletePinMutate.mockReset();
    pinListRef.value = [{ item_type: "task", item_id: "task-1" }];
    const { result: r2 } = renderHook(() => useTaskActions(mockTask), { wrapper });
    await waitFor(() => {
      expect(r2.current.isPinned).toBe(true);
    });
    act(() => {
      r2.current.togglePin();
    });
    expect(mockDeletePinMutate).toHaveBeenCalledWith({
      itemType: "task",
      itemId: "task-1",
    });
    expect(mockCreatePinMutate).not.toHaveBeenCalled();
  });

  it("is a safe no-op when task is null", () => {
    const { result } = renderHook(() => useTaskActions(null), { wrapper });

    act(() => {
      result.current.updateField({ status: "done" });
      result.current.togglePin();
      result.current.openSetParent();
    });

    expect(mockUpdateMutate).not.toHaveBeenCalled();
    expect(mockOpenModal).not.toHaveBeenCalled();
  });

});
