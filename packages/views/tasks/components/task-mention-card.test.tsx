import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { TaskMentionCard } from "./task-mention-card";
import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";

const { taskLinkState } = vi.hoisted(() => ({
  taskLinkState: { openInNewTab: true, setOpenInNewTab: vi.fn() },
}));

vi.mock("@silieco/core/tasks/stores", () => {
  const useTaskLinkStore = (
    selector?: (s: typeof taskLinkState) => unknown,
  ) => (selector ? selector(taskLinkState) : taskLinkState);
  useTaskLinkStore.getState = () => taskLinkState;
  return { useTaskLinkStore };
});

vi.mock("@silieco/core/paths", () => ({
  useWorkspacePaths: () => ({
    taskDetail: (id: string) => `/acme/tasks/${id}`,
  }),
}));

vi.mock("./task-chip", () => ({
  TaskChip: ({ fallbackLabel }: { fallbackLabel?: string }) => (
    <span data-testid="task-chip">{fallbackLabel ?? "chip"}</span>
  ),
}));

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderCard(adapter: NavigationAdapter) {
  return render(
    <NavigationProvider value={adapter}>
      <TaskMentionCard taskId="task-1" fallbackLabel="SILI-7" />
    </NavigationProvider>,
  );
}

describe("TaskMentionCard", () => {
  beforeEach(() => {
    taskLinkState.openInNewTab = true;
  });

  it("with the new-tab preference on (default), plain click opens a foreground new tab and does not push", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    renderCard(makeAdapter({ push, openInNewTab }));

    const anchor = screen.getByTestId("task-chip").closest("a");
    expect(anchor).toHaveAttribute("target", "_blank");

    fireEvent.click(screen.getByTestId("task-chip"));
    expect(openInNewTab).toHaveBeenCalledWith("/acme/tasks/task-1", "SILI-7", {
      activate: true,
    });
    expect(push).not.toHaveBeenCalled();
  });

  it("with the preference off, plain click navigates in place", () => {
    taskLinkState.openInNewTab = false;
    const push = vi.fn();
    const openInNewTab = vi.fn();
    renderCard(makeAdapter({ push, openInNewTab }));

    const anchor = screen.getByTestId("task-chip").closest("a");
    expect(anchor).not.toHaveAttribute("target");

    fireEvent.click(screen.getByTestId("task-chip"));
    expect(push).toHaveBeenCalledWith("/acme/tasks/task-1");
    expect(openInNewTab).not.toHaveBeenCalled();
  });

  it("with the preference on but no adapter openInNewTab (web), leaves the click to the browser's native target=_blank handling", () => {
    const push = vi.fn();
    renderCard(makeAdapter({ push }));

    const defaultNotPrevented = fireEvent.click(
      screen.getByTestId("task-chip"),
    );
    expect(defaultNotPrevented).toBe(true);
    expect(push).not.toHaveBeenCalled();
  });
});
