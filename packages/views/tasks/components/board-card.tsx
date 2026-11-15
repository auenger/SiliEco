"use client";

import { useCallback, memo } from "react";
import { AppLink } from "../../navigation";
import { useSortable, defaultAnimateLayoutChanges } from "@dnd-kit/sortable";
import type { AnimateLayoutChanges } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import type { Task, TaskProperty, Project, UpdateTaskRequest } from "@silieco/core/types";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@silieco/core/hooks";
import { propertyListOptions } from "@silieco/core/properties";
import { CustomPropertyValueDisplay } from "./pickers/custom-property-picker";
import { formatDateOnly, isPastDateOnly } from "@silieco/core/tasks/date";
import { CalendarClock, CalendarDays } from "lucide-react";
import { ActorAvatar } from "../../common/actor-avatar";
import { PropertyIcon } from "../../common/property-icon";
import { useWorkspacePaths } from "@silieco/core/paths";
import { useActorName } from "@silieco/core/workspace/hooks";
import { useTimeAgo } from "../../i18n";
import { ProjectIcon } from "../../projects/components/project-icon";
import { PriorityIcon } from "./priority-icon";
import { PriorityPicker, AssigneePicker, StartDatePicker, DueDatePicker } from "./pickers";
import { useViewStore } from "@silieco/core/tasks/stores/view-store-context";
import { ProgressRing } from "./progress-ring";
import type { ChildProgress } from "./list-row";
import { TaskActionsContextMenu } from "../actions";
import { LabelChip } from "../../labels/label-chip";
import { TaskAgentActivityIndicator } from "./task-agent-activity-indicator";
import { useTaskSurfaceActionsOptional } from "../surface/actions-context";
import { useT } from "../../i18n";

function formatDate(date: string): string {
  return formatDateOnly(date, { month: "short", day: "numeric" }, "en-US");
}

function descriptionPreview(markdown: string): string {
  return markdown
    .replace(/!file\[[^\]]*\]\([^)]*\)/g, "")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, "")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/[*_`~]+/g, "")
    .replace(/^[\s>#]+/gm, "")
    .replace(/\s+/g, " ")
    .trim();
}

/** Stops event from bubbling to Link/drag handlers */
function PickerWrapper({ children, className }: { children: React.ReactNode; className?: string }) {
  const stop = (e: React.SyntheticEvent) => {
    e.stopPropagation();
    e.preventDefault();
  };
  return (
    <div onClick={stop} onMouseDown={stop} onPointerDown={stop} className={className}>
      {children}
    </div>
  );
}

export const BoardCardContent = memo(function BoardCardContent({
  task,
  editable = false,
  childProgress,
  project,
}: {
  task: Task;
  editable?: boolean;
  childProgress?: ChildProgress;
  project?: Project;
}) {
  const { t } = useT("tasks");
  const timeAgo = useTimeAgo();
  const storeProperties = useViewStore((s) => s.cardProperties);
  const cardPropertyIds = useViewStore((s) => s.cardPropertyIds);
  const cardWsId = useWorkspaceId();
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(cardWsId));
  // Custom properties toggled on in Display options, in toggle order, only
  // when this task actually carries a value.
  const cardCustomProperties = cardPropertyIds
    .map((id) => workspaceProperties.find((p) => p.id === id))
    .filter((p): p is TaskProperty => !!p && task.properties?.[p.id] !== undefined);
  const labels = task.labels ?? [];

  const surfaceActions = useTaskSurfaceActionsOptional();
  const handleUpdate = useCallback(
    (updates: Partial<UpdateTaskRequest>) => {
      surfaceActions?.updateTask(task.id, updates, {
        errorMessage: t(($) => $.card.update_failed),
      });
    },
    [task.id, surfaceActions, t],
  );
  const canEdit = editable && !!surfaceActions;

  const showPriority = storeProperties.priority;
  const showDescription = storeProperties.description && task.description;
  const showAssigneeSection = storeProperties.assignee;
  const hasAssignee = !!task.assignee_type && !!task.assignee_id;
  const showStartDate = storeProperties.startDate && task.start_date;
  const showDueDate = storeProperties.dueDate && task.due_date;
  const showProject = storeProperties.project && project;
  const showChildProgress = storeProperties.childProgress && childProgress;
  const showLabels = storeProperties.labels && labels.length > 0;

  const showAssigneeName = showAssigneeSection && hasAssignee && !showStartDate && !showDueDate;
  const showUpdatedHint = showAssigneeName && !showChildProgress;
  const { getActorName } = useActorName();
  const assigneeName =
    showAssigneeName && task.assignee_type && task.assignee_id
      ? getActorName(task.assignee_type, task.assignee_id)
      : null;

  const priorityLabel = t(($) => $.priority[task.priority]);
  const priorityIconNode = showPriority ? (
    canEdit ? (
      <PickerWrapper>
        <PriorityPicker
          priority={task.priority}
          onUpdate={handleUpdate}
          triggerRender={
            <button
              type="button"
              aria-label={priorityLabel}
              className="inline-flex items-center justify-center rounded hover:bg-muted/60"
            >
              <PriorityIcon priority={task.priority} />
            </button>
          }
        />
      </PickerWrapper>
    ) : (
      <span aria-label={priorityLabel} className="inline-flex items-center justify-center">
        <PriorityIcon priority={task.priority} />
      </span>
    )
  ) : null;

  // The parent row gives this container the leftover space; min-w-0 and
  // max-w-full make the nested picker trigger respect that limit.
  const assigneeContainerClass = assigneeName
    ? "flex min-w-0 max-w-full items-center"
    : "inline-flex items-center";

  const assigneeInner = hasAssignee ? (
    <span className="flex min-w-0 max-w-full items-center gap-1.5">
      <ActorAvatar
        actorType={task.assignee_type!}
        actorId={task.assignee_id!}
        size="sm"
        enableHoverCard
        className="shrink-0"
      />
      {assigneeName && (
        <span className="min-w-0 truncate text-caption text-foreground">{assigneeName}</span>
      )}
    </span>
  ) : (
    <span className="text-caption text-muted-foreground">{t(($) => $.pickers.assignee.trigger_unassigned)}</span>
  );

  const assigneeNode = showAssigneeSection ? (
    canEdit ? (
      <PickerWrapper className={assigneeContainerClass}>
        <AssigneePicker
          assigneeType={task.assignee_type}
          assigneeId={task.assignee_id}
          onUpdate={handleUpdate}
          trigger={assigneeInner}
        />
      </PickerWrapper>
    ) : (
      <span className={assigneeContainerClass}>{assigneeInner}</span>
    )
  ) : null;

  const showMetaRow = showAssigneeSection || showStartDate || showDueDate || showChildProgress;
  const showRightMeta = !!showStartDate || !!showDueDate || !!showChildProgress || showUpdatedHint;

  return (
    <div className="rounded-lg border-[0.5px] border-surface-border bg-surface py-3 px-2.5 shadow-[var(--surface-shadow)] transition-colors group-hover/card:border-foreground/15 group-hover/card:bg-surface-hover group-data-[popup-open]/card:border-foreground/15 group-data-[popup-open]/card:bg-surface-hover">
      {/* Row 1: priority + identifier (left), agent activity + assignee (right) */}
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5 min-w-0">
          {priorityIconNode}
          <p className="text-caption text-muted-foreground truncate">{task.identifier}</p>
        </div>
        <TaskAgentActivityIndicator taskId={task.id} />
      </div>

      {/* Row 2: Title */}
      <p className="mt-1 text-body font-medium leading-snug line-clamp-2">
        {task.title}
      </p>

      {showDescription && (() => {
        const preview = descriptionPreview(task.description!);
        if (!preview) return null;
        return (
          <p className="mt-1 text-caption text-muted-foreground line-clamp-1">
            {preview}
          </p>
        );
      })()}

      {/* Chip row: project + labels + custom property values */}
      {(showProject || showLabels || cardCustomProperties.length > 0) && (
        <div className="mt-1.5 flex items-center gap-1.5 flex-wrap">
          {showProject && (
            <span className="inline-flex items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground max-w-[160px]">
              <ProjectIcon project={project} size="sm" />
              <span className="truncate">{project!.title}</span>
            </span>
          )}
          {showLabels && labels.map((label) => (
            <LabelChip key={label.id} label={label} />
          ))}
          {cardCustomProperties.map((property) => (
            <span
              key={property.id}
              className="inline-flex max-w-[160px] items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground"
            >
              <PropertyIcon property={property} className="size-3 text-micro" />
              <CustomPropertyValueDisplay property={property} value={task.properties?.[property.id]} />
            </span>
          ))}
        </div>
      )}

      {/* Meta row: assignee (left), start date, due date, child progress (right) */}
      {showMetaRow && (
        <div className="mt-2 flex items-center justify-between gap-2">
          {showAssigneeSection && (
            <div className="min-w-0 flex-1">
              {assigneeNode}
            </div>
          )}
          {showRightMeta && (
            <div className="ml-auto flex shrink-0 items-center gap-2">
              {showStartDate && (
                canEdit ? (
                  <PickerWrapper className="shrink-0">
                    <StartDatePicker
                      startDate={task.start_date}
                      onUpdate={handleUpdate}
                      trigger={
                        <span className="flex items-center gap-1 text-caption text-muted-foreground">
                          <CalendarClock className="size-3" />
                          {formatDate(task.start_date!)}
                        </span>
                      }
                    />
                  </PickerWrapper>
                ) : (
                  <span className="flex shrink-0 items-center gap-1 text-caption text-muted-foreground">
                    <CalendarClock className="size-3" />
                    {formatDate(task.start_date!)}
                  </span>
                )
              )}
              {showDueDate && (
                canEdit ? (
                  <PickerWrapper className="shrink-0">
                    <DueDatePicker
                      dueDate={task.due_date}
                      onUpdate={handleUpdate}
                      trigger={
                        <span
                          className={`flex items-center gap-1 text-caption ${
                            isPastDateOnly(task.due_date)
                              ? "text-destructive"
                              : "text-muted-foreground"
                          }`}
                        >
                          <CalendarDays className="size-3" />
                          {formatDate(task.due_date!)}
                        </span>
                      }
                    />
                  </PickerWrapper>
                ) : (
                  <span
                    className={`flex shrink-0 items-center gap-1 text-caption ${
                      isPastDateOnly(task.due_date)
                        ? "text-destructive"
                        : "text-muted-foreground"
                    }`}
                  >
                    <CalendarDays className="size-3" />
                    {formatDate(task.due_date!)}
                  </span>
                )
              )}
              {showChildProgress && (
                <div className="inline-flex shrink-0 items-center gap-1">
                  <ProgressRing done={childProgress!.done} total={childProgress!.total} size={14} />
                  <span className="text-micro text-muted-foreground tabular-nums font-medium">
                    {childProgress!.done}/{childProgress!.total}
                  </span>
                </div>
              )}
              {showUpdatedHint && (
                <span className="shrink-0 text-caption text-muted-foreground">
                  {t(($) => $.card.updated_ago, { time: timeAgo(task.updated_at) })}
                </span>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
});

const animateLayoutChanges: AnimateLayoutChanges = (args) => {
  const { isSorting, wasDragging } = args;
  if (isSorting || wasDragging) return false;
  return defaultAnimateLayoutChanges(args);
};

export const DraggableBoardCard = memo(function DraggableBoardCard({
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
  const p = useWorkspacePaths();
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
    <TaskActionsContextMenu task={task}>
      <div
        ref={setNodeRef}
        style={style}
        {...attributes}
        {...listeners}
        className={`group/card ${isDragging ? "opacity-30" : ""}`}
      >
        <AppLink
          href={p.taskDetail(task.id)}
          className={`group block transition-colors ${isDragging ? "pointer-events-none" : ""}`}
        >
          <BoardCardContent
            task={task}
            editable
            childProgress={childProgress}
            project={project}
          />
        </AppLink>
      </div>
    </TaskActionsContextMenu>
  );
});
