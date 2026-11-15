import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { DeleteTaskConfirmModal } from "./delete-task-confirm";
import { NavigationProvider } from "../navigation";
import type { NavigationAdapter } from "../navigation";

const mockDelete = vi.fn().mockResolvedValue(undefined);
vi.mock("@silieco/core/tasks/mutations", () => ({
  useDeleteTask: () => ({ mutateAsync: mockDelete }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("../i18n", () => ({
  useT: () => ({
    t: (sel: (x: Record<string, Record<string, string>>) => string) =>
      sel({
        delete_task: {
          title: "Delete task?",
          description: "This cannot be undone.",
          hint: "Sub-tasks are deleted too.",
          cancel: "Cancel",
          confirm: "Delete",
          deleting: "Deleting...",
          toast_deleted: "Task deleted",
          toast_delete_failed: "Delete failed",
        },
      }),
  }),
}));

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/tasks/task-1",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

async function deleteWith(
  adapter: NavigationAdapter,
  data: Record<string, unknown> | null,
) {
  const onClose = vi.fn();
  render(
    <NavigationProvider value={adapter}>
      <DeleteTaskConfirmModal onClose={onClose} data={data} />
    </NavigationProvider>,
  );
  fireEvent.click(screen.getByText("Delete"));
  await waitFor(() => expect(mockDelete).toHaveBeenCalledWith("task-1"));
  return onClose;
}

describe("DeleteTaskConfirmModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // The bug this guards (GH #5995): deleting from an task opened out of My
  // Tasks used to hard-push the workspace Tasks list, dropping the user's
  // navigation context. The same applied to project lists, search and pins.
  it("returns to the list the task was opened from", async () => {
    const adapter = makeAdapter({ canGoBack: () => true });

    await deleteWith(adapter, {
      taskId: "task-1",
      onDeletedFallbackPath: "/acme/tasks",
    });

    await waitFor(() => expect(adapter.back).toHaveBeenCalledTimes(1));
    expect(adapter.replace).not.toHaveBeenCalled();
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("falls back to the workspace list when the task was opened cold", async () => {
    const adapter = makeAdapter({ canGoBack: () => false });

    await deleteWith(adapter, {
      taskId: "task-1",
      onDeletedFallbackPath: "/acme/tasks",
    });

    await waitFor(() =>
      expect(adapter.replace).toHaveBeenCalledWith("/acme/tasks"),
    );
    expect(adapter.back).not.toHaveBeenCalled();
  });

  // List surfaces delete in place (row context menu, batch toolbar): there is
  // no page to leave, so the modal must not navigate at all.
  it("does not navigate when no fallback path is supplied", async () => {
    const adapter = makeAdapter({ canGoBack: () => true });

    const onClose = await deleteWith(adapter, { taskId: "task-1" });

    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(adapter.back).not.toHaveBeenCalled();
    expect(adapter.replace).not.toHaveBeenCalled();
    expect(adapter.push).not.toHaveBeenCalled();
  });

  it("stays put when the delete fails", async () => {
    mockDelete.mockRejectedValueOnce(new Error("nope"));
    const adapter = makeAdapter({ canGoBack: () => true });

    render(
      <NavigationProvider value={adapter}>
        <DeleteTaskConfirmModal
          onClose={vi.fn()}
          data={{ taskId: "task-1", onDeletedFallbackPath: "/acme/tasks" }}
        />
      </NavigationProvider>,
    );
    fireEvent.click(screen.getByText("Delete"));

    await waitFor(() => expect(screen.getByText("Delete")).toBeInTheDocument());
    expect(adapter.back).not.toHaveBeenCalled();
    expect(adapter.replace).not.toHaveBeenCalled();
  });
});
