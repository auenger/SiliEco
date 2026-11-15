export const TASK_WINDOW_ARGUMENT_PREFIX = "--silieco-task-window=";

const MAX_TASK_WINDOW_PATH_LENGTH = 2_048;
const MAX_TASK_WINDOW_TITLE_LENGTH = 256;
const WORKSPACE_SLUG_PATTERN = /^[a-z0-9][a-z0-9-]{0,62}$/;
const TASK_REF_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/;

export type TaskWindowRequest = {
  path: string;
  title: string;
};

export type TaskWindowContext = TaskWindowRequest & {
  kind: "task";
  workspaceSlug: string;
  taskId: string;
};

export type DesktopWindowContext =
  | { kind: "main" }
  | TaskWindowContext;

/**
 * Validate renderer-controlled input before it can become a BrowserWindow
 * launch argument. Dedicated windows are intentionally limited to one
 * workspace-scoped task-detail route; list pages and every other app route
 * stay in the main tabbed window.
 */
export function parseTaskWindowRequest(value: unknown): TaskWindowContext | null {
  if (!value || typeof value !== "object") return null;

  const input = value as Record<string, unknown>;
  if (typeof input.path !== "string") return null;
  if (input.path.length === 0 || input.path.length > MAX_TASK_WINDOW_PATH_LENGTH) {
    return null;
  }

  const parsedPath = parseTaskWindowPath(input.path);
  if (!parsedPath) return null;

  const rawTitle = typeof input.title === "string" ? input.title.trim() : "";
  const title = rawTitle.slice(0, MAX_TASK_WINDOW_TITLE_LENGTH) || "Task";

  return {
    kind: "task",
    path: parsedPath.path,
    title,
    workspaceSlug: parsedPath.workspaceSlug,
    taskId: parsedPath.taskId,
  };
}

/** Return a canonical task-detail path, or null for every other URL shape. */
export function parseTaskWindowPath(
  value: unknown,
): Pick<TaskWindowContext, "path" | "workspaceSlug" | "taskId"> | null {
  if (typeof value !== "string") return null;
  if (!value.startsWith("/") || value.startsWith("//")) return null;

  let url: URL;
  try {
    url = new URL(value, "https://desktop.silieco.invalid");
  } catch {
    return null;
  }
  if (url.origin !== "https://desktop.silieco.invalid") return null;

  const segments = url.pathname.split("/");
  if (
    segments.length !== 4 ||
    segments[0] !== "" ||
    (segments[2] !== "tasks" && segments[2] !== "tasks")
  ) {
    return null;
  }

  let workspaceSlug: string;
  let taskId: string;
  try {
    workspaceSlug = decodeURIComponent(segments[1] ?? "");
    taskId = decodeURIComponent(segments[3] ?? "");
  } catch {
    return null;
  }

  if (!WORKSPACE_SLUG_PATTERN.test(workspaceSlug)) return null;
  if (!TASK_REF_PATTERN.test(taskId)) return null;

  const path = `/${workspaceSlug}/tasks/${encodeURIComponent(taskId)}${url.search}${url.hash}`;
  if (path.length > MAX_TASK_WINDOW_PATH_LENGTH) return null;

  return { path, workspaceSlug, taskId };
}

export function encodeTaskWindowArgument(request: TaskWindowRequest): string {
  const context = parseTaskWindowRequest(request);
  if (!context) {
    throw new Error("Invalid task window request");
  }
  return `${TASK_WINDOW_ARGUMENT_PREFIX}${encodeURIComponent(
    JSON.stringify({ path: context.path, title: context.title }),
  )}`;
}

export function readDesktopWindowContext(
  argv: readonly string[],
): DesktopWindowContext {
  const argument = argv.find((item) =>
    item.startsWith(TASK_WINDOW_ARGUMENT_PREFIX),
  );
  if (!argument) return { kind: "main" };

  try {
    const encoded = argument.slice(TASK_WINDOW_ARGUMENT_PREFIX.length);
    const decoded = JSON.parse(decodeURIComponent(encoded)) as unknown;
    return parseTaskWindowRequest(decoded) ?? { kind: "main" };
  } catch {
    return { kind: "main" };
  }
}
