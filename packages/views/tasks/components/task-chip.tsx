"use client";

import { useQuery } from "@tanstack/react-query";
import { taskListOptions, taskDetailOptions } from "@silieco/core/tasks/queries";
import { useWorkspaceId } from "@silieco/core/hooks";
import { StatusIcon } from "./status-icon";

/**
 * Compact, presentation-only representation of an task —
 * `<StatusIcon> <identifier> <title>`, bordered, capped at the container width
 * (`max-w-full`) with the title truncating to an ellipsis. As an atomic inline
 * box it wraps to the next line as a unit when it doesn't fit at the current
 * position; the ellipsis only kicks in once a whole line can't hold it. The cap
 * lives here (single source of truth) — wrappers must NOT add their own flex
 * container around it, or a percentage cap gets dropped during the wrapper's
 * intrinsic sizing and the clickable box diverges from the truncated chip.
 *
 * This is the single source of truth for the "task-mention card" look.
 * It is intentionally **not** a link or button: callers wrap it in whatever
 * interactive shell they need (AppLink for markdown mentions, an <a> with
 * cmd-click support inside the editor's NodeView, a plain span next to a
 * dismiss button in chat's context anchor card, …).
 *
 * Size budget: must fit within a 14px line-box when used inline — hence
 * `py-0.5` + text-caption (see MentionView docstring for the math).
 */
export interface TaskChipProps {
  taskId: string;
  /** Shown when the task can't be resolved (deleted, other workspace, …). */
  fallbackLabel?: string;
  /** Extra classes — callers layer interaction hints here
   *  (e.g. `hover:bg-accent cursor-pointer` for navigable variants). */
  className?: string;
}

const BASE_CLASS =
  "task-mention inline-flex min-w-0 max-w-full items-center gap-1.5 rounded-md border mx-0.5 px-2 py-0.5 text-caption";

export function TaskChip({ taskId, fallbackLabel, className }: TaskChipProps) {
  const wsId = useWorkspaceId();
  const { data: tasks = [] } = useQuery(taskListOptions(wsId));
  const listTask = tasks.find((i) => i.id === taskId);

  // Fallback fetch for tasks outside the first page of the list (e.g. Done).
  const { data: detailTask } = useQuery({
    ...taskDetailOptions(wsId, taskId),
    enabled: !listTask,
  });

  const task = listTask ?? detailTask;
  const cls = className ? `${BASE_CLASS} ${className}` : BASE_CLASS;

  if (!task) {
    return (
      <span className={cls}>
        <span className="min-w-0 truncate font-medium text-muted-foreground">
          {fallbackLabel ?? taskId.slice(0, 8)}
        </span>
      </span>
    );
  }

  return (
    <span className={cls}>
      <StatusIcon status={task.status} className="h-3.5 w-3.5 shrink-0" />
      <span className="font-medium text-muted-foreground shrink-0">
        {task.identifier}
      </span>
      <span className="min-w-0 truncate text-foreground">{task.title}</span>
    </span>
  );
}
