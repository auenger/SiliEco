"use client";

import {
  cloneElement,
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from "react";
import type { Task } from "@silieco/core/types";
import {
  ContextMenu,
  ContextMenuContent,
} from "@silieco/ui/components/ui/context-menu";
import { useTaskActions } from "./use-task-actions";
import {
  TaskActionsMenuItems,
  contextPrimitives,
} from "./task-actions-menu-items";
import { AssigneePicker } from "../components/pickers";

/**
 * One shared context menu per surface instead of one Base UI ContextMenu
 * root per card/row. A board remount used to create N × (MenuRoot + Trigger)
 * — the single largest slice of the tab-switch freeze — even though at most
 * one menu is ever open. Items delegate: the per-item wrapper only attaches
 * an `onContextMenu` that reports (task, cursor position) up to the
 * provider's singleton, which anchors the menu at the cursor via a virtual
 * anchor element.
 *
 * Known debt: Base UI's ContextMenuTrigger also opened on touch long-press;
 * the delegated `contextmenu` event covers right-click (and Android
 * long-press) but not iOS Safari long-press.
 */

interface ActiveMenu {
  task: Task;
  position: { x: number; y: number };
}

type OpenTaskContextMenu = (task: Task, event: React.MouseEvent) => void;

const TaskContextMenuContext = createContext<OpenTaskContextMenu | null>(
  null,
);

export function TaskContextMenuProvider({
  children,
}: {
  children: ReactNode;
}) {
  const [active, setActive] = useState<ActiveMenu | null>(null);
  const [open, setOpen] = useState(false);
  // The element the user right-clicked. Base UI's per-item trigger used to
  // stamp `data-popup-open` on it (rows/cards style their "menu is open for
  // me" state off that attribute). React never renders this attribute, so
  // managing it imperatively here keeps the affordance without re-rendering
  // every row on open/close.
  const triggerElRef = useRef<HTMLElement | null>(null);

  const openMenu = useCallback<OpenTaskContextMenu>((task, event) => {
    event.preventDefault();
    triggerElRef.current?.removeAttribute("data-popup-open");
    const el = event.currentTarget as HTMLElement;
    el.setAttribute("data-popup-open", "");
    triggerElRef.current = el;
    setActive({ task, position: { x: event.clientX, y: event.clientY } });
    setOpen(true);
  }, []);

  const handleOpenChange = useCallback((v: boolean) => {
    if (!v) {
      triggerElRef.current?.removeAttribute("data-popup-open");
      triggerElRef.current = null;
    }
    setOpen(v);
  }, []);

  return (
    <TaskContextMenuContext.Provider value={openMenu}>
      {children}
      {/* Mounted on first use, kept mounted after (one closed menu root per
          surface — the popup itself unmounts while closed). */}
      {active && (
        <TaskContextMenuSingleton
          task={active.task}
          position={active.position}
          open={open}
          onOpenChange={handleOpenChange}
        />
      )}
    </TaskContextMenuContext.Provider>
  );
}

function TaskContextMenuSingleton({
  task,
  position,
  open,
  onOpenChange,
}: ActiveMenu & {
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const actions = useTaskActions(task);
  const [assigneeOpen, setAssigneeOpen] = useState(false);

  // Point-sized virtual anchor at the right-click position — replaces the
  // cursor anchor Base UI's own trigger would have registered.
  const anchor = useMemo(
    () => ({
      getBoundingClientRect: () =>
        DOMRect.fromRect({
          x: position.x,
          y: position.y,
          width: 0,
          height: 0,
        }),
    }),
    [position.x, position.y],
  );

  return (
    <>
      <ContextMenu open={open} onOpenChange={(v) => onOpenChange(v)}>
        <ContextMenuContent anchor={anchor}>
          <TaskActionsMenuItems
            task={task}
            actions={actions}
            primitives={contextPrimitives}
            onOpenAssignee={() => setAssigneeOpen(true)}
          />
        </ContextMenuContent>
      </ContextMenu>
      {/* Mount the picker only once the user actually opens it, anchored at
          the right-click position so it opens where the context menu just
          was instead of jumping to the row's top-left corner. */}
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
              className="pointer-events-none fixed"
              style={{
                left: position.x,
                top: position.y,
                width: 0,
                height: 0,
              }}
            />
          }
          trigger={<span />}
          align="start"
        />
      )}
    </>
  );
}

interface TaskActionsContextMenuProps {
  task: Task;
  /** A single React element the menu trigger behavior is grafted onto. */
  children: ReactElement<{
    onContextMenu?: (e: React.MouseEvent) => void;
    className?: string;
  }>;
}

/**
 * Per-item wrapper: clones its child with an `onContextMenu` that opens the
 * surface-level singleton menu for this task. Costs one context read — no
 * menu machinery per item. Requires {@link TaskContextMenuProvider} above
 * (TaskSurface mounts it).
 */
export function TaskActionsContextMenu({
  task,
  children,
}: TaskActionsContextMenuProps) {
  const openMenu = useContext(TaskContextMenuContext);
  if (!openMenu) {
    throw new Error(
      "TaskActionsContextMenu requires an TaskContextMenuProvider ancestor",
    );
  }
  const childOnContextMenu = children.props.onContextMenu;
  const childClassName = children.props.className;
  return cloneElement(children, {
    // Base UI's trigger merged `select-none` into the item; keep that so
    // right-click targets don't become text-selectable.
    className: childClassName ? `${childClassName} select-none` : "select-none",
    onContextMenu: (e: React.MouseEvent) => {
      childOnContextMenu?.(e);
      openMenu(task, e);
    },
  });
}
