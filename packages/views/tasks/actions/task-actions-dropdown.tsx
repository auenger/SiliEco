"use client";

import { useState, type ReactElement } from "react";
import type { Task } from "@silieco/core/types";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
} from "@silieco/ui/components/ui/dropdown-menu";
import { useTaskActions } from "./use-task-actions";
import {
  TaskActionsMenuItems,
  dropdownPrimitives,
} from "./task-actions-menu-items";
import { AssigneePicker } from "../components/pickers";

interface TaskActionsDropdownProps {
  task: Task;
  /** A single React element cloned by Base UI as the trigger (via `render` prop). */
  trigger: ReactElement;
  align?: "start" | "end" | "center";
  /** If set, leave the page after the task is deleted — back to wherever the
   *  user came from, or to this path when there is no in-app history. */
  onDeletedFallbackPath?: string;
}

export function TaskActionsDropdown({
  task,
  trigger,
  align = "end",
  onDeletedFallbackPath,
}: TaskActionsDropdownProps) {
  const actions = useTaskActions(task);
  const [assigneeOpen, setAssigneeOpen] = useState(false);

  // The outer `relative inline-flex` is the picker's anchor box: the
  // absolute, pointer-events-none span inside `triggerRender` fills it, so
  // the popover positions itself relative to the dropdown's 3-dot button
  // without us having to thread a ref through Base UI's anchor API.
  return (
    <span className="relative inline-flex">
      <DropdownMenu>
        <DropdownMenuTrigger render={trigger} />
        <DropdownMenuContent align={align} className="w-auto">
          <TaskActionsMenuItems
            task={task}
            actions={actions}
            primitives={dropdownPrimitives}
            onOpenAssignee={() => setAssigneeOpen(true)}
            onDeletedFallbackPath={onDeletedFallbackPath}
          />
        </DropdownMenuContent>
      </DropdownMenu>
      {/* Mount the picker only once the user actually opens it. Otherwise
          every row in a list/board would subscribe to members/agents/squads
          /frequency queries on mount, multiplying memory + render cost. */}
      {assigneeOpen && (
        <AssigneePicker
          assigneeType={task.assignee_type}
          assigneeId={task.assignee_id}
          onUpdate={actions.updateField}
          open={assigneeOpen}
          onOpenChange={setAssigneeOpen}
          triggerRender={
            <span
              aria-hidden
              className="pointer-events-none absolute inset-0"
            />
          }
          trigger={<span />}
          align={align}
        />
      )}
    </span>
  );
}
