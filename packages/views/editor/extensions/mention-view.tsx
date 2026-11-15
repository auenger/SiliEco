"use client";

/**
 * MentionView — NodeView for rendering @mentions inline in the editor.
 *
 * Member/agent mentions: plain "@Name" text with .mention class styling.
 * Task mentions: TaskChip inside a custom <a> that supports cmd/shift-click
 * to open in a new tab (AppLink doesn't expose that intent hook).
 *
 * Task chip sizing: must fit within the paragraph line box (14px * 1.625 =
 * 22.75px). Card is text-caption (12px) + py-0.5 + border ≈ 22px total. The
 * `vertical-align: middle` rule on `[data-node-view-wrapper]` in CSS handles
 * line-box alignment; setting it on the inner <a> has no effect because the
 * wrapper is the outermost inline element.
 */

import { NodeViewWrapper } from "@tiptap/react";
import type { NodeViewProps } from "@tiptap/react";
import { useWorkspacePaths } from "@silieco/core/paths";
import { useTaskLinkStore } from "@silieco/core/tasks/stores";
import { useNavigation } from "../../navigation";
import { TaskChip } from "../../tasks/components/task-chip";
import { ProjectChip } from "../../projects/components/project-chip";

export function MentionView({ node }: NodeViewProps) {
  const { type, id, label } = node.attrs;

  if (type === "task") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <TaskMention taskId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  if (type === "project") {
    return (
      <NodeViewWrapper as="span" className="inline">
        <ProjectMention projectId={id} fallbackLabel={label} />
      </NodeViewWrapper>
    );
  }

  return (
    <NodeViewWrapper as="span" className="inline">
      <span className="mention">@{label ?? id}</span>
    </NodeViewWrapper>
  );
}

function ProjectMention({
  projectId,
  fallbackLabel,
}: {
  projectId: string;
  fallbackLabel?: string;
}) {
  const p = useWorkspacePaths();
  const { push, openInNewTab } = useNavigation();
  const projectPath = p.projectDetail(projectId);

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) {
        e.preventDefault();
        openInNewTab(projectPath, fallbackLabel);
      }
      // Web: no adapter — leave the event alone so the browser's native
      // modifier-click on the anchor opens the tab (or window for shift),
      // preserving background/foreground semantics window.open would flatten.
      return;
    }
    e.preventDefault();
    push(projectPath);
  };

  return (
    <a href={projectPath} onClick={handleClick} className="project-mention">
      <ProjectChip
        projectId={projectId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
  );
}

function TaskMention({
  taskId,
  fallbackLabel,
}: {
  taskId: string;
  fallbackLabel?: string;
}) {
  const p = useWorkspacePaths();
  const { push, openInNewTab } = useNavigation();
  const newTabPreferred = useTaskLinkStore((s) => s.openInNewTab);
  const taskPath = p.taskDetail(taskId);

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) {
        e.preventDefault();
        openInNewTab(taskPath, fallbackLabel);
      }
      // Web: no adapter — native modifier-click on the anchor opens a tab.
      return;
    }
    if (newTabPreferred) {
      if (openInNewTab) {
        e.preventDefault();
        openInNewTab(taskPath, fallbackLabel, { activate: true });
      }
      // Web: native target="_blank" opens a browser tab.
      return;
    }
    e.preventDefault();
    push(taskPath);
  };

  return (
    <a
      href={taskPath}
      target={newTabPreferred ? "_blank" : undefined}
      rel={newTabPreferred ? "noopener noreferrer" : undefined}
      onClick={handleClick}
      className="task-mention"
    >
      <TaskChip
        taskId={taskId}
        fallbackLabel={fallbackLabel}
        className="cursor-pointer hover:bg-accent transition-colors"
      />
    </a>
  );
}
