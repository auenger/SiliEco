import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import type { Task } from "@silieco/core/types";
import { BatchActionToolbar } from "./batch-action-toolbar";

// Mutable selection state shared with the store mock below. The real toolbar
// derives the pickers' current value from the tasks it can resolve out of
// this selection, so each test sets `selection.selectedIds` before rendering.
const selection = vi.hoisted(() => ({
  selectedIds: new Set<string>(),
  clear: () => {},
}));

vi.mock("@silieco/core/tasks/stores/selection-store", () => ({
  useTaskSelectionStore: (selector: (s: typeof selection) => unknown) =>
    selector(selection),
}));

vi.mock("@silieco/core/tasks/mutations", () => ({
  useBatchUpdateTasks: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useBatchDeleteTasks: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "label" }),
}));

// Render each picker as a probe that surfaces the value the toolbar passed in,
// so the test asserts the wiring (real `commonTaskFields` runs underneath).
vi.mock("./pickers", () => ({
  StatusPicker: ({ status }: { status: string | null }) => (
    <div data-testid="status-picker" data-status={status ?? "__none__"} />
  ),
  PriorityPicker: ({ priority }: { priority: string | null }) => (
    <div data-testid="priority-picker" data-priority={priority ?? "__none__"} />
  ),
  AssigneePicker: ({
    assigneeType,
    assigneeId,
    mixed,
  }: {
    assigneeType: string | null;
    assigneeId: string | null;
    mixed?: boolean;
  }) => (
    <div
      data-testid="assignee-picker"
      data-assignee-type={assigneeType ?? "__null__"}
      data-assignee-id={assigneeId ?? "__null__"}
      data-mixed={String(Boolean(mixed))}
    />
  ),
}));

function makeTask(overrides: Partial<Task> = {}): Task {
  return {
    id: "task-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "SILI-1",
    title: "Task 1",
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_task_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  selection.selectedIds = new Set();
});

describe("BatchActionToolbar picker wiring", () => {
  it("reflects the shared status / priority / assignee of the selected tasks", () => {
    const tasks = [
      makeTask({ id: "a", status: "in_progress", priority: "high", assignee_type: "member", assignee_id: "u-1" }),
      makeTask({ id: "b", status: "in_progress", priority: "high", assignee_type: "member", assignee_id: "u-1" }),
    ];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar tasks={tasks} />);

    expect(screen.getByTestId("status-picker")).toHaveAttribute("data-status", "in_progress");
    expect(screen.getByTestId("priority-picker")).toHaveAttribute("data-priority", "high");
    const assignee = screen.getByTestId("assignee-picker");
    expect(assignee).toHaveAttribute("data-assignee-type", "member");
    expect(assignee).toHaveAttribute("data-assignee-id", "u-1");
    expect(assignee).toHaveAttribute("data-mixed", "false");
  });

  it("falls back to an empty (no-checkmark) state when the selection is mixed", () => {
    const tasks = [
      makeTask({ id: "a", status: "todo", priority: "none", assignee_type: "member", assignee_id: "u-1" }),
      makeTask({ id: "b", status: "done", priority: "urgent", assignee_type: "agent", assignee_id: "ag-1" }),
    ];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar tasks={tasks} />);

    expect(screen.getByTestId("status-picker")).toHaveAttribute("data-status", "__none__");
    expect(screen.getByTestId("priority-picker")).toHaveAttribute("data-priority", "__none__");
    expect(screen.getByTestId("assignee-picker")).toHaveAttribute("data-mixed", "true");
  });

  it("treats an all-unassigned selection as unassigned, not mixed", () => {
    const tasks = [
      makeTask({ id: "a", assignee_type: null, assignee_id: null }),
      makeTask({ id: "b", assignee_type: null, assignee_id: null }),
    ];
    selection.selectedIds = new Set(["a", "b"]);

    render(<BatchActionToolbar tasks={tasks} />);

    const assignee = screen.getByTestId("assignee-picker");
    expect(assignee).toHaveAttribute("data-mixed", "false");
    expect(assignee).toHaveAttribute("data-assignee-type", "__null__");
    expect(assignee).toHaveAttribute("data-assignee-id", "__null__");
  });

  it("renders nothing when nothing is selected", () => {
    render(<BatchActionToolbar tasks={[makeTask({ id: "a" })]} />);
    expect(screen.queryByTestId("status-picker")).toBeNull();
  });

  it("removes the toolbar after the final selected task is cleared", async () => {
    const tasks = [makeTask({ id: "a" })];
    selection.selectedIds = new Set(["a"]);
    const view = render(<BatchActionToolbar tasks={tasks} />);

    expect(screen.getByTestId("status-picker")).toBeInTheDocument();
    selection.selectedIds = new Set();
    view.rerender(<BatchActionToolbar tasks={tasks} />);

    await waitFor(() => {
      expect(screen.queryByTestId("status-picker")).not.toBeInTheDocument();
    });
  });
});
