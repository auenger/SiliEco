"use client";

import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@silieco/core/paths";
import { useTaskLinkStore } from "@silieco/core/tasks/stores";
import { TaskChip } from "./task-chip";

interface TaskMentionCardProps {
  taskId: string;
  /** Fallback text when task is not in store (e.g. "SILI-7") */
  fallbackLabel?: string;
}

/**
 * Navigable chip — wraps TaskChip in an AppLink pointing at the task's
 * detail page. Hover/cursor affordance is layered onto the chip itself so
 * the visual target matches the clickable target.
 *
 * Plain click honors the "open task links in new tab" preference
 * (Settings → Preferences): new browser tab on web, new app tab on desktop.
 */
export function TaskMentionCard({ taskId, fallbackLabel }: TaskMentionCardProps) {
  const p = useWorkspacePaths();
  const openInNewTab = useTaskLinkStore((s) => s.openInNewTab);
  return (
    <AppLink
      href={p.taskDetail(taskId)}
      target={openInNewTab ? "_blank" : undefined}
      newTabTitle={fallbackLabel}
      className="task-mention not-prose align-middle"
    >
      <TaskChip
        taskId={taskId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </AppLink>
  );
}
