/**
 * Editor mention modifier-click (SILI-5456).
 *
 * Both mention chips render a real `<a href>`, so on web the correct move is
 * to leave a modifier-click alone and let the browser do it — that keeps
 * cmd+click (background tab), shift+click (new window) and cmd+shift+click
 * (foreground tab) distinct, which `window.open` would flatten into one.
 *
 * The project mention used to `preventDefault()` at the top of the handler and
 * then return without an adapter, producing a dead click on web. These tests
 * pin the no-adapter path for both chips.
 */
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { NavigationProvider } from "../../navigation/context";
import type { NavigationAdapter } from "../../navigation/types";

// Tiptap NodeView primitives can't be instantiated without a full editor.
vi.mock("@tiptap/react", () => ({
  NodeViewWrapper: ({ children, ...rest }: any) => <span {...rest}>{children}</span>,
}));

vi.mock("@silieco/core/paths", () => ({
  useWorkspacePaths: () => ({
    taskDetail: (id: string) => `/acme/tasks/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));

const { newTabPreferred } = vi.hoisted(() => ({ newTabPreferred: { value: false } }));

vi.mock("@silieco/core/tasks/stores", () => ({
  useTaskLinkStore: (selector: (s: { openInNewTab: boolean }) => unknown) =>
    selector({ openInNewTab: newTabPreferred.value }),
}));

vi.mock("../../tasks/components/task-chip", () => ({
  TaskChip: ({ fallbackLabel }: { fallbackLabel?: string }) => (
    <span data-testid="task-chip">{fallbackLabel}</span>
  ),
}));

vi.mock("../../projects/components/project-chip", () => ({
  ProjectChip: ({ fallbackLabel }: { fallbackLabel?: string }) => (
    <span data-testid="project-chip">{fallbackLabel}</span>
  ),
}));

import { MentionView } from "./mention-view";

const PROJECT_ID = "8f14e45f-ceea-4d0e-a1a2-9b1c0d3e4f5a";
const PROJECT_PATH = `/acme/projects/${PROJECT_ID}`;

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => `https://app.example${p}`,
    ...overrides,
  };
}

function renderMention(
  attrs: { type: string; id: string; label?: string },
  adapter: NavigationAdapter,
) {
  return render(
    <NavigationProvider value={adapter}>
      <MentionView {...({ node: { attrs } } as any)} />
    </NavigationProvider>,
  );
}

function renderProjectMention(adapter: NavigationAdapter) {
  return renderMention({ type: "project", id: PROJECT_ID, label: "Roadmap" }, adapter);
}

describe("MentionView project mention", () => {
  it("renders an anchor carrying the project path", () => {
    renderProjectMention(makeAdapter());

    expect(screen.getByTestId("project-chip").closest("a")).toHaveAttribute(
      "href",
      PROJECT_PATH,
    );
  });

  it("pushes on plain click and prevents the anchor's default navigation", () => {
    const push = vi.fn();
    renderProjectMention(makeAdapter({ push }));

    // fireEvent returns false when preventDefault was called.
    const defaultNotPrevented = fireEvent.click(screen.getByTestId("project-chip"));

    expect(defaultNotPrevented).toBe(false);
    expect(push).toHaveBeenCalledWith(PROJECT_PATH);
  });

  it("uses openInNewTab for cmd/ctrl click when available (desktop)", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    renderProjectMention(makeAdapter({ push, openInNewTab }));

    const defaultNotPrevented = fireEvent.click(screen.getByTestId("project-chip"), {
      metaKey: true,
    });

    expect(defaultNotPrevented).toBe(false);
    expect(openInNewTab).toHaveBeenCalledWith(PROJECT_PATH, "Roadmap");
    expect(push).not.toHaveBeenCalled();
  });

  it("leaves modifier-click to the browser when openInNewTab is absent (web)", () => {
    const push = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderProjectMention(makeAdapter({ push }));

    for (const modifier of [{ metaKey: true }, { ctrlKey: true }, { shiftKey: true }]) {
      const defaultNotPrevented = fireEvent.click(
        screen.getByTestId("project-chip"),
        modifier,
      );
      expect(defaultNotPrevented).toBe(true);
    }

    expect(push).not.toHaveBeenCalled();
    // Native anchor behaviour, not window.open — the latter would collapse
    // background tab / new window / foreground tab into one outcome.
    expect(open).not.toHaveBeenCalled();
    open.mockRestore();
  });
});

describe("MentionView task mention", () => {
  const TASK_ID = "1b9d6bcd-bbfd-4b2d-9b5d-ab8dfbbd4bed";
  const TASK_PATH = `/acme/tasks/${TASK_ID}`;

  // The reference implementation the project mention was aligned to — guard it
  // so the two chips can't drift apart again.
  it("leaves modifier-click to the browser when openInNewTab is absent (web)", () => {
    const push = vi.fn();
    renderMention({ type: "task", id: TASK_ID, label: "SILI-7" }, makeAdapter({ push }));

    const defaultNotPrevented = fireEvent.click(screen.getByTestId("task-chip"), {
      metaKey: true,
    });

    expect(defaultNotPrevented).toBe(true);
    expect(push).not.toHaveBeenCalled();
  });

  it("uses openInNewTab for cmd/ctrl click when available (desktop)", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    renderMention(
      { type: "task", id: TASK_ID, label: "SILI-7" },
      makeAdapter({ push, openInNewTab }),
    );

    const defaultNotPrevented = fireEvent.click(screen.getByTestId("task-chip"), {
      metaKey: true,
    });

    expect(defaultNotPrevented).toBe(false);
    expect(openInNewTab).toHaveBeenCalledWith(TASK_PATH, "SILI-7");
    expect(push).not.toHaveBeenCalled();
  });
});
