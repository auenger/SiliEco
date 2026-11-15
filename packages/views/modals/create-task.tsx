"use client";

import { useState, useRef, useEffect, useLayoutEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigation } from "../navigation";
import {
  AlertTriangle,
  ArrowDown,
  ArrowLeftRight,
  ArrowUp,
  CalendarClock,
  CalendarDays,
  Check,
  ChevronRight,
  CircleUser,
  FolderKanban,
  Maximize2,
  Minimize2,
  MoreHorizontal,
  Settings2,
  Shapes,
  Tag,
  X as XIcon,
} from "lucide-react";
import { cn } from "@silieco/ui/lib/utils";
import { toast } from "sonner";
import type {
  Task,
  TaskStatus,
  TaskPriority,
  TaskAssigneeType,
  TaskPropertyValue,
} from "@silieco/core/types";
import { attachmentIdsFromContent, contentReferencesAttachment } from "@silieco/core/types";
import {
  DialogContent,
  DialogTitle,
} from "@silieco/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@silieco/ui/components/ui/dropdown-menu";
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "@silieco/ui/components/ui/tooltip";
import { Button } from "@silieco/ui/components/ui/button";
import { Switch } from "@silieco/ui/components/ui/switch";
import { ContentEditor, type ContentEditorRef, TitleEditor, type TitleEditorRef, useFileDropZone, FileDropOverlay, useUploadGate, useComposerSubmit } from "../editor";
import { useTaskCreateUploads } from "./use-task-create-uploads";
import { useShortcut } from "@silieco/core/shortcuts";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import { StatusIcon, StatusPicker, PriorityIcon, PriorityPicker, StagePicker, AssigneePicker, StartDatePicker, DueDatePicker, LabelPicker } from "../tasks/components";
import { maxSiblingStage } from "../tasks/components/pickers/stage-picker";
import { ProjectPicker } from "../projects/components/project-picker";
import { WorkflowAssignmentPicker } from "../workflows/components";
import { useTaskTriggerPreview } from "../tasks/hooks/use-task-trigger-preview";
import { useActorName } from "@silieco/core/workspace/hooks";
import { useCurrentWorkspace, useWorkspacePaths } from "@silieco/core/paths";
import { useWorkspaceId } from "@silieco/core/hooks";
import { useTaskDraftStore, type TaskCreateDraft } from "@silieco/core/tasks/stores/draft-store";
import { useCreateModeStore } from "@silieco/core/tasks/stores/create-mode-store";
import { useQuickCreateStore } from "@silieco/core/tasks/stores/quick-create-store";
import {
  useTaskCreateSettingsStore,
  type ManualCreateField,
} from "@silieco/core/tasks/stores/task-create-settings-store";
import { taskDetailOptions, childTasksOptions } from "@silieco/core/tasks/queries";
import { useCreateTask, useUpdateTask } from "@silieco/core/tasks/mutations";
import { useAttachLabelToTask } from "@silieco/core/labels";
import {
  propertyListOptions,
  useSetTaskProperty,
} from "@silieco/core/properties";
import {
  api,
  ApiError,
  DuplicateTaskErrorBodySchema,
  type DuplicateTaskErrorBody,
  parseWithFallback,
} from "@silieco/core/api";
import { FileUploadButton } from "@silieco/ui/components/common/file-upload-button";
import { PillButton } from "../common/pill-button";
import { ActorAvatar } from "../common/actor-avatar";
import { PropertyIcon } from "../common/property-icon";
import {
  CustomPropertyValueDisplay,
  CustomPropertyValueInput,
} from "../tasks/components/pickers/custom-property-picker";
import { TaskPickerModal } from "./task-picker-modal";
import { useT } from "../i18n";

// ---------------------------------------------------------------------------
// ManualCreatePanel — manual-mode body of the create-task dialog. Renders
// DialogContent + everything inside; the surrounding `<Dialog>` is owned by
// CreateTaskDialog so mode switching swaps only the inner panel without
// remounting the Dialog Root (no overlay flash). `onSwitchMode` flips the
// shell's local mode state.
// ---------------------------------------------------------------------------

// CreateRunHint is the create modal's passive pre-trigger label (SILI-3375 §4):
// whether saving will start a run, driven by the unified backend predicate
// (preview, isCreate) — never a frontend guess. No dialog, no blocking.
//
// Visually it borrows the comment header's avatar+text line, minus the
// interactivity — purely a caption, never a link/hover-card. It renders its own
// reveal band (a grid 0fr→1fr collapse) so it sits on a dedicated row above the
// property toolbar without reflowing anything: collapsed it is 0px (the flex-1
// editor absorbs the delta), and it expands only once the predicate resolves,
// animating straight to the correct copy.
function CreateRunHint({
  assigneeType,
  assigneeId,
  status,
}: {
  assigneeType?: TaskAssigneeType;
  assigneeId?: string;
  status: TaskStatus;
}) {
  const { t } = useT("modals");
  const { getActorName } = useActorName();
  const isAgentLike = assigneeType === "agent" || assigneeType === "squad";
  const preview = useTaskTriggerPreview({
    isCreate: true,
    assigneeType: assigneeType ?? null,
    assigneeId: assigneeId ?? null,
    status,
    enabled: isAgentLike && !!assigneeId,
  });

  // Reveal only after the predicate resolves so the band animates to the final
  // copy instead of flashing "parked" before the run preview lands.
  const ready = isAgentLike && !!assigneeId && !preview.isLoading;
  const willStart = preview.totalCount > 0;
  const isSquad = assigneeType === "squad";
  const triggerAgentId = preview.triggers[0]?.agent_id ?? assigneeId;

  // Avatar + copy mirror the flow. A squad doesn't "work" — its leader
  // evaluates and delegates — so the squad path keeps the squad as the subject
  // (avatar + name) and uses the leader-delegates copy. A single agent picks
  // the task up directly; a parked task shows whoever it was assigned to.
  let avatarType: string;
  let avatarId: string | undefined;
  let text: string;
  if (!willStart) {
    avatarType = assigneeType ?? "agent";
    avatarId = assigneeId;
    text = t(($) => $.run_confirm.create_parked);
  } else if (isSquad) {
    avatarType = "squad";
    avatarId = assigneeId;
    text = t(($) => $.run_confirm.create_will_start_squad, {
      name: getActorName("squad", assigneeId ?? ""),
    });
  } else {
    avatarType = "agent";
    avatarId = triggerAgentId;
    text = t(($) => $.run_confirm.create_will_start, {
      name: getActorName("agent", triggerAgentId ?? assigneeId ?? ""),
    });
  }

  return (
    <div
      className={cn(
        "grid shrink-0 transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none",
        ready ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
      )}
      aria-hidden={!ready}
    >
      <div className="overflow-hidden">
        <div
          aria-live="polite"
          className="flex items-center gap-1.5 px-4 pb-1 pt-0.5 text-micro text-muted-foreground"
        >
          {avatarId && (
            <ActorAvatar
              actorType={avatarType}
              actorId={avatarId}
              size="sm"
              profileLink={false}
            />
          )}
          <span className="truncate">{text}</span>
        </div>
      </div>
    </div>
  );
}

export function ManualCreatePanel({
  onClose,
  onSwitchMode,
  data,
  isExpanded,
  setIsExpanded,
}: {
  onClose: () => void;
  /** Called with the carry payload to seed the agent panel after switch. */
  onSwitchMode?: (carry?: Record<string, unknown> | null) => void;
  data?: Record<string, unknown> | null;
  /** Lifted to the shell so DialogContent's mode-aware className can react
   *  without the body itself having to live inside DialogContent (which would
   *  re-mount the Portal on mode swap and replay the open animation). */
  isExpanded: boolean;
  setIsExpanded: (v: boolean) => void;
}) {
  const { t } = useT("modals");
  const { t: tEditor } = useT("editor");
  const { t: tResources } = useT("resources");
  const router = useNavigation();
  const p = useWorkspacePaths();
  const workspaceName = useCurrentWorkspace()?.name;

  const draft = useTaskDraftStore((s) => s.draft);
  const setManual = useTaskDraftStore((s) => s.setManual);
  const setShared = useTaskDraftStore((s) => s.setShared);
  const setAgent = useTaskDraftStore((s) => s.setAgent);
  const setActiveMode = useTaskDraftStore((s) => s.setActiveMode);
  const clearDraft = useTaskDraftStore((s) => s.clearDraft);
  const setLastAssignee = useTaskDraftStore((s) => s.setLastAssignee);
  const setLastMode = useCreateModeStore((s) => s.setLastMode);
  const keepOpen = useQuickCreateStore((s) => s.keepOpen);
  const setKeepOpen = useQuickCreateStore((s) => s.setKeepOpen);
  const manualFields = useTaskCreateSettingsStore((s) => s.manualCreateFields);

  const sendShortcut = useShortcut("send");
  const [title, setTitle] = useState(draft.manual.title);
  const [formResetKey, setFormResetKey] = useState(0);
  const titleEditorRef = useRef<TitleEditorRef>(null);
  const descEditorRef = useRef<ContentEditorRef>(null);
  const { isDragOver: descDragOver, dropZoneProps: descDropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => descEditorRef.current?.uploadFile(f)),
  });
  const [status, setStatus] = useState<TaskStatus>((data?.status as TaskStatus) || draft.manual.status);
  const [priority, setPriority] = useState<TaskPriority>(
    (data?.priority as TaskPriority | undefined) ?? draft.shared.priority,
  );
  const [assigneeType, setAssigneeType] = useState<TaskAssigneeType | undefined>(() => {
    if (data && "assignee_type" in data) {
      return (data.assignee_type as TaskAssigneeType | null) ?? undefined;
    }
    return draft.manual.assigneeType;
  });
  const [assigneeId, setAssigneeId] = useState<string | undefined>(() => {
    if (data && "assignee_id" in data) {
      return (data.assignee_id as string | null) ?? undefined;
    }
    return draft.manual.assigneeId;
  });
  const [startDate, setStartDate] = useState<string | null>(draft.manual.startDate);
  const [dueDate, setDueDate] = useState<string | null>(
    (data?.due_date as string | undefined) ?? draft.shared.dueDate,
  );
  const [labelIds, setLabelIds] = useState<string[]>(draft.manual.labelIds);
  const [propertyValues, setPropertyValues] = useState(draft.manual.propertyValues ?? {});
  const [customPropertyPickerId, setCustomPropertyPickerId] = useState<string | null>(null);
  const [projectId, setProjectId] = useState<string | undefined>(() => {
    if (data && "project_id" in data) {
      return (data.project_id as string | null) ?? undefined;
    }
    return draft.shared.projectId;
  });
  const [workflowInstanceId, setWorkflowInstanceId] = useState<
    string | undefined
  >(
    typeof data?.workflow_instance_id === "string"
      ? data.workflow_instance_id
      : undefined,
  );
  const [workflowStageId, setWorkflowStageId] = useState<string | undefined>(
    typeof data?.workflow_stage_id === "string"
      ? data.workflow_stage_id
      : undefined,
  );
  const [parentTaskId, setParentTaskId] = useState<string | undefined>(
    (data?.parent_task_id as string) || undefined,
  );
  // Stage only applies to a sub-task; kept local (not in the persisted draft)
  // since it's a per-creation choice tied to the chosen parent.
  const [stage, setStage] = useState<number | null>(
    typeof data?.stage === "number" ? (data.stage as number) : null,
  );
  const [parentPickerOpen, setParentPickerOpen] = useState(false);
  // Toolbar fields hidden via Settings → Task reuse the overflow reveal
  // pattern: the ⋯ menu item flips this open, which mounts the inline pill
  // (the popover's anchor) AND opens the picker. Closing without a value
  // unmounts the pill again; a field holding a non-default value always
  // renders regardless of the setting so nothing applied is ever invisible.
  const [fieldPickerOpen, setFieldPickerOpen] = useState<Exclude<
    ManualCreateField,
    "due_date" | "start_date"
  > | null>(null);
  // Start date is a low-frequency field — by default it lives in the
  // overflow ⋯ menu. Clicking the menu item flips this open, which both
  // mounts the inline pill (the popover's anchor) AND opens the calendar.
  // When the popover closes without a value set, the pill unmounts again.
  const [startDatePickerOpen, setStartDatePickerOpen] = useState(false);
  // Due date follows the same overflow pattern as start date: collapsed into
  // the ⋯ menu by default, mounted inline (as the popover anchor) only when it
  // has a value or the user just opened it from the menu.
  const [dueDatePickerOpen, setDueDatePickerOpen] = useState(false);
  // Children live as full Task objects — the picker always returns the whole
  // object, and we never need to hydrate from an ID the way we do for parent.
  const [childTasks, setChildTasks] = useState<Task[]>([]);
  const [childPickerOpen, setChildPickerOpen] = useState(false);
  // Fetch parent task details for the chip (status/identifier/title).
  // List cache usually has it already, so this resolves synchronously.
  const wsId = useWorkspaceId();
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(wsId));
  const { data: parentTask } = useQuery({
    ...taskDetailOptions(wsId, parentTaskId ?? ""),
    enabled: !!parentTaskId,
  });
  // Sibling stages under the chosen parent, so the Stage picker can offer the
  // already-used max stage (and one beyond) instead of flooring at Stage 1–3.
  const { data: parentChildren = [] } = useQuery({
    ...childTasksOptions(wsId, parentTaskId ?? ""),
    enabled: !!parentTaskId,
  });

  // Set the persisted draft's active mode so a later reopen (and any reader of
  // the unified draft) knows which form the user is editing in.
  useEffect(() => {
    setActiveMode("manual");
  }, [setActiveMode]);

  // Prune completed uploads whose markdown reference was deleted in an
  // earlier editing session. Runs once on mount: at that point the persisted
  // manual description / agent prompt ARE the draft bodies (no editor edits
  // have happened yet), so dropping `uploaded` records referenced by neither
  // is safe. Placeholders (uploading / failed / interrupted) are always kept —
  // they have no body reference yet and the status chips are their only UI.
  // Don't prune on description updates — an onUpdate flush can race a
  // just-finished upload whose markdown link hasn't been inserted yet, and
  // pruning there would drop a live attachment.
  useEffect(() => {
    const { draft: current } = useTaskDraftStore.getState();
    const uploads = current.shared.attachments ?? [];
    const kept = uploads.filter(
      (u) =>
        u.status !== "uploaded" ||
        contentReferencesAttachment(current.manual.description, u.attachment) ||
        contentReferencesAttachment(current.agent.prompt, u.attachment),
    );
    if (kept.length !== uploads.length) setShared({ attachments: kept });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Gate every action that fixes this draft: Create and the switch to agent
  // mode (which assist-inits the agent prompt from the description and would
  // carry a stripped body across).
  const uploadGate = useUploadGate(descEditorRef);
  // Coordinator-owned uploads in the shared pool (SILI-5181, L2): a file picked
  // here survives dialog close, aborts on logout, and is dropped after a
  // reload. `gate` widens the editor gate with the pool's placeholders.
  const {
    attachments: draftAttachments,
    handleUpload,
    gate,
  } = useTaskCreateUploads("manual", uploadGate, descEditorRef);

  // Sync field changes to the draft store — manual-only fields to the manual
  // slot, project / priority / due date to the shared slot.
  const updateTitle = (v: string) => { setTitle(v); setManual({ title: v }); };
  const updateStatus = (v: TaskStatus) => { setStatus(v); setManual({ status: v }); };
  const updatePriority = (v: TaskPriority) => { setPriority(v); setShared({ priority: v }); };
  const updateAssignee = (type?: TaskAssigneeType, id?: string) => {
    setAssigneeType(type); setAssigneeId(id);
    setManual({ assigneeType: type, assigneeId: id });
  };
  const updateProject = (id?: string) => {
    if (id !== projectId) {
      setWorkflowInstanceId(undefined);
      setWorkflowStageId(undefined);
    }
    setProjectId(id);
    setShared({ projectId: id });
  };
  const updateStartDate = (v: string | null) => { setStartDate(v); setManual({ startDate: v }); };
  const updateDueDate = (v: string | null) => { setDueDate(v); setShared({ dueDate: v }); };
  const updateLabelIds = (ids: string[]) => { setLabelIds(ids); setManual({ labelIds: ids }); };
  const updatePropertyValue = (propertyId: string, value: TaskPropertyValue | undefined) => {
    const next = { ...propertyValues };
    if (value === undefined) delete next[propertyId];
    else next[propertyId] = value;
    setPropertyValues(next);
    setManual({ propertyValues: next });
  };

  // Inline pill reveal per toolbar field: kept by Settings → Task, holding a
  // non-default value (a hidden field with a value must stay visible — the
  // draft or a mode-switch carry may have set it), or just opened from the ⋯
  // overflow (the picker popover needs the inline pill as its anchor).
  const showField = {
    status: manualFields.includes("status") || status !== "todo" || fieldPickerOpen === "status",
    priority: manualFields.includes("priority") || priority !== "none" || fieldPickerOpen === "priority",
    assignee: manualFields.includes("assignee") || assigneeId != null || fieldPickerOpen === "assignee",
    labels: manualFields.includes("labels") || labelIds.length > 0 || fieldPickerOpen === "labels",
    project: manualFields.includes("project") || projectId != null || fieldPickerOpen === "project",
    due_date: manualFields.includes("due_date") || dueDate !== null || dueDatePickerOpen,
    start_date: manualFields.includes("start_date") || startDate !== null || startDatePickerOpen,
  };

  // Field visibility lives in Settings → Task; the modal closes first so the
  // dialog doesn't linger over the settings page. The draft store already
  // holds everything typed, so nothing is lost across the round-trip.
  const openFieldSettings = () => {
    onClose();
    router.push(`${p.settings()}?tab=task`);
  };

  const createTaskMutation = useCreateTask();
  const updateTaskMutation = useUpdateTask();
  const attachLabelMutation = useAttachLabelToTask();
  const setTaskPropertyMutation = useSetTaskProperty();
  const resetForNextTask = () => {
    setTitle("");
    setStatus("todo");
    setPriority("none");
    setStartDate(null);
    setDueDate(null);
    setLabelIds([]);
    setPropertyValues({});
    setCustomPropertyPickerId(null);
    setProjectId(undefined);
    setWorkflowInstanceId(undefined);
    setWorkflowStageId(undefined);
    setParentTaskId(undefined);
    setStage(null);
    setChildTasks([]);
    // Keep the just-used assignee for the next task in the batch; reset
    // everything else across the manual + shared slots.
    setManual({
      title: "",
      description: "",
      status: "todo",
      assigneeType,
      assigneeId,
      startDate: null,
      labelIds: [],
      propertyValues: {},
    });
    setShared({
      priority: "none",
      projectId: undefined,
      dueDate: null,
      attachments: [],
    });
    descEditorRef.current?.clearContent();
    setFormResetKey((key) => key + 1);
  };

  // Manual create runs through the shared await-then-render composer contract
  // (single-flight ref, submit-time upload re-check, lock+spin, await→boolean,
  // clear only on acceptance). Manual is gated on the TITLE rather than the
  // editor body — a title-only task is valid — so `normalize` ignores the
  // description markdown and feeds the title through as the empty-guard/content;
  // the body is read separately inside onSubmit.
  // Stale-submit guard (SILI-5181 P0): the task draft is a SINGLETON store
  // and the editors stay interactive during a request. Snapshot the draft's
  // object identity at submit; success clears ONLY an untouched draft —
  // whether the edit came mid-flight or from a reopened dialog.
  const mountedRef = useRef(true);
  useLayoutEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const submittedDraftRef = useRef<TaskCreateDraft | null>(null);

  const composer = useComposerSubmit({
    editorRef: descEditorRef,
    uploadGate: gate,
    normalize: () => title.trim(),
    onSubmit: async (): Promise<boolean> => {
      // An SOP assignment is atomic: a Task may be project-only, but it may
      // never be attached to a run without a Stage.
      if (workflowInstanceId && !workflowStageId) {
        toast.error(t(($) => $.create_task.stage_required));
        return false;
      }
      // Flush the description editor's pending debounce into the store BEFORE
      // snapshotting, so a late flush of pre-submit typing cannot masquerade
      // as an edit made during the request.
      const pendingDesc = descEditorRef.current?.flushPendingUpdate?.();
      if (pendingDesc != null) setManual({ description: pendingDesc });
      submittedDraftRef.current = useTaskDraftStore.getState().draft;
      try {
      const description = descEditorRef.current?.getMarkdown()?.trim() || undefined;
      const activeAttachmentIds = draftAttachments
        .filter((a) => contentReferencesAttachment(description ?? "", a))
        .map((a) => a.id);
      const uploadedAttachmentIds = new Set(activeAttachmentIds);
      const referencedResourceIds = attachmentIdsFromContent(description ?? "")
        .filter((attachmentId) => !uploadedAttachmentIds.has(attachmentId));
      const task = await createTaskMutation.mutateAsync({
        title: title.trim(),
        description,
        status,
        priority,
        assignee_type: assigneeType,
        assignee_id: assigneeId,
        start_date: startDate || undefined,
        due_date: dueDate || undefined,
        attachment_ids: activeAttachmentIds.length > 0 ? activeAttachmentIds : undefined,
        // The server attaches these in the same transaction as the create and
        // echoes them back as `task.labels`, so a stale selection fails the
        // create instead of leaving a committed-but-unlabeled task. A legacy
        // backend that predates this ignores the field — handled by the
        // compatibility fallback below.
        label_ids: labelIds.length > 0 ? labelIds : undefined,
        parent_task_id: parentTaskId,
        // Stage is only meaningful for a sub-task (relative to its siblings).
        stage: parentTaskId && stage != null ? stage : undefined,
        project_id: projectId,
        ...(workflowInstanceId && workflowStageId
          ? {
              workflow_instance_id: workflowInstanceId,
              workflow_stage_id: workflowStageId,
            }
          : {}),
      });

      if (referencedResourceIds.length > 0) {
        const results = await Promise.allSettled(
          referencedResourceIds.map((attachmentId) =>
            api.createAttachmentReference("task", task.id, attachmentId),
          ),
        );
        const failed = results.filter((result) => result.status === "rejected").length;
        if (failed > 0) {
          console.error("[create-task] resource reference failed", results);
          toast.error(tResources(($) => $.reference_failed));
        }
      }

      // Custom-property values can only be addressed once the task has an
      // id. Keep the modal in its submitting state until every value settles
      // so closing or "Create another" cannot race the fan-out.
      const propertyEntries = Object.entries(propertyValues);
      if (propertyEntries.length > 0) {
        const results = await Promise.allSettled(
          propertyEntries.map(([propertyId, value]) =>
            setTaskPropertyMutation.mutateAsync({
              taskId: task.id,
              propertyId,
              value,
            }),
          ),
        );
        let failed = 0;
        for (const result of results) {
          if (result.status === "rejected") {
            failed += 1;
            console.error("[create-task] custom property set failed", result.reason);
          }
        }
        if (failed > 0) {
          toast.error(
            t(($) => $.create_task.toast_set_properties_failed, { count: failed }),
          );
        }
      }

      // Link queued children to the new parent. Deferred to after create
      // because the new task's ID doesn't exist yet. Partial failures don't
      // roll back the new task — it's already committed.
      if (childTasks.length > 0) {
        const results = await Promise.allSettled(
          childTasks.map((child) =>
            updateTaskMutation.mutateAsync({
              id: child.id,
              parent_task_id: task.id,
            }),
          ),
        );
        // Aggregate fan-out: N independent requests can fail for N different
        // reasons. The user-facing toast stays count-based (any single
        // err.message would mislead), but log each rejection so developers
        // still have signal in dev-tools / Sentry.
        for (const result of results) {
          if (result.status === "rejected") {
            console.error("[create-task] sub-task link failed", result.reason);
          }
        }
        const failed = results.filter((r) => r.status === "rejected").length;
        if (failed > 0) {
          toast.error(
            failed === childTasks.length
              ? t(($) => $.create_task.toast_link_subtasks_all_failed)
              : t(($) => $.create_task.toast_link_subtasks_partial, {
                  failed,
                  total: childTasks.length,
                }),
          );
        }
      }

      // Backend-compatibility fallback for the rolling deploy window: the web
      // app auto-deploys on merge but the backend deploys manually, so a newer
      // web build can briefly talk to a backend that predates atomic label
      // creation. That backend silently ignores `label_ids` and returns an
      // task with no `labels` field. Only then do we fall back to the legacy
      // per-label attach so the user's labels aren't silently dropped. When
      // `labels` is present (current backend) the atomic path already ran, so
      // we skip this — no double-write, no per-label fan-out.
      if (labelIds.length > 0 && task.labels === undefined) {
        const results = await Promise.allSettled(
          labelIds.map((labelId) =>
            attachLabelMutation.mutateAsync({ taskId: task.id, labelId }),
          ),
        );
        let labelsFailed = 0;
        for (const result of results) {
          if (result.status === "rejected") {
            labelsFailed += 1;
            console.error("[create-task] label attach fallback failed", result.reason);
          }
        }
        if (labelsFailed > 0) {
          toast.error(t(($) => $.create_task.toast_link_labels_failed));
        }
      }

      // The old post-create "agent paused in Backlog" blocking panel is gone —
      // a passive inline hint now warns before submit (SILI-3375). The draft
      // reset + close/keep-open happens in onAccepted once we report success.
      {
        toast.custom((toastId) => (
          <div className="bg-popover text-popover-foreground border rounded-lg shadow-lg p-4 w-[360px]">
            <div className="flex items-center gap-2 mb-2">
              <div className="flex items-center justify-center size-5 rounded-full bg-emerald-500/15 text-emerald-500">
                <Check className="size-3" />
              </div>
              <span className="text-body font-medium">{t(($) => $.create_task.toast_created)}</span>
            </div>
            <div className="flex items-center gap-2 text-body text-muted-foreground ml-7">
              <StatusIcon status={task.status} className="size-3.5 shrink-0" />
              <span className="truncate">{task.identifier} – {task.title}</span>
            </div>
            <button
              type="button"
              className="ml-7 mt-2 text-body text-primary hover:underline cursor-pointer"
              onClick={() => {
                router.push(p.taskDetail(task.id));
                toast.dismiss(toastId);
              }}
            >
              {t(($) => $.create_task.view_task)}
            </button>
          </div>
        ), { duration: 5000 });
      }
      return true;
    } catch (err) {
      // Duplicate-task is the only structured 409 the create endpoint
      // returns. We schema-guard the body (ApiError.body is `unknown`) so a
      // future server-side rename / drop of `code` / `task` degrades to the
      // normal error toast instead of throwing inside the toast renderer.
      if (err instanceof ApiError && err.status === 409) {
        const dup = parseWithFallback<DuplicateTaskErrorBody | null>(
          err.body,
          DuplicateTaskErrorBodySchema,
          null,
          { endpoint: "POST /api/workspaces/:wsId/tasks (active_duplicate_task)" },
        );
        if (dup) {
          toast.custom(
            (toastId) => (
              <div className="bg-popover text-popover-foreground border rounded-lg shadow-lg p-4 w-[360px]">
                <div className="flex items-center gap-2 mb-2">
                  <div className="flex items-center justify-center size-5 rounded-full bg-amber-500/15 text-amber-500">
                    <AlertTriangle className="size-3" />
                  </div>
                  <span className="text-body font-medium">
                    {t(($) => $.create_task.toast_duplicate_title)}
                  </span>
                </div>
                <div className="flex items-center gap-2 text-body text-muted-foreground ml-7">
                  <span className="truncate">{dup.task.identifier} – {dup.task.title}</span>
                </div>
                <button
                  type="button"
                  className="ml-7 mt-2 text-body text-primary hover:underline cursor-pointer"
                  onClick={() => {
                    router.push(p.taskDetail(dup.task.id));
                    toast.dismiss(toastId);
                  }}
                >
                  {t(($) => $.create_task.toast_duplicate_view)}
                </button>
              </div>
            ),
            { duration: 5000 },
          );
          return false;
        }
      }
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.create_task.toast_failed),
      );
      return false;
    }
  },
    onAccepted: () => {
      // These preferences derive from the SUBMITTED values, not the live
      // draft — an task was created, so record them regardless of the guard.
      setLastAssignee(assigneeType, assigneeId);
      setLastMode("manual");
      // Success may only consume the draft it submitted (SILI-5181 P0): any
      // edit after the submit snapshot — typing while the request is in
      // flight, or a reopened dialog — survives, and the dialog then stays
      // open on the newer draft instead of closing/resetting over it. Flush
      // the editor's pending debounce first so mid-flight typing still inside
      // the debounce window is judged correctly.
      const lateDesc = descEditorRef.current?.flushPendingUpdate?.();
      if (lateDesc != null) setManual({ description: lateDesc });
      const untouched =
        useTaskDraftStore.getState().draft === submittedDraftRef.current;
      if (untouched) clearDraft();
      if (!mountedRef.current || !untouched) return;
      if (keepOpen) {
        resetForNextTask();
      } else {
        onClose();
      }
    },
  });

  // Button + shortcut entry point. The title-empty case can't rely on the
  // button tooltip (shortcuts bypass the button), so focus the title to point
  // at the fix; otherwise hand off to the composer (single-flight + gate live
  // there).
  const handleSubmit = () => {
    if (!title.trim()) {
      titleEditorRef.current?.focus();
      return;
    }
    void composer.submit();
  };
  const submitting = composer.submitting;

  // Switch to agent mode WITHOUT destroying the manual draft. The manual slot
  // (title, description, …) is left untouched so a later agent→manual flip
  // restores it verbatim. Project / priority / due date already live in the
  // shared slot, so they carry across for free. Only two things are handed to
  // the agent panel:
  //   1. A one-time assist-init of the agent prompt / actor: when the agent
  //      draft is still empty, seed the prompt from title + description and the
  //      actor from the manual assignee (if agent-like). An existing agent
  //      draft is preserved — no repeated concatenate-then-clobber.
  //   2. The parent-task context, which is not persisted in the draft (it is a
  //      per-invocation intent from "Add sub task"), so it rides the carry.
  const switchToAgent = () => {
    // Serializing mid-upload packs a description that has already lost the
    // pending image into the agent prompt, so gate the switch too.
    if (gate.isBlocked()) return;
    // Commit the shared fields to the draft so the agent panel reads them from
    // there. Local state can hold a value seeded from `data` (e.g. an opener's
    // project) that was never written through a picker, so a plain flip would
    // otherwise drop it.
    setShared({ projectId, priority, dueDate });
    const existingPrompt = draft.agent.prompt;
    if (!existingPrompt.trim()) {
      const desc = descEditorRef.current?.getMarkdown()?.trim() ?? "";
      const seeded = [title.trim(), desc].filter(Boolean).join("\n\n");
      if (seeded) setAgent({ prompt: seeded });
    }
    if (
      !draft.agent.actorId &&
      assigneeId &&
      (assigneeType === "agent" || assigneeType === "squad")
    ) {
      setAgent({ actorType: assigneeType, actorId: assigneeId });
    }
    setLastMode("agent");
    setActiveMode("agent");
    // Prefer the hydrated identifier from `parentTask`, but fall back to the
    // identifier the modal opener seeded on `data`. Without the fallback, a
    // flip that happens before the task detail query resolves drops the
    // identifier and the agent chip renders as "Sub-task of " with an empty
    // tail. The UUID alone still wires the sub-task relationship correctly;
    // this only affects the display affordance.
    const carryParentIdentifier =
      parentTask?.identifier ?? (data?.parent_task_identifier as string | undefined);
    const carry: Record<string, unknown> = {};
    if (parentTaskId) carry.parent_task_id = parentTaskId;
    if (carryParentIdentifier) carry.parent_task_identifier = carryParentIdentifier;
    if (workflowInstanceId) carry.workflow_instance_id = workflowInstanceId;
    if (workflowStageId) carry.workflow_stage_id = workflowStageId;
    onSwitchMode?.(Object.keys(carry).length > 0 ? carry : null);
  };

  // One state for the button and the keyboard paths, so a rendered affordance
  // can never disagree with what `handleSubmit` will actually do.
  const submitState: "submitting" | "uploading" | "missing_title" | "ready" =
    submitting
      ? "submitting"
      : gate.uploading
        ? "uploading"
        : !title.trim()
          ? "missing_title"
          : "ready";
  const submitBusy = submitState === "submitting" || submitState === "uploading";

  // Built once and reused by both footer branches: rendering a separate Button
  // per branch is how the keycaps drifted out of one of them before.
  const createButton = (
    <Button
      size="sm"
      onClick={handleSubmit}
      // Native `disabled` for the transient busy states, but `aria-disabled`
      // for a missing title — a native-disabled button is not focusable, so
      // keyboard and screen-reader users could never reach the tooltip that
      // explains why nothing happens. `handleSubmit` is the real gate either way.
      disabled={submitBusy}
      aria-disabled={submitState === "missing_title" || undefined}
      aria-busy={submitBusy || undefined}
      // The Button base only dims/blocks on native `disabled`, so aria-disabled
      // would otherwise stay a fully lit, pressable-looking primary button.
      // Deliberately no `pointer-events-none`: this control still has to hover
      // its tooltip and take the click that focuses the title.
      className="aria-disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:active:translate-y-0"
    >
      {submitState === "submitting" ? (
        t(($) => $.create_task.submitting)
      ) : submitState === "uploading" ? (
        tEditor(($) => $.upload.in_progress)
      ) : (
        <>
          {t(($) => $.create_task.submit)}
          {/* Decorative: the accessible name must stay "Create Task", not
              "Create Task Command Enter". Absent when `send` is unbound. */}
          {sendShortcut ? (
            <ShortcutKeycaps
              shortcut={sendShortcut}
              decorative
              className="ml-1"
              keyClassName="border-background/30 bg-background/15 text-primary-foreground shadow-none"
            />
          ) : null}
        </>
      )}
    </Button>
  );

  return (
    <>
            <DialogTitle className="sr-only">{t(($) => $.create_task.sr_manual)}</DialogTitle>

            {/* Header */}
            <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
              <div className="flex items-center gap-1.5 text-caption">
                <span className="text-muted-foreground">{workspaceName}</span>
                <ChevronRight className="size-3 text-muted-foreground/50" />
                <span className="font-medium">{t(($) => $.create_task.manual_breadcrumb)}</span>
              </div>
              <div className="flex items-center gap-1">
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        type="button"
                        onClick={() => setIsExpanded(!isExpanded)}
                        className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                      >
                        {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
                      </button>
                    }
                  />
                  <TooltipContent side="bottom">
                    {isExpanded
                      ? t(($) => $.common.collapse_tooltip)
                      : t(($) => $.common.expand_tooltip)}
                  </TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        type="button"
                        onClick={onClose}
                        className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
                      >
                        <XIcon className="size-4" />
                      </button>
                    }
                  />
                  <TooltipContent side="bottom">{t(($) => $.common.close)}</TooltipContent>
                </Tooltip>
              </div>
            </div>

            {/* Title */}
            <div className="px-5 pb-2 shrink-0">
              <TitleEditor
                key={formResetKey}
                ref={titleEditorRef}
                autoFocus
                defaultValue={draft.manual.title}
                placeholder={t(($) => $.create_task.title_placeholder)}
                className="text-title font-semibold"
                onChange={(v) => updateTitle(v)}
                // Chord only — plain Enter still just ends title editing (#5532).
                onSubmitShortcut={handleSubmit}
              />
            </div>

            {/* Description — takes remaining space */}
            <div {...descDropZoneProps} className="relative flex flex-1 min-h-0 overflow-y-auto px-5">
              <ContentEditor
                ref={descEditorRef}
                defaultValue={draft.manual.description}
                placeholder={t(($) => $.create_task.description_placeholder)}
                onUpdate={(md) => setManual({ description: md })}
                onSubmit={handleSubmit}
                onUploadFile={handleUpload}
                onUploadingChange={uploadGate.onUploadingChange}
                debounceMs={500}
                attachments={draftAttachments}
                enableResourceMentions
                className="resource-inline-content"
              />
              {descDragOver && <FileDropOverlay />}
            </div>


            {/* Pre-trigger preview — a passive caption above the toolbar; reveals
                when an agent assignee will pick the task up. */}
            <CreateRunHint assigneeType={assigneeType} assigneeId={assigneeId} status={status} />

            {/* Property toolbar — each field renders per the Settings → Task
                selection (see showField above). */}
            <div className="flex items-center gap-1.5 px-4 py-2 shrink-0 flex-wrap">
              {/* Status */}
              {showField.status && (
                <StatusPicker
                  status={status}
                  onUpdate={(u) => { if (u.status) updateStatus(u.status); }}
                  triggerRender={<PillButton />}
                  align="start"
                  open={fieldPickerOpen === "status" ? true : undefined}
                  onOpenChange={(open) => setFieldPickerOpen(open ? "status" : null)}
                />
              )}

              {/* Priority */}
              {showField.priority && (
                <PriorityPicker
                  priority={priority}
                  onUpdate={(u) => { if (u.priority) updatePriority(u.priority); }}
                  triggerRender={<PillButton />}
                  align="start"
                  open={fieldPickerOpen === "priority" ? true : undefined}
                  onOpenChange={(open) => setFieldPickerOpen(open ? "priority" : null)}
                />
              )}

              {/* Assignee */}
              {showField.assignee && (
                <AssigneePicker
                  assigneeType={assigneeType ?? null}
                  assigneeId={assigneeId ?? null}
                  onUpdate={(u) => updateAssignee(
                    u.assignee_type ?? undefined,
                    u.assignee_id ?? undefined,
                  )}
                  triggerRender={<PillButton />}
                  align="start"
                  open={fieldPickerOpen === "assignee" ? true : undefined}
                  onOpenChange={(open) => setFieldPickerOpen(open ? "assignee" : null)}
                />
              )}

              {/* Labels — occupies the slot that used to hold Due date so the
                  add-label entry is exposed directly on the dialog. Draft mode:
                  selection is local until the task is created (handleSubmit
                  attaches the labels afterward). */}
              {showField.labels && (
                <LabelPicker
                  selectedIds={labelIds}
                  onSelectedIdsChange={updateLabelIds}
                  triggerRender={<PillButton />}
                  align="start"
                  open={fieldPickerOpen === "labels" ? true : undefined}
                  onOpenChange={(open) => setFieldPickerOpen(open ? "labels" : null)}
                />
              )}

              {/* Project */}
              {showField.project && (
                <ProjectPicker
                  projectId={projectId ?? null}
                  onUpdate={(u) => updateProject(u.project_id ?? undefined)}
                  triggerRender={<PillButton />}
                  align="start"
                  open={fieldPickerOpen === "project" ? true : undefined}
                  onOpenChange={(open) => setFieldPickerOpen(open ? "project" : null)}
                />
              )}

              <WorkflowAssignmentPicker
                projectId={projectId}
                workflowInstanceId={workflowInstanceId}
                workflowStageId={workflowStageId}
                onChange={(assignment) => {
                  setWorkflowInstanceId(assignment.workflowInstanceId);
                  setWorkflowStageId(assignment.workflowStageId);
                }}
              />

              {/* Stage — only relevant when creating a sub-task under a parent */}
              {parentTaskId && (
                <StagePicker
                  stage={stage}
                  onUpdate={(u) => setStage(u.stage ?? null)}
                  maxStage={maxSiblingStage(parentChildren)}
                  triggerRender={<PillButton />}
                  align="start"
                />
              )}

              {/* Start date — collapsed into the ⋯ menu by default since it's
                  a low-frequency field (exposable via Settings → Task).
                  Renders inline when configured visible, when the field has a
                  value, OR when the user just opened it from the overflow
                  menu (the picker's calendar popover needs the inline pill
                  as its anchor). */}
              {showField.start_date && (
                <StartDatePicker
                  startDate={startDate}
                  onUpdate={(u) => updateStartDate(u.start_date ?? null)}
                  triggerRender={<PillButton />}
                  align="start"
                  open={startDatePickerOpen}
                  onOpenChange={setStartDatePickerOpen}
                />
              )}

              {/* Due date — collapsed into the ⋯ menu by default (moved off
                  the toolbar to make room for Labels). Same reveal rule as
                  start date. */}
              {showField.due_date && (
                <DueDatePicker
                  dueDate={dueDate}
                  onUpdate={(u) => updateDueDate(u.due_date ?? null)}
                  triggerRender={<PillButton />}
                  align="start"
                  open={dueDatePickerOpen}
                  onOpenChange={setDueDatePickerOpen}
                />
              )}

              {/* Workspace-defined fields use the same typed editors as task
                  detail, but write into the persisted draft until creation. */}
              {workspaceProperties
                .filter(
                  (property) =>
                    Object.prototype.hasOwnProperty.call(propertyValues, property.id) ||
                    customPropertyPickerId === property.id,
                )
                .map((property) => {
                  const value = propertyValues[property.id];
                  return (
                    <CustomPropertyValueInput
                      key={property.id}
                      property={property}
                      value={value}
                      onChange={(next) => updatePropertyValue(property.id, next)}
                      open={customPropertyPickerId === property.id}
                      onOpenChange={(open) =>
                        setCustomPropertyPickerId(open ? property.id : null)
                      }
                      triggerRender={<PillButton />}
                      trigger={
                        <>
                          <PropertyIcon property={property} className="size-3.5 text-caption" />
                          <span className="max-w-32 truncate">{property.name}</span>
                          {value !== undefined && (
                            <span className="max-w-40 truncate text-muted-foreground">
                              <CustomPropertyValueDisplay property={property} value={value} />
                            </span>
                          )}
                        </>
                      }
                    />
                  );
                })}

              {/* Parent chip — appears when parent is set.
                  Placed before the ⋯ so it wraps to a new line with ⋯ if
                  space is tight, but ⋯ always stays last in DOM order. */}
              {parentTaskId && parentTask && (
                <div className="inline-flex items-center rounded-full border text-caption transition-colors hover:bg-accent/60">
                  <button
                    type="button"
                    onClick={() => setParentPickerOpen(true)}
                    className="flex items-center gap-1.5 py-1 pl-2.5 cursor-pointer"
                  >
                    <ArrowUp className="size-3 text-muted-foreground" />
                    <span>
                      {t(($) => $.create_task.subtask_of, { identifier: parentTask.identifier })}
                    </span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setParentTaskId(undefined)}
                    className="p-1 pr-2 text-muted-foreground hover:text-foreground cursor-pointer"
                    aria-label={t(($) => $.create_task.remove_parent_aria)}
                  >
                    <XIcon className="size-3" />
                  </button>
                </div>
              )}

              {/* Child chips — one per queued sub-task. Links are deferred
                  until create resolves (see handleSubmit). */}
              {childTasks.map((c) => (
                <div
                  key={c.id}
                  className="inline-flex items-center rounded-full border text-caption transition-colors hover:bg-accent/60"
                >
                  <div className="flex items-center gap-1.5 py-1 pl-2.5">
                    <ArrowDown className="size-3 text-muted-foreground" />
                    <span>{t(($) => $.create_task.subtask_chip, { identifier: c.identifier })}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() =>
                      setChildTasks((prev) => prev.filter((x) => x.id !== c.id))
                    }
                    className="p-1 pr-2 text-muted-foreground hover:text-foreground cursor-pointer"
                    aria-label={t(($) => $.create_task.remove_subtask_aria, { identifier: c.identifier })}
                  >
                    <XIcon className="size-3" />
                  </button>
                </div>
              ))}

              {/* Overflow — always the last child so DOM order keeps it at the
                  end of the wrap flow, no matter how many chips are present. */}
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <PillButton aria-label={t(($) => $.create_task.more_options_aria)}>
                      <MoreHorizontal className="size-3.5" />
                    </PillButton>
                  }
                />
                <DropdownMenuContent align="start" className="w-auto">
                  {/* Re-entry points for toolbar fields hidden via
                      Settings → Task. Listed in toolbar order; each opens
                      the picker inline (mounting the pill as its anchor). */}
                  {!showField.status && (
                    <DropdownMenuItem onClick={() => setFieldPickerOpen("status")}>
                      <StatusIcon status={status} className="h-3.5 w-3.5" />
                      {t(($) => $.create_task.set_status)}
                    </DropdownMenuItem>
                  )}
                  {!showField.priority && (
                    <DropdownMenuItem onClick={() => setFieldPickerOpen("priority")}>
                      <PriorityIcon priority="none" className="h-3.5 w-3.5" />
                      {t(($) => $.create_task.set_priority)}
                    </DropdownMenuItem>
                  )}
                  {!showField.assignee && (
                    <DropdownMenuItem onClick={() => setFieldPickerOpen("assignee")}>
                      <CircleUser className="h-3.5 w-3.5" />
                      {t(($) => $.create_task.set_assignee)}
                    </DropdownMenuItem>
                  )}
                  {!showField.labels && (
                    <DropdownMenuItem onClick={() => setFieldPickerOpen("labels")}>
                      <Tag className="h-3.5 w-3.5" />
                      {t(($) => $.create_task.set_labels)}
                    </DropdownMenuItem>
                  )}
                  {!showField.project && (
                    <DropdownMenuItem onClick={() => setFieldPickerOpen("project")}>
                      <FolderKanban className="h-3.5 w-3.5" />
                      {t(($) => $.create_task.set_project)}
                    </DropdownMenuItem>
                  )}
                  {!showField.due_date && (
                    <DropdownMenuItem onClick={() => setDueDatePickerOpen(true)}>
                      <CalendarDays className="h-3.5 w-3.5" />
                      {t(($) => $.create_task.set_due_date)}
                    </DropdownMenuItem>
                  )}
                  {!showField.start_date && (
                    <DropdownMenuItem onClick={() => setStartDatePickerOpen(true)}>
                      <CalendarClock className="h-3.5 w-3.5" />
                      {t(($) => $.create_task.set_start_date)}
                    </DropdownMenuItem>
                  )}
                  {parentTaskId && parentTask ? (
                    <DropdownMenuItem onClick={() => setParentPickerOpen(true)}>
                      <ArrowUp className="h-3.5 w-3.5" />
                      {t(($) => $.create_task.parent_with_id, { identifier: parentTask.identifier })}
                    </DropdownMenuItem>
                  ) : (
                    <DropdownMenuItem onClick={() => setParentPickerOpen(true)}>
                      <ArrowUp className="h-3.5 w-3.5" />
                      {t(($) => $.create_task.set_parent)}
                    </DropdownMenuItem>
                  )}
                  <DropdownMenuItem onClick={() => setChildPickerOpen(true)}>
                    <ArrowDown className="h-3.5 w-3.5" />
                    {t(($) => $.create_task.add_subtask)}
                  </DropdownMenuItem>
                  {workspaceProperties.length > 0 && (
                    <DropdownMenuSub>
                      <DropdownMenuSubTrigger>
                        <Shapes className="h-3.5 w-3.5" />
                        {t(($) => $.create_task.custom_properties)}
                      </DropdownMenuSubTrigger>
                      <DropdownMenuSubContent className="w-56">
                        {workspaceProperties.map((property) => (
                          <DropdownMenuItem
                            key={property.id}
                            disabled={Object.prototype.hasOwnProperty.call(
                              propertyValues,
                              property.id,
                            )}
                            onClick={() => setCustomPropertyPickerId(property.id)}
                          >
                            <PropertyIcon property={property} className="size-3.5 text-caption" />
                            <span className="truncate">{property.name}</span>
                            {Object.prototype.hasOwnProperty.call(
                              propertyValues,
                              property.id,
                            ) && <Check className="ml-auto size-3.5" />}
                          </DropdownMenuItem>
                        ))}
                      </DropdownMenuSubContent>
                    </DropdownMenuSub>
                  )}
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onClick={openFieldSettings}>
                    <Settings2 className="h-3.5 w-3.5" />
                    {t(($) => $.create_task.customize_fields)}
                  </DropdownMenuItem>
                  {parentTaskId && parentTask && (
                    <>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem
                        variant="destructive"
                        onClick={() => setParentTaskId(undefined)}
                      >
                        <XIcon className="h-3.5 w-3.5" />
                        {t(($) => $.create_task.remove_parent)}
                      </DropdownMenuItem>
                    </>
                  )}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>

            {/* Parent / child pickers — rendered inline so they stack over this
                modal instead of replacing it via useModalStore. */}
            <TaskPickerModal
              open={parentPickerOpen}
              onOpenChange={setParentPickerOpen}
              title={t(($) => $.create_task.set_parent_picker.title)}
              description={t(($) => $.create_task.set_parent_picker.description)}
              excludeIds={[
                ...childTasks.map((c) => c.id),
                ...(parentTaskId ? [parentTaskId] : []),
              ]}
              onSelect={(selected) => {
                setParentTaskId(selected.id);
              }}
            />
            <TaskPickerModal
              open={childPickerOpen}
              onOpenChange={setChildPickerOpen}
              title={t(($) => $.create_task.add_subtask_picker.title)}
              description={t(($) => $.create_task.add_subtask_picker.description)}
              excludeIds={[
                ...childTasks.map((c) => c.id),
                ...(parentTaskId ? [parentTaskId] : []),
              ]}
              onSelect={(selected) => {
                setChildTasks((prev) =>
                  prev.some((x) => x.id === selected.id) ? prev : [...prev, selected],
                );
              }}
            />

            {/* Footer */}
            <div className="flex flex-col gap-2 border-t px-4 py-3 shrink-0 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex min-h-7 items-center gap-2">
                <FileUploadButton
                  multiple
                  onSelect={(file) => descEditorRef.current?.uploadFile(file)}
                />
              </div>
              <div className="flex flex-wrap items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={switchToAgent}
                  disabled={gate.uploading}
                  aria-disabled={gate.uploading || undefined}
                  aria-busy={gate.uploading || undefined}
                  title={t(($) => $.create_task.switch_to_agent_tooltip)}
                  className="border-beam group flex shrink-0 items-center gap-1.5 text-caption px-2 py-1 rounded-sm text-muted-foreground bg-brand/5 hover:bg-brand/10 hover:text-foreground transition-colors cursor-pointer disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <ArrowLeftRight className="size-3.5 text-brand/80 transition-transform duration-300 group-hover:rotate-180" />
                  {t(($) => $.create_task.switch_to_agent)}
                </button>
                <label className="flex shrink-0 items-center gap-1.5 text-caption text-muted-foreground cursor-pointer select-none">
                  <Switch
                    size="sm"
                    checked={keepOpen}
                    onCheckedChange={setKeepOpen}
                  />
                  {t(($) => $.create_task.create_another)}
                </label>
                {submitState === "missing_title" ? (
                  <TooltipProvider delay={200}>
                    <Tooltip>
                      {/* No `<span>` wrapper needed now: aria-disabled leaves the
                          button focusable and hoverable, so it can anchor its own
                          tooltip. */}
                      <TooltipTrigger render={createButton} />
                      <TooltipContent side="top">{t(($) => $.create_task.title_required)}</TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                ) : (
                  createButton
                )}
              </div>
            </div>
    </>
  );
}

/** className for DialogContent in manual mode — depends on isExpanded.
 *  Exported so the shell (which now owns the DialogContent) can apply the same
 *  visual treatment without duplicating it. */
export function manualDialogContentClass(isExpanded: boolean) {
  return cn(
    "p-0 gap-0 flex flex-col overflow-hidden",
    "!top-1/2 !left-1/2 !-translate-x-1/2",
    "!transition-all !duration-300 !ease-out",
    isExpanded
      ? "!max-w-4xl !w-full !h-5/6 !-translate-y-1/2"
      : "!max-w-2xl !w-full !h-96 !-translate-y-1/2",
  );
}

// Thin Dialog-wrapping export — registry mounts the panel directly under the
// shell's shared Dialog, but a few legacy callers (and the test suite) still
// import this module's modal version. Equivalent runtime behavior to the
// pre-refactor component when used standalone.
import { Dialog as DialogRoot } from "@silieco/ui/components/ui/dialog";
export function CreateTaskModal(props: {
  onClose: () => void;
  data?: Record<string, unknown> | null;
}) {
  const [isExpanded, setIsExpanded] = useState(false);
  return (
    <DialogRoot open onOpenChange={(v) => { if (!v) props.onClose(); }}>
      <DialogContent
        finalFocus={false}
        showCloseButton={false}
        className={manualDialogContentClass(isExpanded)}
      >
        <ManualCreatePanel
          {...props}
          isExpanded={isExpanded}
          setIsExpanded={setIsExpanded}
        />
      </DialogContent>
    </DialogRoot>
  );
}
