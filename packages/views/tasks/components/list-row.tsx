"use client";

import { memo, type Ref } from "react";
import { useSortable, defaultAnimateLayoutChanges } from "@dnd-kit/sortable";
import type { AnimateLayoutChanges } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { AppLink } from "../../navigation";
import type { Task, Project,
  TaskProperty,
} from "@silieco/core/types";
import { formatDateOnly } from "@silieco/core/tasks/date";
import { ActorAvatar } from "../../common/actor-avatar";
import { PropertyIcon } from "../../common/property-icon";
import { useWorkspacePaths } from "@silieco/core/paths";
import { useQuery } from "@tanstack/react-query";
import { useViewStore } from "@silieco/core/tasks/stores/view-store-context";
import { useWorkspaceId } from "@silieco/core/hooks";
import { propertyListOptions } from "@silieco/core/properties";
import { CustomPropertyValueDisplay } from "./pickers/custom-property-picker";
import { ProjectIcon } from "../../projects/components/project-icon";
import { PriorityIcon } from "./priority-icon";
import { ProgressRing } from "./progress-ring";
import { TaskActionsContextMenu } from "../actions";
import { LabelChip } from "../../labels/label-chip";
import { TaskAgentActivityIndicator } from "./task-agent-activity-indicator";
import { useTaskSurfaceSelection } from "../surface/selection-context";

export interface ChildProgress {
  done: number;
  total: number;
}

function formatDate(date: string): string {
  return formatDateOnly(date, { month: "short", day: "numeric" }, "en-US");
}

function ListRowContent({
  task,
  childProgress,
  project,
  isDragging,
  containerRef,
  containerStyle,
  containerProps,
  checkboxProps,
}: {
  task: Task;
  childProgress?: ChildProgress;
  project?: Project;
  isDragging?: boolean;
  containerRef?: Ref<HTMLDivElement>;
  containerStyle?: React.CSSProperties;
  containerProps?: Record<string, unknown>;
  checkboxProps?: Pick<React.HTMLAttributes<HTMLDivElement>, "onClick" | "onMouseDown" | "onPointerDown">;
}) {
  const selection = useTaskSurfaceSelection();
  const selected = selection.selectedIds.has(task.id);
  const toggle = selection.toggle;
  const p = useWorkspacePaths();
  const storeProperties = useViewStore((s) => s.cardProperties);
  const cardPropertyIds = useViewStore((s) => s.cardPropertyIds);
  const rowWsId = useWorkspaceId();
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(rowWsId));
  const cardCustomProperties = cardPropertyIds
    .map((id) => workspaceProperties.find((p) => p.id === id))
    .filter((p): p is TaskProperty => !!p && task.properties?.[p.id] !== undefined);
  const labels = task.labels ?? [];

  const showProject = storeProperties.project && project;
  const showChildProgress = storeProperties.childProgress && childProgress;
  const showAssignee = storeProperties.assignee && task.assignee_type && task.assignee_id;
  const showStartDate = storeProperties.startDate && task.start_date;
  const showDueDate = storeProperties.dueDate && task.due_date;
  const showLabels = storeProperties.labels && labels.length > 0;

  return (
    <TaskActionsContextMenu task={task}>
      <div
        ref={containerRef}
        style={containerStyle}
        {...containerProps}
        className={`group/row flex h-9 items-center gap-2 px-4 text-body transition-colors ${
          selected
            ? "bg-surface-selected hover:not-data-[popup-open]:bg-surface-selected data-[popup-open]:bg-surface-selected"
            : "hover:not-data-[popup-open]:bg-surface-hover data-[popup-open]:bg-surface-hover"
        } ${isDragging ? "opacity-30" : ""}`}
      >
        <div
          className="relative flex shrink-0 items-center justify-center w-4 h-4"
          {...checkboxProps}
        >
          <PriorityIcon
            priority={task.priority}
            className={selected ? "hidden" : "group-hover/row:hidden"}
          />
          <input
            type="checkbox"
            checked={selected}
            onChange={() => toggle(task.id)}
            className={`absolute inset-0 cursor-pointer accent-primary ${
              selected ? "" : "hidden group-hover/row:block"
            }`}
          />
        </div>
        <AppLink
          href={p.taskDetail(task.id)}
          className={`flex flex-1 items-center gap-2 min-w-0 ${isDragging ? "pointer-events-none" : ""}`}
        >
          <span className="w-16 shrink-0 text-caption text-muted-foreground">
            {task.identifier}
          </span>
          <TaskAgentActivityIndicator taskId={task.id} />

          <span className="flex min-w-0 flex-1 items-center gap-1.5">
            <span className="truncate">{task.title}</span>
            {showChildProgress && (
              <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5">
                <ProgressRing done={childProgress!.done} total={childProgress!.total} size={14} />
                <span className="text-micro text-muted-foreground tabular-nums font-medium">
                  {childProgress!.done}/{childProgress!.total}
                </span>
              </span>
            )}
            {showLabels && (
              <span className="ml-1.5 hidden md:inline-flex shrink-0 items-center gap-1 max-w-[260px] overflow-hidden">
                {labels.slice(0, 3).map((label) => (
                  <LabelChip key={label.id} label={label} />
                ))}
                {labels.length > 3 && (
                  <span className="text-micro text-muted-foreground">
                    +{labels.length - 3}
                  </span>
                )}
              </span>
            )}
            {cardCustomProperties.length > 0 && (
              <span className="ml-1.5 hidden md:inline-flex shrink-0 items-center gap-1 max-w-[260px] overflow-hidden">
                {cardCustomProperties.slice(0, 3).map((property) => (
                  <span
                    key={property.id}
                    className="inline-flex max-w-[120px] items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground"
                  >
                    <PropertyIcon property={property} className="size-3 text-micro" />
                    <CustomPropertyValueDisplay property={property} value={task.properties?.[property.id]} />
                  </span>
                ))}
              </span>
            )}
          </span>
          {showProject && (
            <span className="inline-flex shrink-0 items-center gap-1 text-caption text-muted-foreground max-w-[140px]">
              <ProjectIcon project={project} size="sm" />
              <span className="truncate">{project!.title}</span>
            </span>
          )}
          {showStartDate && (
            <span className="shrink-0 text-caption text-muted-foreground">
              {formatDate(task.start_date!)}
            </span>
          )}
          {showDueDate && (
            <span className="shrink-0 text-caption text-muted-foreground">
              {formatDate(task.due_date!)}
            </span>
          )}
          {showAssignee && (
            <ActorAvatar
              actorType={task.assignee_type!}
              actorId={task.assignee_id!}
              size="sm"
              enableHoverCard
            />
          )}
        </AppLink>
      </div>
    </TaskActionsContextMenu>
  );
}

export const ListRow = memo(function ListRow({
  task,
  childProgress,
  project,
}: {
  task: Task;
  childProgress?: ChildProgress;
  project?: Project;
}) {
  return (
    <ListRowContent
      task={task}
      childProgress={childProgress}
      project={project}
    />
  );
});

const animateLayoutChanges: AnimateLayoutChanges = (args) => {
  const { isSorting, wasDragging } = args;
  if (isSorting || wasDragging) return false;
  return defaultAnimateLayoutChanges(args);
};

const stopDrag = (e: React.SyntheticEvent) => {
  e.stopPropagation();
};

export const DraggableListRow = memo(function DraggableListRow({
  task,
  childProgress,
  project,
  disableSorting,
}: {
  task: Task;
  childProgress?: ChildProgress;
  project?: Project;
  disableSorting?: boolean;
}) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id: task.id,
    data: { status: task.status },
    animateLayoutChanges,
    disabled: disableSorting ? { droppable: true } : undefined,
  });

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  return (
    <ListRowContent
      task={task}
      childProgress={childProgress}
      project={project}
      isDragging={isDragging}
      containerRef={setNodeRef}
      containerStyle={style}
      containerProps={{ ...attributes, ...listeners }}
      checkboxProps={{ onClick: stopDrag, onMouseDown: stopDrag, onPointerDown: stopDrag }}
    />
  );
});
