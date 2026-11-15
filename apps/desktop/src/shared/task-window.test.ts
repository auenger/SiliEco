import { describe, expect, it } from "vitest";
import {
  encodeTaskWindowArgument,
  parseTaskWindowPath,
  parseTaskWindowRequest,
  readDesktopWindowContext,
} from "./task-window";

describe("task window request", () => {
  it("accepts and canonicalizes a workspace-scoped task detail path", () => {
    expect(
      parseTaskWindowRequest({
        path: "/acme/tasks/task-123?comment=comment-1#activity",
        title: "  SILI-1: Fix tabs  ",
      }),
    ).toEqual({
      kind: "task",
      path: "/acme/tasks/task-123?comment=comment-1#activity",
      title: "SILI-1: Fix tabs",
      workspaceSlug: "acme",
      taskId: "task-123",
    });
  });

  it.each([
    "/acme/tasks",
    "/acme/projects/project-1",
    "/acme/tasks/task-1/attachments",
    "https://example.com/acme/tasks/task-1",
    "//example.com/acme/tasks/task-1",
    "/acme/tasks/task%2F1",
    "/UPPERCASE/tasks/task-1",
  ])("rejects non-task or unsafe paths: %s", (path) => {
    expect(parseTaskWindowPath(path)).toBeNull();
  });

  it("uses a safe fallback title and bounds renderer-controlled titles", () => {
    expect(
      parseTaskWindowRequest({ path: "/acme/tasks/task-1", title: "  " }),
    ).toMatchObject({ title: "Task" });
    expect(
      parseTaskWindowRequest({
        path: "/acme/tasks/task-1",
        title: "x".repeat(400),
      })?.title,
    ).toHaveLength(256);
  });

  it("round-trips a validated request through Electron additionalArguments", () => {
    const argument = encodeTaskWindowArgument({
      path: "/acme/tasks/task-1",
      title: "SILI-1: Fix tabs",
    });

    expect(readDesktopWindowContext(["electron", argument])).toEqual({
      kind: "task",
      path: "/acme/tasks/task-1",
      title: "SILI-1: Fix tabs",
      workspaceSlug: "acme",
      taskId: "task-1",
    });
  });

  it("falls back to the main window for malformed launch arguments", () => {
    expect(
      readDesktopWindowContext(["electron", "--silieco-task-window=%7Bbad"]),
    ).toEqual({ kind: "main" });
  });
});
