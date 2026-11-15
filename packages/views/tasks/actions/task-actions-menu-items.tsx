"use client";

import { agentRunsKeys } from "@silieco/core/agent-runs/queries";
import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  ArrowDown,
  ArrowUp,
  Calendar,
  CalendarClock,
  ExternalLink,
  FolderOpen,
  Link2,
  Network,
  Pin,
  PinOff,
  Plus,
  Trash2,
  Unlink,
  UserMinus,
} from "lucide-react";
import type { AgentRun, Task } from "@silieco/core/types";
import { todayDateOnly, addDaysDateOnly } from "@silieco/core/tasks/date";
import { api } from "@silieco/core/api";
import {
  ALL_STATUSES,
  PRIORITY_ORDER,
  PRIORITY_CONFIG,
} from "@silieco/core/tasks/config";
import { useWorkspaceId } from "@silieco/core/hooks";
import { StatusIcon } from "../components/status-icon";
import { PriorityIcon } from "../components/priority-icon";
import {
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
  DropdownMenuSeparator,
} from "@silieco/ui/components/ui/dropdown-menu";
import {
  ContextMenuItem,
  ContextMenuSub,
  ContextMenuSubTrigger,
  ContextMenuSubContent,
  ContextMenuSeparator,
} from "@silieco/ui/components/ui/context-menu";
import { copyText } from "@silieco/ui/lib/clipboard";
import type { UseTaskActionsResult } from "./use-task-actions";
import { useT } from "../../i18n";

// Both Dropdown and Context menu wrappers expose an API-compatible surface
// (variant, inset, onClick, etc.). We bundle the primitives we need into a
// single object so `TaskActionsMenuItems` can render the same JSX for both.
export interface MenuPrimitives {
  Item: typeof DropdownMenuItem;
  Sub: typeof DropdownMenuSub;
  SubTrigger: typeof DropdownMenuSubTrigger;
  SubContent: typeof DropdownMenuSubContent;
  Separator: typeof DropdownMenuSeparator;
}

export const dropdownPrimitives: MenuPrimitives = {
  Item: DropdownMenuItem,
  Sub: DropdownMenuSub,
  SubTrigger: DropdownMenuSubTrigger,
  SubContent: DropdownMenuSubContent,
  Separator: DropdownMenuSeparator,
};

// Context primitives are API-compatible with Dropdown primitives, but their
// TypeScript identities differ. Cast once here and call it a day — this is the
// single bridge between the two primitive sets.
export const contextPrimitives: MenuPrimitives = {
  Item: ContextMenuItem as unknown as typeof DropdownMenuItem,
  Sub: ContextMenuSub as unknown as typeof DropdownMenuSub,
  SubTrigger: ContextMenuSubTrigger as unknown as typeof DropdownMenuSubTrigger,
  SubContent: ContextMenuSubContent as unknown as typeof DropdownMenuSubContent,
  Separator: ContextMenuSeparator as unknown as typeof DropdownMenuSeparator,
};

interface TaskActionsMenuItemsProps {
  task: Task;
  actions: UseTaskActionsResult;
  primitives: MenuPrimitives;
  /** Called when the user clicks the Assignee menu item. The parent should
   *  close the surrounding menu and open the shared `AssigneePicker` popover.
   *  Decoupled this way so the same item can drive both the dropdown
   *  (3-dot button) and the context menu (right-click) wrappers. */
  onOpenAssignee: () => void;
  /** If set, leave the page after the task is deleted (used by the detail
   *  page, which renders the task being deleted). The delete modal goes back
   *  to the list the user came from and only falls back to this path when
   *  there is no in-app history. List surfaces leave it unset and stay put. */
  onDeletedFallbackPath?: string;
}

export function TaskActionsMenuItems({
  task,
  actions,
  primitives: P,
  onOpenAssignee,
  onDeletedFallbackPath,
}: TaskActionsMenuItemsProps) {
  const { t } = useT("tasks");
  const {
    isPinned,
    updateField,
    openInNewTab,
    togglePin,
    copyLink,
    openCreateSubTask,
    openSetParent,
    removeParent,
    openAddChild,
    openDeleteConfirm,
  } = actions;

  // Subscribe to the task's task list so the cache is warm by the time the
  // user clicks "Copy local workdir path". The query only fires while the
  // menu is open (Base UI portals the menu content lazily) — list views
  // that wrap every row in TaskActionsContextMenu pay nothing until the
  // menu actually opens.
  //
  // The query shares its key with ExecutionLogSection, so navigating from
  // the task detail page is a free cache hit.
  const wsId = useWorkspaceId();
  const { data: runs } = useQuery({
    queryKey: agentRunsKeys.forTask(wsId, task.id),
    queryFn: () => api.listRunsByTask(task.id),
    staleTime: 30_000,
  });

  // Synchronous click handler — the awaited fetch in the previous version
  // dropped the browser's transient user activation, which made
  // navigator.clipboard.writeText() reject from the menu when the cache
  // was cold. We now read straight from the cached query result and write
  // to the clipboard inside the same task as the click.
  const handleCopyWorkdirPath = useCallback(() => {
    const latestWorkDir = pickLatestWorkDir(runs);
    if (!latestWorkDir) {
      toast.error(t(($) => $.detail.workdir_path_unavailable));
      return;
    }
    void copyText(latestWorkDir).then((ok) => {
      if (ok) toast.success(t(($) => $.detail.workdir_path_copied));
      else toast.error(t(($) => $.detail.workdir_path_copy_failed));
    });
  }, [runs, t]);

  return (
    <>
      {/* Status */}
      <P.Sub>
        <P.SubTrigger>
          <StatusIcon status={task.status} className="h-3.5 w-3.5" />
          {t(($) => $.actions.status)}
        </P.SubTrigger>
        <P.SubContent>
          {ALL_STATUSES.map((s) => (
            <P.Item key={s} onClick={() => updateField({ status: s })}>
              <StatusIcon status={s} className="h-3.5 w-3.5" />
              {t(($) => $.status[s])}
              {task.status === s && (
                <span className="ml-auto text-caption text-muted-foreground">{"✓"}</span>
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Priority */}
      <P.Sub>
        <P.SubTrigger>
          <PriorityIcon priority={task.priority} />
          {t(($) => $.actions.priority)}
        </P.SubTrigger>
        <P.SubContent>
          {PRIORITY_ORDER.map((p) => (
            <P.Item key={p} onClick={() => updateField({ priority: p })}>
              <span
                className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-caption font-medium ${PRIORITY_CONFIG[p].badgeBg} ${PRIORITY_CONFIG[p].badgeText}`}
              >
                <PriorityIcon priority={p} className="h-3 w-3" inheritColor />
                {t(($) => $.priority[p])}
              </span>
              {task.priority === p && (
                <span className="ml-auto text-caption text-muted-foreground">{"✓"}</span>
              )}
            </P.Item>
          ))}
        </P.SubContent>
      </P.Sub>

      {/* Assignee — closes this menu and hands off to the shared
          AssigneePicker (members + agents + squads, with search and
          permission checks). Keeps a single source of truth for the
          assignee UX across detail sidebar, board cards, and right-click /
          3-dot menus. */}
      <P.Item onClick={onOpenAssignee}>
        <UserMinus className="h-3.5 w-3.5" />
        {t(($) => $.actions.assignee)}
      </P.Item>

      {/* Start date */}
      <P.Sub>
        <P.SubTrigger>
          <CalendarClock className="h-3.5 w-3.5" />
          {t(($) => $.actions.start_date)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={() => updateField({ start_date: todayDateOnly() })}>
            {t(($) => $.actions.start_today)}
          </P.Item>
          <P.Item onClick={() => updateField({ start_date: addDaysDateOnly(1) })}>
            {t(($) => $.actions.start_tomorrow)}
          </P.Item>
          <P.Item onClick={() => updateField({ start_date: addDaysDateOnly(7) })}>
            {t(($) => $.actions.start_next_week)}
          </P.Item>
          {task.start_date && (
            <>
              <P.Separator />
              <P.Item onClick={() => updateField({ start_date: null })}>
                {t(($) => $.actions.start_clear)}
              </P.Item>
            </>
          )}
        </P.SubContent>
      </P.Sub>

      {/* Due date */}
      <P.Sub>
        <P.SubTrigger>
          <Calendar className="h-3.5 w-3.5" />
          {t(($) => $.actions.due_date)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={() => updateField({ due_date: todayDateOnly() })}>
            {t(($) => $.actions.due_today)}
          </P.Item>
          <P.Item onClick={() => updateField({ due_date: addDaysDateOnly(1) })}>
            {t(($) => $.actions.due_tomorrow)}
          </P.Item>
          <P.Item onClick={() => updateField({ due_date: addDaysDateOnly(7) })}>
            {t(($) => $.actions.due_next_week)}
          </P.Item>
          {task.due_date && (
            <>
              <P.Separator />
              <P.Item onClick={() => updateField({ due_date: null })}>
                {t(($) => $.actions.due_clear)}
              </P.Item>
            </>
          )}
        </P.SubContent>
      </P.Sub>

      <P.Separator />

      {/* Leads the "do something with this task itself" group: the only
          discoverable way to open an task elsewhere for users who don't know
          modifier-click, so it sits above the copy actions. */}
      <P.Item onClick={openInNewTab}>
        <ExternalLink className="h-3.5 w-3.5" />
        {t(($) => $.actions.open_in_new_tab)}
      </P.Item>
      <P.Item onClick={togglePin}>
        {isPinned ? (
          <PinOff className="h-3.5 w-3.5" />
        ) : (
          <Pin className="h-3.5 w-3.5" />
        )}
        {isPinned ? t(($) => $.actions.unpin_from_sidebar) : t(($) => $.actions.pin_to_sidebar)}
      </P.Item>
      <P.Item onClick={copyLink}>
        <Link2 className="h-3.5 w-3.5" />
        {t(($) => $.actions.copy_link)}
      </P.Item>
      <P.Item onClick={handleCopyWorkdirPath}>
        <FolderOpen className="h-3.5 w-3.5" />
        {t(($) => $.actions.copy_workdir_path)}
      </P.Item>

      <P.Separator />

      {/* Relationship actions live under "Relations" — a semantically explicit
          label (unlike the old "More") so the first level tells you what the
          submenu does. Holds parent/sub-task links today, and will grow
          (blocks, duplicates, related) as we add more relation types. */}
      <P.Sub>
        <P.SubTrigger>
          <Network className="h-3.5 w-3.5" />
          {t(($) => $.actions.relations)}
        </P.SubTrigger>
        <P.SubContent>
          <P.Item onClick={openCreateSubTask}>
            <Plus className="h-3.5 w-3.5" />
            {t(($) => $.actions.create_sub_task)}
          </P.Item>
          <P.Item onClick={openSetParent}>
            <ArrowUp className="h-3.5 w-3.5" />
            {t(($) => $.actions.set_parent_task)}
          </P.Item>
          {task.parent_task_id && (
            <P.Item onClick={removeParent}>
              <Unlink className="h-3.5 w-3.5" />
              {t(($) => $.actions.remove_parent_task)}
            </P.Item>
          )}
          <P.Item onClick={openAddChild}>
            <ArrowDown className="h-3.5 w-3.5" />
            {t(($) => $.actions.add_sub_task)}
          </P.Item>
        </P.SubContent>
      </P.Sub>

      <P.Separator />

      <P.Item
        variant="destructive"
        onClick={() => openDeleteConfirm({ onDeletedFallbackPath })}
      >
        <Trash2 className="h-3.5 w-3.5" />
        {t(($) => $.actions.delete_task)}
      </P.Item>
    </>
  );
}

function pickLatestWorkDir(runs: AgentRun[] | undefined): string | undefined {
  if (!runs?.length) return undefined;
  let latest: AgentRun | undefined;
  for (const run of runs) {
    if (!run.work_dir) continue;
    if (!latest || run.created_at > latest.created_at) {
      latest = run;
    }
  }
  return latest?.work_dir;
}
