"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type FormEvent,
} from "react";
import {
  DndContext,
  PointerSensor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import { CSS } from "@dnd-kit/utilities";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  CalendarClock,
  CalendarDays,
  ChevronRight,
  CircleDot,
  Filter,
  GitBranch,
  GripVertical,
  Plus,
  Route,
  ShieldCheck,
  Sparkles,
  Users,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@silieco/core/api";
import { useWorkspaceId } from "@silieco/core/hooks";
import { formatDateOnly, isPastDateOnly } from "@silieco/core/issues/date";
import { issueKeys } from "@silieco/core/issues/queries";
import { useWorkspacePaths } from "@silieco/core/paths";
import { useActorName } from "@silieco/core/workspace/hooks";
import type {
  Issue,
  IssueStatus,
  Workflow,
  WorkflowGateType,
  WorkflowInstance,
  WorkflowStage,
} from "@silieco/core/types";
import {
  useAttachWorkflowTask,
  useCreateWorkflow,
  useCreateWorkflowInstance,
  workflowInstanceOptions,
  workflowInstancesOptions,
  workflowKeys,
  workflowListOptions,
} from "@silieco/core/workflows";
import { Badge } from "@silieco/ui/components/ui/badge";
import { Button } from "@silieco/ui/components/ui/button";
import { Input } from "@silieco/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@silieco/ui/components/ui/select";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { cn } from "@silieco/ui/lib/utils";
import { AppLink, useNavigation } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { StatusIcon } from "../../issues/components/status-icon";
import { useT } from "../../i18n";
import {
  sortWorkflowTasksByLifecycle,
  WORKFLOW_TASK_STATUSES,
} from "./workflow-task-sort";
import { ProjectSopDesigner } from "./project-sop-designer";
import { StageReviewControl } from "./stage-review-control";

const ALL_TASK_STATUSES = "all";

type BoardMode = "status" | "stage";
type TaskStatusFilter = typeof ALL_TASK_STATUSES | IssueStatus;

function formatTaskDate(date: string): string {
  return formatDateOnly(
    date,
    { month: "short", day: "numeric" },
    "en-US",
  );
}

function gateType(stage: Pick<WorkflowStage, "gate">): WorkflowGateType {
  const value = stage.gate.type;
  return value === "human" ||
    value === "agent" ||
    value === "hybrid" ||
    value === "none"
    ? value
    : "none";
}

function WorkflowTaskCard({ task }: { task: Issue }) {
  const paths = useWorkspacePaths();
  const { t } = useT("issues");
  const { t: tWorkflows } = useT("workflows");
  const { getActorName } = useActorName();
  const { attributes, listeners, setNodeRef, transform, isDragging } =
    useDraggable({ id: task.id });
  const assigneeName =
    task.assignee_type && task.assignee_id
      ? getActorName(task.assignee_type, task.assignee_id)
      : null;
  return (
    <article
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform) }}
      className={cn(
        "group/card rounded-lg border-[0.5px] border-surface-border bg-surface px-2.5 py-3 shadow-[var(--surface-shadow)] transition-colors hover:border-foreground/15 hover:bg-surface-hover",
        isDragging && "z-20 opacity-50 shadow-lg",
      )}
    >
      <div className="flex items-start gap-2">
        <button
          type="button"
          aria-label={tWorkflows(($) => $.board.drag_task)}
          className="mt-0.5 cursor-grab touch-none rounded text-muted-foreground/50 opacity-0 transition-opacity group-hover/card:opacity-100 focus-visible:opacity-100 active:cursor-grabbing"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-3.5" />
        </button>
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2">
            <span className="flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground">
              <PriorityIcon priority={task.priority} />
              <span className="truncate">{task.identifier}</span>
            </span>
            <span className="inline-flex shrink-0 items-center gap-1 rounded-full bg-muted/70 px-1.5 py-0.5 text-micro font-medium text-foreground">
              <StatusIcon status={task.status} className="size-3" />
              {t(($) => $.status[task.status])}
            </span>
          </div>
          <AppLink
            href={paths.taskDetail(task.id)}
            target="_blank"
            newTabTitle={`${task.identifier}: ${task.title}`}
            className="mt-1 block line-clamp-2 text-body font-medium leading-snug text-foreground hover:underline"
          >
            {task.title}
          </AppLink>
          {task.description && (
            <p className="mt-1 line-clamp-1 text-caption text-muted-foreground">
              {task.description}
            </p>
          )}
          {task.labels && task.labels.length > 0 && (
            <div className="mt-1.5 flex min-w-0 flex-wrap items-center gap-1.5">
              {task.labels.slice(0, 2).map((label) => (
                <span
                  key={label.id}
                  className="inline-flex max-w-28 items-center gap-1 rounded-full bg-muted/70 px-1.5 py-0.5 text-micro text-muted-foreground"
                >
                  <span
                    className="size-1.5 shrink-0 rounded-full"
                    style={{ backgroundColor: label.color }}
                  />
                  <span className="truncate">{label.name}</span>
                </span>
              ))}
              {task.labels.length > 2 && (
                <span className="text-micro text-muted-foreground">
                  +{task.labels.length - 2}
                </span>
              )}
            </div>
          )}
          <div className="mt-2 flex min-w-0 items-center justify-between gap-2 text-caption text-muted-foreground">
            {task.assignee_type && task.assignee_id ? (
              <span className="flex min-w-0 items-center gap-1.5">
                <ActorAvatar
                  actorType={task.assignee_type}
                  actorId={task.assignee_id}
                  size="sm"
                  enableHoverCard
                  className="shrink-0"
                />
                <span className="max-w-24 truncate">{assigneeName}</span>
              </span>
            ) : (
              <span>{t(($) => $.pickers.assignee.trigger_unassigned)}</span>
            )}
            <span className="ml-auto flex shrink-0 items-center gap-2">
              {task.start_date && (
                <span className="flex items-center gap-1">
                  <CalendarClock className="size-3" />
                  {formatTaskDate(task.start_date)}
                </span>
              )}
              {task.due_date && (
                <span
                  className={cn(
                    "flex items-center gap-1",
                    isPastDateOnly(task.due_date) && "text-destructive",
                  )}
                >
                  <CalendarDays className="size-3" />
                  {formatTaskDate(task.due_date)}
                </span>
              )}
            </span>
          </div>
        </div>
      </div>
    </article>
  );
}

function WorkflowBoardColumn({
  id,
  title,
  caption,
  tasks,
  active,
}: {
  id: string;
  title: string;
  caption?: string;
  tasks: Issue[];
  active?: boolean;
}) {
  const { t } = useT("workflows");
  const { setNodeRef, isOver } = useDroppable({ id });
  return (
    <section
      ref={setNodeRef}
      aria-label={title}
      className={cn(
        "flex min-h-0 w-[280px] shrink-0 flex-col rounded-xl bg-muted/40 p-2 transition-colors",
        isOver && "bg-accent/60 ring-2 ring-brand/25",
        active && "ring-1 ring-brand/25",
      )}
    >
      <header className="mb-2 flex min-h-8 items-start justify-between gap-2 px-1.5">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            {active && <CircleDot className="size-3.5 shrink-0 text-brand" />}
            <h3 className="truncate text-body font-medium">{title}</h3>
          </div>
          {caption && (
            <p className="mt-1 line-clamp-1 text-micro text-muted-foreground">
              {caption}
            </p>
          )}
        </div>
        <span className="shrink-0 rounded-full bg-background px-1.5 py-0.5 text-micro font-medium tabular-nums text-muted-foreground">
          {tasks.length}
        </span>
      </header>
      <div className="relative min-h-[200px] flex-1 rounded-lg">
        <div
          data-workflow-board-scroll
          className="absolute inset-0 overflow-y-auto overscroll-contain rounded-lg p-1 transition-colors"
        >
          {tasks.map((task, index) => (
            <div key={task.id} className={index === 0 ? undefined : "pt-2"}>
              <WorkflowTaskCard task={task} />
            </div>
          ))}
          {tasks.length === 0 && (
            <p className="py-8 text-center text-caption text-muted-foreground">
              {t(($) => $.board.empty_stage)}
            </p>
          )}
        </div>
      </div>
    </section>
  );
}

export function WorkflowBoard({
  instance,
  mode,
}: {
  instance: WorkflowInstance;
  mode: BoardMode;
}) {
  const { t } = useT("workflows");
  const { t: tIssues } = useT("issues");
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  const attachTask = useAttachWorkflowTask();
  const tasks = useMemo(() => instance.tasks ?? [], [instance.tasks]);
  const stages = useMemo(() => instance.stages ?? [], [instance.stages]);
  const [statusFilter, setStatusFilter] =
    useState<TaskStatusFilter>(ALL_TASK_STATUSES);
  const sortedTasks = useMemo(
    () => sortWorkflowTasksByLifecycle(tasks),
    [tasks],
  );
  const visibleTasks = useMemo(
    () =>
      statusFilter === ALL_TASK_STATUSES
        ? sortedTasks
        : sortedTasks.filter((task) => task.status === statusFilter),
    [sortedTasks, statusFilter],
  );
  const statusFilterItems = useMemo(
    () => [
      {
        value: ALL_TASK_STATUSES,
        label: t(($) => $.board.all_statuses),
      },
      ...WORKFLOW_TASK_STATUSES.map((status) => ({
        value: status,
        label: tIssues(($) => $.status[status]),
      })),
    ],
    [t, tIssues],
  );
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
  );
  const updateStatus = useMutation({
    mutationFn: ({
      taskId,
      status,
    }: {
      taskId: string;
      status: IssueStatus;
    }) => api.updateIssue(taskId, { status }),
    onSettled: () => {
      queryClient.invalidateQueries({
        queryKey: workflowKeys.instance(workspaceId, instance.id),
      });
      queryClient.invalidateQueries({ queryKey: issueKeys.all(workspaceId) });
    },
  });

  const onDragEnd = useCallback(
    (event: DragEndEvent) => {
      if (!event.over) return;
      const taskId = String(event.active.id);
      const destination = String(event.over.id);
      const task = tasks.find((item) => item.id === taskId);
      if (!task) return;
      if (mode === "stage") {
        if (task.workflow_stage_id === destination) return;
        attachTask.mutate(
          { instanceId: instance.id, taskId, stageId: destination },
          {
            onError: () => toast.error(t(($) => $.board.move_failed)),
          },
        );
        return;
      }
      const status = destination as IssueStatus;
      if (
        !WORKFLOW_TASK_STATUSES.includes(status) ||
        task.status === status
      )
        return;
      updateStatus.mutate(
        { taskId, status },
        { onError: () => toast.error(t(($) => $.board.move_failed)) },
      );
    },
    [attachTask, instance.id, mode, t, tasks, updateStatus],
  );

  return (
    <DndContext sensors={sensors} onDragEnd={onDragEnd}>
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="mb-2 flex min-h-8 items-center justify-between gap-3">
          <span className="text-caption text-muted-foreground">
            {t(($) => $.board.shown_tasks, {
              shown: visibleTasks.length,
              total: tasks.length,
            })}
          </span>
          <div className="flex items-center gap-2">
            <Select
              items={statusFilterItems}
              value={statusFilter}
              onValueChange={(value) => {
                if (value) setStatusFilter(value as TaskStatusFilter);
              }}
            >
              <SelectTrigger
                size="sm"
                aria-label={t(($) => $.board.status_filter)}
              >
                <Filter className="size-3.5 text-muted-foreground" />
                <SelectValue />
              </SelectTrigger>
              <SelectContent
                align="end"
                alignItemWithTrigger={false}
                className="max-h-72"
              >
                <SelectItem value={ALL_TASK_STATUSES}>
                  <Filter className="size-3.5 text-muted-foreground" />
                  {t(($) => $.board.all_statuses)}
                </SelectItem>
                {WORKFLOW_TASK_STATUSES.map((status) => (
                  <SelectItem key={status} value={status}>
                    <StatusIcon status={status} className="size-3.5" />
                    {tIssues(($) => $.status[status])}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
        {mode === "stage" && (
          <div className="mb-2">
            <StageReviewControl instance={instance} />
          </div>
        )}
        <div className="flex min-h-0 flex-1 gap-4 overflow-x-auto overflow-y-hidden p-2">
          {mode === "stage"
            ? stages.map((stage) => (
                <WorkflowBoardColumn
                  key={stage.id}
                  id={stage.id}
                  title={stage.name}
                  caption={t(
                    ($) =>
                      $.stage[
                        `gate_${gateType(stage)}` as keyof typeof $.stage
                      ],
                  )}
                  tasks={visibleTasks.filter(
                    (task) => task.workflow_stage_id === stage.id,
                  )}
                  active={instance.current_stage_id === stage.id}
                />
              ))
            : WORKFLOW_TASK_STATUSES.filter(
                (status) =>
                  statusFilter === ALL_TASK_STATUSES ||
                  statusFilter === status,
              ).map((status) => (
                <WorkflowBoardColumn
                  key={status}
                  id={status}
                  title={tIssues(($) => $.status[status])}
                  tasks={visibleTasks.filter((task) => task.status === status)}
                />
              ))}
        </div>
      </div>
    </DndContext>
  );
}

function CreateWorkflowPanel({
  projectId,
  onClose,
}: {
  projectId: string;
  onClose: () => void;
}) {
  const { t } = useT("workflows");
  const createWorkflow = useCreateWorkflow();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [stages, setStages] = useState(["", "", ""]);

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const normalized = stages.map((stage) => stage.trim()).filter(Boolean);
    if (!name.trim() || normalized.length === 0) return;
    createWorkflow.mutate(
      {
        project_id: projectId,
        name: name.trim(),
        description: description.trim() || null,
        publish: true,
        stages: normalized.map((stage, index) => ({
          stable_key: `stage_${index + 1}`,
          name: stage,
          gate:
            index === normalized.length - 1
              ? { type: "human" }
              : { type: "none" },
          rollback_stage_key:
            index > 0 ? `stage_${Math.max(1, index)}` : null,
        })),
      },
      { onSuccess: onClose },
    );
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/70 p-4 backdrop-blur-sm">
      <form
        onSubmit={submit}
        className="w-full max-w-xl rounded-2xl border border-border bg-popover p-5 shadow-2xl"
      >
        <div className="mb-5 flex items-center justify-between">
          <div>
            <p className="text-caption font-semibold tracking-[0.14em] text-brand">
              SOP
            </p>
            <h2 className="mt-1 text-title-lg font-semibold">
              {t(($) => $.create.title)}
            </h2>
          </div>
          <Button type="button" variant="ghost" size="icon" onClick={onClose}>
            <X className="size-4" />
          </Button>
        </div>
        <div className="space-y-4">
          <label className="block space-y-1.5 text-body font-medium">
            {t(($) => $.create.name)}
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={t(($) => $.create.name_placeholder)}
              autoFocus
            />
          </label>
          <label className="block space-y-1.5 text-body font-medium">
            {t(($) => $.create.description)}
            <Textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder={t(($) => $.create.description_placeholder)}
              rows={3}
            />
          </label>
          <fieldset>
            <legend className="mb-2 text-body font-medium">
              {t(($) => $.create.stages)}
            </legend>
            <div className="space-y-2">
              {stages.map((stage, index) => (
                <div key={index} className="flex items-center gap-2">
                  <span className="flex size-7 shrink-0 items-center justify-center rounded-full bg-muted text-caption font-semibold text-muted-foreground">
                    {index + 1}
                  </span>
                  <Input
                    value={stage}
                    onChange={(event) =>
                      setStages((current) =>
                        current.map((value, itemIndex) =>
                          itemIndex === index ? event.target.value : value,
                        ),
                      )
                    }
                    placeholder={t(($) => $.create.stage_placeholder)}
                  />
                  {stages.length > 1 && (
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() =>
                        setStages((current) =>
                          current.filter((_, itemIndex) => itemIndex !== index),
                        )
                      }
                    >
                      <X className="size-3.5" />
                    </Button>
                  )}
                </div>
              ))}
            </div>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="mt-2"
              onClick={() => setStages((current) => [...current, ""])}
            >
              <Plus className="size-3.5" />
              {t(($) => $.create.add_stage)}
            </Button>
          </fieldset>
        </div>
        <div className="mt-6 flex justify-end gap-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            {t(($) => $.create.cancel)}
          </Button>
          <Button
            type="submit"
            disabled={
              createWorkflow.isPending ||
              !name.trim() ||
              !stages.some((stage) => stage.trim())
            }
          >
            <Sparkles className="size-4" />
            {t(($) => $.create.publish)}
          </Button>
        </div>
      </form>
    </div>
  );
}

function WorkflowDefinitionCard({
  workflow,
  selected,
  onSelect,
}: {
  workflow: Workflow;
  selected: boolean;
  onSelect?: () => void;
}) {
  const paths = useWorkspacePaths();
  const className = cn(
    "group block w-full rounded-xl border p-3.5 text-left transition",
    selected
      ? "border-brand/40 bg-brand/5 shadow-sm"
      : "border-transparent hover:border-border hover:bg-muted/50",
  );
  const content = (
    <>
      <div className="flex items-start gap-3">
        <div
          className={cn(
            "flex size-9 shrink-0 items-center justify-center rounded-xl",
            selected
              ? "bg-brand text-brand-foreground"
              : "bg-muted text-muted-foreground",
          )}
        >
          <GitBranch className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <p className="truncate text-body font-semibold">{workflow.name}</p>
            {workflow.status === "published" && (
              <span className="size-1.5 shrink-0 rounded-full bg-emerald-500" />
            )}
          </div>
          <p className="mt-1 line-clamp-2 text-caption leading-5 text-muted-foreground">
            {workflow.description || "SOP"}
          </p>
        </div>
        <ChevronRight className="mt-2 size-3.5 text-muted-foreground/50 transition group-hover:translate-x-0.5" />
      </div>
    </>
  );
  if (onSelect) {
    return (
      <button type="button" className={className} onClick={onSelect}>
        {content}
      </button>
    );
  }
  return (
    <AppLink href={paths.workflowDetail(workflow.id)} className={className}>
      {content}
    </AppLink>
  );
}

export function WorkflowsPage({ workflowId }: { workflowId?: string }) {
  return <ProjectSopDesigner workflowId={workflowId} />;
}

export function LegacyWorkflowsPage({
  workflowId: routeWorkflowId,
  projectId,
  embedded = false,
}: {
  workflowId?: string;
  projectId?: string;
  embedded?: boolean;
}) {
  const { t } = useT("workflows");
  const workspaceId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const [embeddedSelectedId, setEmbeddedSelectedId] = useState<string>();
  const { data: workflows = [], isLoading } = useQuery(
    workflowListOptions(workspaceId, projectId),
  );
  const selectedId = embedded
    ? embeddedSelectedId ?? workflows[0]?.id
    : routeWorkflowId ?? workflows[0]?.id;
  const selected = workflows.find((workflow) => workflow.id === selectedId);
  const { data: allInstances = [] } = useQuery({
    ...workflowInstancesOptions(workspaceId, selectedId, projectId),
    enabled: Boolean(selectedId),
  });
  const instances = useMemo(
    () => allInstances.filter((item) => !item.archived_at),
    [allInstances],
  );
  const [selectedInstanceId, setSelectedInstanceId] = useState<string>();
  useEffect(() => {
    if (
      !selectedInstanceId ||
      !instances.some((instance) => instance.id === selectedInstanceId)
    ) {
      setSelectedInstanceId(instances[0]?.id);
    }
  }, [instances, selectedInstanceId]);
  const { data: instance } = useQuery({
    ...workflowInstanceOptions(workspaceId, selectedInstanceId ?? ""),
    enabled: Boolean(selectedInstanceId),
  });
  const [showCreate, setShowCreate] = useState(false);
  const [runTitle, setRunTitle] = useState("");
  const [taskToAddId, setTaskToAddId] = useState<string | null>(null);
  const [boardMode, setBoardMode] = useState<BoardMode>("stage");
  const createInstance = useCreateWorkflowInstance();
  const attachTask = useAttachWorkflowTask();
  const { data: taskPool } = useQuery({
    queryKey: ["workflow-task-pool", workspaceId, projectId ?? "all-projects"],
    queryFn: () =>
      api.listIssues({
        limit: 100,
        ...(projectId ? { project_id: projectId } : {}),
      }),
  });
  const availableTasks = useMemo(() => {
    const attached = new Set(instance?.tasks?.map((task) => task.id) ?? []);
    return (taskPool?.issues ?? []).filter((task) => !attached.has(task.id));
  }, [instance?.tasks, taskPool?.issues]);

  useEffect(() => {
    if (
      embedded &&
      (!embeddedSelectedId ||
        !workflows.some((workflow) => workflow.id === embeddedSelectedId))
    ) {
      setEmbeddedSelectedId(workflows[0]?.id);
    }
  }, [embedded, embeddedSelectedId, workflows]);

  useEffect(() => {
    if (!embedded && !routeWorkflowId && workflows[0]) {
      navigation.replace(paths.workflowDetail(workflows[0].id));
    }
  }, [embedded, navigation, paths, routeWorkflowId, workflows]);

  const startRun = () => {
    if (!selected || !projectId || !runTitle.trim()) return;
    createInstance.mutate(
      {
        workflowId: selected.id,
        data: { title: runTitle.trim(), project_id: projectId, start: true },
      },
      {
        onSuccess: (created) => {
          setRunTitle("");
          setSelectedInstanceId(created.id);
          queryClient.invalidateQueries({
            queryKey: workflowKeys.instances(
              workspaceId,
              selected.id,
              projectId,
            ),
          });
        },
      },
    );
  };

  const addTask = (taskId: string) => {
    const firstStage = instance?.stages?.[0];
    if (!firstStage || !taskId || !instance) return;
    attachTask.mutate(
      { instanceId: instance.id, taskId, stageId: firstStage.id },
      {
        onSuccess: () =>
          queryClient.invalidateQueries({
            queryKey: ["workflow-task-pool", workspaceId],
          }),
      },
    );
  };

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center text-body text-muted-foreground">
        {t(($) => $.board.loading)}
      </div>
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden bg-background">
      {embedded ? (
        <header className="flex min-h-12 items-center justify-between gap-4 border-b border-border/70 px-5 py-2.5">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <GitBranch className="size-4 text-brand" />
              <h2 className="text-body font-semibold">
                {t(($) => $.designer.library_title)}
              </h2>
              <Badge variant="secondary">{workflows.length}</Badge>
            </div>
            <p className="mt-0.5 text-caption text-muted-foreground">
              {t(($) => $.designer.library_description)}
            </p>
          </div>
          <Button size="sm" onClick={() => setShowCreate(true)}>
            <Plus className="size-3.5" />
            {t(($) => $.page.create)}
          </Button>
        </header>
      ) : (
      <header className="border-b border-border/70 px-6 py-5">
        <div className="mx-auto flex max-w-[1500px] items-end justify-between gap-5">
          <div>
            <p className="text-micro font-semibold tracking-[0.18em] text-brand">
              {t(($) => $.page.eyebrow)}
            </p>
            <h1 className="mt-1.5 text-display-sm font-semibold tracking-tight">
              {t(($) => $.page.title)}
            </h1>
            <p className="mt-2 max-w-3xl text-body leading-6 text-muted-foreground">
              {t(($) => $.page.description)}
            </p>
          </div>
          <Button
            disabled={!projectId}
            onClick={() => setShowCreate(true)}
          >
            <Plus className="size-4" />
            {t(($) => $.page.create)}
          </Button>
        </div>
      </header>
      )}

      {workflows.length === 0 ? (
        <main className="flex flex-1 items-center justify-center p-6">
          <div className="max-w-md text-center">
            <div className="mx-auto flex size-14 items-center justify-center rounded-2xl bg-brand/10 text-brand">
              <Route className="size-6" />
            </div>
            <h2 className="mt-5 text-title-lg font-semibold">
              {t(($) => $.page.empty_title)}
            </h2>
            <p className="mt-2 text-body leading-6 text-muted-foreground">
              {t(($) => $.page.empty_description)}
            </p>
            <Button
              className="mt-5"
              disabled={!projectId}
              onClick={() => setShowCreate(true)}
            >
              <Plus className="size-4" />
              {t(($) => $.page.create)}
            </Button>
          </div>
        </main>
      ) : (
        <main className="mx-auto flex min-h-0 w-full max-w-[1500px] flex-1">
          <aside className="w-[292px] shrink-0 overflow-y-auto border-r border-border/70 p-3">
            <div className="space-y-1">
              {workflows.map((workflow) => (
                <WorkflowDefinitionCard
                  key={workflow.id}
                  workflow={workflow}
                  selected={workflow.id === selectedId}
                  onSelect={
                    embedded
                      ? () => setEmbeddedSelectedId(workflow.id)
                      : undefined
                  }
                />
              ))}
            </div>
          </aside>

          <section className="flex min-w-0 flex-1 flex-col overflow-hidden">
            {selected && (
              <>
                <div className="border-b border-border/70 px-5 py-4">
                  <div className="flex flex-wrap items-start justify-between gap-4">
                    <div>
                      <div className="flex items-center gap-2">
                        <h2 className="text-title font-semibold">{selected.name}</h2>
                        <Badge variant="secondary">
                          {t(
                            ($) =>
                              $.detail[
                                selected.status as keyof typeof $.detail
                              ],
                          )}
                        </Badge>
                        {selected.current_version && (
                          <span className="text-caption text-muted-foreground">
                            {t(($) => $.detail.version, {
                              version: selected.current_version!.version,
                            })}
                          </span>
                        )}
                      </div>
                      <p className="mt-1 max-w-2xl text-body text-muted-foreground">
                        {selected.description}
                      </p>
                    </div>
                    <div className="flex items-end gap-2">
                      <label className="space-y-1 text-caption text-muted-foreground">
                        <span className="block">{t(($) => $.detail.run_title)}</span>
                        <Input
                          className="h-8 w-56"
                          value={runTitle}
                          onChange={(event) => setRunTitle(event.target.value)}
                          placeholder={t(($) => $.detail.run_placeholder)}
                        />
                      </label>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={
                          !projectId ||
                          !runTitle.trim() ||
                          createInstance.isPending
                        }
                        onClick={startRun}
                      >
                        <Route className="size-3.5" />
                        {t(($) => $.detail.start_confirm)}
                      </Button>
                    </div>
                  </div>

                  <div className="mt-4 flex gap-1 overflow-x-auto">
                    {(selected.current_version?.stages ?? []).map(
                      (stage, index, all) => (
                        <div key={stage.id} className="flex items-center">
                          <div className="flex items-center gap-2 rounded-lg border border-border/70 bg-muted/30 px-2.5 py-1.5">
                            <span className="flex size-5 items-center justify-center rounded-full bg-background text-micro font-semibold">
                              {index + 1}
                            </span>
                            <span className="whitespace-nowrap text-caption font-medium">
                              {stage.name}
                            </span>
                            {gateType(stage) !== "none" && (
                              <ShieldCheck className="size-3 text-brand" />
                            )}
                          </div>
                          {index < all.length - 1 && (
                            <ChevronRight className="mx-0.5 size-3.5 text-muted-foreground/50" />
                          )}
                        </div>
                      ),
                    )}
                  </div>
                </div>

                <div className="flex min-h-0 flex-1 flex-col px-5 pt-4">
                  <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
                    <div>
                      <div className="flex items-center gap-2">
                        <h2 className="text-title-sm font-semibold">
                          {t(($) => $.board.title)}
                        </h2>
                        <div className="flex rounded-lg bg-muted p-0.5">
                          <button
                            type="button"
                            className={cn(
                              "rounded-md px-2.5 py-1 text-caption font-medium transition",
                              boardMode === "status"
                                ? "bg-background text-foreground shadow-sm"
                                : "text-muted-foreground",
                            )}
                            onClick={() => setBoardMode("status")}
                          >
                            {t(($) => $.board.status_mode)}
                          </button>
                          <button
                            type="button"
                            className={cn(
                              "rounded-md px-2.5 py-1 text-caption font-medium transition",
                              boardMode === "stage"
                                ? "bg-background text-foreground shadow-sm"
                                : "text-muted-foreground",
                            )}
                            onClick={() => setBoardMode("stage")}
                          >
                            {t(($) => $.board.stage_mode)}
                          </button>
                        </div>
                      </div>
                      <p className="mt-1 text-caption text-muted-foreground">
                        {t(($) => $.board.context)}
                      </p>
                    </div>
                    <div className="flex items-center gap-2">
                      {instances.length > 0 && (
                        <Select
                          items={instances.map((item) => ({
                            value: item.id,
                            label: item.title,
                          }))}
                          value={selectedInstanceId}
                          onValueChange={(value) => {
                            if (value) setSelectedInstanceId(value);
                          }}
                        >
                          <SelectTrigger
                            size="sm"
                            className="max-w-56"
                            aria-label={t(($) => $.detail.select_run)}
                          >
                            <Route className="size-3.5 text-muted-foreground" />
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent
                            align="end"
                            alignItemWithTrigger={false}
                            className="max-h-72"
                          >
                            {instances.map((item) => (
                              <SelectItem key={item.id} value={item.id}>
                                <Route className="size-3.5 text-muted-foreground" />
                                <span className="truncate">{item.title}</span>
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      )}
                      {instance && instance.stages?.length ? (
                        <Select
                          items={availableTasks.map((task) => ({
                            value: task.id,
                            label: `${task.identifier} · ${task.title}`,
                          }))}
                          value={taskToAddId}
                          onValueChange={(value) => {
                            if (!value) return;
                            addTask(value);
                            setTaskToAddId(null);
                          }}
                        >
                          <SelectTrigger
                            size="sm"
                            className="max-w-56"
                            disabled={availableTasks.length === 0}
                            aria-label={t(($) => $.board.add_task)}
                          >
                            <Plus className="size-3.5 text-muted-foreground" />
                            <SelectValue
                              placeholder={t(($) => $.board.add_task)}
                            />
                          </SelectTrigger>
                          <SelectContent
                            align="end"
                            alignItemWithTrigger={false}
                            className="max-h-72 min-w-64"
                          >
                            {availableTasks.map((task) => (
                              <SelectItem key={task.id} value={task.id}>
                                <span className="truncate">
                                  {task.identifier} · {task.title}
                                </span>
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      ) : null}
                    </div>
                  </div>

                  {instance ? (
                    <WorkflowBoard instance={instance} mode={boardMode} />
                  ) : (
                    <div className="flex flex-1 items-center justify-center rounded-2xl border border-dashed border-border text-body text-muted-foreground">
                      <div className="text-center">
                        <Users className="mx-auto mb-3 size-7 text-muted-foreground/50" />
                        {t(($) => $.detail.no_runs)}
                      </div>
                    </div>
                  )}
                </div>
              </>
            )}
          </section>
        </main>
      )}
      {showCreate && projectId && (
        <CreateWorkflowPanel
          projectId={projectId}
          onClose={() => setShowCreate(false)}
        />
      )}
    </div>
  );
}
