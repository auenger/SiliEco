import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useQuery } from "@tanstack/react-query";
import { TaskChip } from "./task-chip";

vi.mock("@tanstack/react-query", () => ({
  useQuery: vi.fn(),
}));

vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@silieco/core/tasks/queries", () => ({
  taskListOptions: () => ({ queryKey: ["tasks"] }),
  taskDetailOptions: (_workspaceId: string, taskId: string) => ({
    queryKey: ["task", taskId],
  }),
}));

vi.mock("./status-icon", () => ({
  StatusIcon: ({ className }: { className?: string }) => (
    <svg data-testid="status-icon" className={className} />
  ),
}));

const mockUseQuery = vi.mocked(useQuery);

describe("TaskChip", () => {
  beforeEach(() => {
    mockUseQuery.mockImplementation((options: { queryKey?: readonly unknown[] }) => {
      if (options.queryKey?.[0] === "tasks") {
        return {
          data: [
            {
              id: "task-1",
              identifier: "SILI-3405",
              title: "A very long task title that should stay inside a narrow chat bubble",
              status: "todo",
            },
          ],
        } as ReturnType<typeof useQuery>;
      }
      return { data: undefined } as ReturnType<typeof useQuery>;
    });
  });

  it("caps the chip to its parent container and truncates the title", () => {
    render(<TaskChip taskId="task-1" />);

    const chip = screen.getByText("SILI-3405").closest(".task-mention");
    expect(chip).toHaveClass("min-w-0", "max-w-full");
    expect(screen.getByText("A very long task title that should stay inside a narrow chat bubble"))
      .toHaveClass("min-w-0", "truncate");
  });

  it("truncates unresolved fallback labels inside the chip width", () => {
    render(
      <TaskChip
        taskId="missing-task"
        fallbackLabel="SILI-999999999999999999999999999999999"
      />,
    );

    expect(screen.getByText("SILI-999999999999999999999999999999999"))
      .toHaveClass("min-w-0", "truncate");
  });
});
