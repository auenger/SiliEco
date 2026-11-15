"use client";

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeftRight,
  CalendarDays,
  Check,
  ChevronRight,
  FolderKanban,
  Maximize2,
  Minimize2,
  MoreHorizontal,
  Settings2,
  X as XIcon,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { DialogTitle } from "@silieco/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@silieco/ui/components/ui/dropdown-menu";
import { Button } from "@silieco/ui/components/ui/button";
import { Switch } from "@silieco/ui/components/ui/switch";
import { api, ApiError } from "@silieco/core/api";
import { useWorkspaceId } from "@silieco/core/hooks";
import { useCurrentWorkspace, useWorkspacePaths } from "@silieco/core/paths";
import { useNavigation } from "../navigation";
import { agentListOptions, squadListOptions } from "@silieco/core/workspace/queries";
import { projectListOptions } from "@silieco/core/projects/queries";
import {
  useQuickCreateStore,
  type QuickCreateActorType,
} from "@silieco/core/tasks/stores/quick-create-store";
import {
  useTaskCreateSettingsStore,
  type QuickCreateField,
} from "@silieco/core/tasks/stores/task-create-settings-store";
import { useTaskDraftStore, type TaskCreateDraft } from "@silieco/core/tasks/stores/draft-store";
import { useCreateModeStore } from "@silieco/core/tasks/stores/create-mode-store";
import {
  runtimeListOptions,
  checkQuickCreateCliVersion,
  checkQuickCreateFieldsCliVersion,
  readRuntimeCliVersion,
} from "@silieco/core/runtimes";
import { useShortcut } from "@silieco/core/shortcuts";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import {
  attachmentIdsFromContent,
  contentReferencesAttachment,
  type Agent,
  type TaskPriority,
  type Squad,
} from "@silieco/core/types";
import { ActorAvatar } from "../common/actor-avatar";
import { PillButton } from "../common/pill-button";
import { ProjectPicker } from "../projects/components/project-picker";
import { WorkflowAssignmentPicker } from "../workflows/components";
import { DueDatePicker, PriorityIcon, PriorityPicker } from "../tasks/components";
import { canAssignAgent } from "../tasks/components/pickers/assignee-picker";
import {
  PropertyPicker,
  PickerItem,
  PickerSection,
  PickerEmpty,
} from "../tasks/components/pickers/property-picker";
import { useAuthStore } from "@silieco/core/auth";
import { memberListOptions } from "@silieco/core/workspace/queries";
import {
  ContentEditor,
  type ContentEditorRef,
  useFileDropZone,
  FileDropOverlay,
  useUploadGate,
  useComposerSubmit,
} from "../editor";
import { useTaskCreateUploads } from "./use-task-create-uploads";
import { FileUploadButton } from "@silieco/ui/components/common/file-upload-button";
import { useT } from "../i18n";
import { matchesPinyin } from "../editor/extensions/pinyin-match";

type ActorSelection =
  | { type: "agent"; id: string }
  | { type: "squad"; id: string };

// AgentCreatePanel — agent-mode body of the create-task dialog. Renders
// only the inner content; the surrounding `<Dialog>` AND `<DialogContent>`
// (Portal + Overlay + Popup) are owned by CreateTaskDialog so mode-switching
// swaps only this body. Lifting the Portal is what eliminates the close→open
// animation flash — Base UI replays Popup enter/exit when DialogContent is
// remounted, even inside a still-open Dialog Root.
//
// `onSwitchMode` is wired by the shell — the panel calls it with an optional
// carry payload (currently `project_id`). The shared draft store carries the
// description + agent across the agent→manual flip; project_id rides through
// the same carry channel manual→agent uses, so the manual panel reads it
// from `data?.project_id` without a parallel store.
export function AgentCreatePanel({
  onClose,
  onSwitchMode,
  data,
  isExpanded,
  setIsExpanded,
}: {
  onClose: () => void;
  onSwitchMode?: (carry?: Record<string, unknown> | null) => void;
  data?: Record<string, unknown> | null;
  /** Lifted to the shell so DialogContent's mode-aware className can react —
   *  same pattern as ManualCreatePanel. Shared across modes so the user's
   *  expand preference persists when switching between agent and manual. */
  isExpanded: boolean;
  setIsExpanded: (v: boolean) => void;
}) {
  const { t } = useT("modals");
  const sendShortcut = useShortcut("send");
  const workspaceName = useCurrentWorkspace()?.name;
  const workspacePaths = useWorkspacePaths();
  const navigation = useNavigation();
  const wsId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  // Pull `isSuccess` so the stale-id sweep below can distinguish "still
  // loading" from "loaded as empty". Reading length alone treats both as
  // empty and incorrectly clears a valid persisted preference on every open.
  const { data: projects = [], isSuccess: projectsLoaded } = useQuery(
    projectListOptions(wsId),
  );

  const memberRole = useMemo(
    () => members.find((m) => m.user_id === userId)?.role,
    [members, userId],
  );

  // Visible = not archived AND assignable by this user. Squads inherit
  // their leader agent's reachability: the backend always routes a squad
  // pick to the leader, so hiding squads whose leader isn't visible keeps
  // the picker honest with what the server would actually accept.
  const visibleAgents = useMemo(
    () =>
      agents.filter(
        (a) => !a.archived_at && canAssignAgent(a, userId, memberRole),
      ),
    [agents, userId, memberRole],
  );
  const visibleAgentIds = useMemo(
    () => new Set(visibleAgents.map((a) => a.id)),
    [visibleAgents],
  );
  const visibleSquads = useMemo(
    () =>
      squads.filter(
        (s) => !s.archived_at && visibleAgentIds.has(s.leader_id),
      ),
    [squads, visibleAgentIds],
  );

  const lastActorType = useQuickCreateStore((s) => s.lastActorType);
  const lastActorId = useQuickCreateStore((s) => s.lastActorId);
  const setLastActor = useQuickCreateStore((s) => s.setLastActor);
  const lastProjectId = useQuickCreateStore((s) => s.lastProjectId);
  const setLastProjectId = useQuickCreateStore((s) => s.setLastProjectId);
  const visibleFields = useTaskCreateSettingsStore((s) => s.quickCreateFields);
  const keepOpen = useQuickCreateStore((s) => s.keepOpen);
  const setKeepOpen = useQuickCreateStore((s) => s.setKeepOpen);
  const setLastMode = useCreateModeStore((s) => s.setLastMode);
  // The agent draft (prompt + actor) and the shared fields (project, priority,
  // due date, attachments) live in the unified task-create draft, so a switch
  // to/from the manual form preserves them.
  const draft = useTaskDraftStore((s) => s.draft);
  const setShared = useTaskDraftStore((s) => s.setShared);
  const setManual = useTaskDraftStore((s) => s.setManual);
  const setAgent = useTaskDraftStore((s) => s.setAgent);
  const setActiveMode = useTaskDraftStore((s) => s.setActiveMode);
  const clearDraft = useTaskDraftStore((s) => s.clearDraft);

  // Resolve a candidate actor against the currently-visible agents / squads.
  // Returns null when the candidate doesn't exist in this workspace right
  // now (deleted, archived, permission revoked, etc.) so callers can fall
  // through to the next seed in the chain.
  const resolveActor = useCallback(
    (
      type: QuickCreateActorType | "agent" | "squad" | null | undefined,
      id: string | null | undefined,
    ): ActorSelection | null => {
      if (!type || !id) return null;
      if (type === "squad" && visibleSquads.some((s) => s.id === id)) {
        return { type: "squad", id };
      }
      if (type === "agent" && visibleAgentIds.has(id)) {
        return { type: "agent", id };
      }
      return null;
    },
    [visibleSquads, visibleAgentIds],
  );

  const seedActor = useCallback((): ActorSelection | null => {
    // Caller-provided seed wins (e.g. shell pre-seeds with `agent_id` /
    // `squad_id`), then the persisted agent draft, the last successful pick,
    // and finally the first visible agent.
    const dataAgent = data?.agent_id as string | undefined;
    const dataSquad = data?.squad_id as string | undefined;
    return (
      resolveActor("agent", dataAgent) ||
      resolveActor("squad", dataSquad) ||
      resolveActor(draft.agent.actorType, draft.agent.actorId) ||
      resolveActor(lastActorType, lastActorId) ||
      (visibleAgents[0]
        ? ({ type: "agent", id: visibleAgents[0].id } as const)
        : null)
    );
  }, [
    resolveActor,
    data?.agent_id,
    data?.squad_id,
    draft.agent.actorType,
    draft.agent.actorId,
    lastActorType,
    lastActorId,
    visibleAgents,
  ]);

  const [actor, setActor] = useState<ActorSelection | null>(() => seedActor());

  // Re-seed once visible lists resolve (queries may be empty on first render).
  useEffect(() => {
    if (actor && resolveActor(actor.type, actor.id)) return;
    setActor(seedActor());
  }, [actor, resolveActor, seedActor]);

  const selectedAgent = useMemo<Agent | undefined>(() => {
    if (!actor) return undefined;
    if (actor.type === "agent") return visibleAgents.find((a) => a.id === actor.id);
    const squad = visibleSquads.find((s) => s.id === actor.id);
    if (!squad) return undefined;
    return visibleAgents.find((a) => a.id === squad.leader_id);
  }, [actor, visibleAgents, visibleSquads]);

  const selectedSquad = useMemo<Squad | undefined>(() => {
    if (actor?.type !== "squad") return undefined;
    return visibleSquads.find((s) => s.id === actor.id);
  }, [actor, visibleSquads]);

  // Unfinished selections live in the shared task-create draft. Last-successful
  // actor/project values remain separate fallbacks, so closing a draft never
  // overwrites the defaults established by an actual create.
  const [projectId, setProjectId] = useState<string | null>(() => {
    const seed =
      (data?.project_id as string | undefined) ??
      draft.shared.projectId ??
      lastProjectId;
    return seed ?? null;
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
  const [priority, setPriority] = useState<TaskPriority>(
    (data?.priority as TaskPriority | undefined) ?? draft.shared.priority,
  );
  const [dueDate, setDueDate] = useState<string | null>(
    (data?.due_date as string | undefined) ?? draft.shared.dueDate,
  );
  const [fieldPickerOpen, setFieldPickerOpen] = useState<QuickCreateField | null>(null);

  // Parent-task context — seeded by `openCreateSubTask` when the modal is
  // opened from the "Add sub task" entry on an existing task. We carry it
  // through (not as an editable form field) so a manual→agent flip preserves
  // the sub-task intent; the agent panel never exposes this as a picker.
  // Identifier is best-effort display context only — the UUID is the
  // authoritative reference the backend/agent uses for `--parent <uuid>`.
  const parentTaskId = (data?.parent_task_id as string | undefined) ?? undefined;
  const parentTaskIdentifier =
    (data?.parent_task_identifier as string | undefined) ?? undefined;

  // Stale-id sweep. Once the project list query has actually resolved
  // (`isSuccess` — distinct from "data is the empty default during loading"),
  // a `projectId` that isn't in the list means the project was deleted in
  // another session. Clear local state, the unfinished draft, and the
  // last-successful preference; dropping any persisted copy would make the
  // next open re-seed and submit the same dead value.
  useEffect(() => {
    if (!projectsLoaded || projectId === null) return;
    if (projects.some((p) => p.id === projectId)) return;
    setProjectId(null);
    setWorkflowInstanceId(undefined);
    setWorkflowStageId(undefined);
    if (draft.shared.projectId === projectId) {
      setShared({ projectId: undefined });
    }
    if (lastProjectId === projectId) setLastProjectId(null);
  }, [
    projectsLoaded,
    projects,
    projectId,
    draft.shared.projectId,
    lastProjectId,
    setShared,
    setLastProjectId,
  ]);

  // Mark the persisted draft's active mode so a later reopen and any reader of
  // the unified draft know which form is being edited.
  useEffect(() => {
    setActiveMode("agent");
  }, [setActiveMode]);

  // Daemon CLI version gate. The agent-create flow needs the runtime's
  // bundled silieco CLI to be ≥ MIN_QUICK_CREATE_CLI_VERSION; older
  // daemons handle attachments and partial-failure retries incorrectly
  // (see PR #1851 / SILI-1496). Pre-check on the picker so the user gets
  // immediate feedback instead of waiting for the inbox failure; the
  // server re-validates as the trust boundary. Dev-built daemons
  // (git-describe shape) are exempted inside checkQuickCreateCliVersion
  // — frontend and server share the same signal there, so they agree by
  // construction across web/desktop/staging without comparing env flags.
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const selectedRuntime = useMemo(
    () =>
      selectedAgent?.runtime_id
        ? runtimes.find((r) => r.id === selectedAgent.runtime_id)
        : undefined,
    [runtimes, selectedAgent?.runtime_id],
  );
  const runtimeCliVersion = readRuntimeCliVersion(selectedRuntime?.metadata);
  const baseVersionCheck = useMemo(
    () => checkQuickCreateCliVersion(runtimeCliVersion),
    [runtimeCliVersion],
  );
  const fieldVersionCheck = useMemo(
    () => checkQuickCreateFieldsCliVersion(runtimeCliVersion),
    [runtimeCliVersion],
  );
  const usesExplicitFields = priority !== "none" || dueDate !== null;
  const versionCheck = usesExplicitFields ? fieldVersionCheck : baseVersionCheck;
  const versionBlocked =
    baseVersionCheck.state !== "ok" ||
    (usesExplicitFields && fieldVersionCheck.state !== "ok");

  const initialPrompt = draft.agent.prompt || (data?.prompt as string) || "";
  // The editor is uncontrolled — we read the latest markdown via the ref at
  // submit/switch time. `hasContent` mirrors emptiness so the Create button
  // can disable correctly without a controlled-input rerender on every keystroke.
  const editorRef = useRef<ContentEditorRef>(null);
  const [hasContent, setHasContent] = useState(initialPrompt.trim().length > 0);
  const [justSent, setJustSent] = useState(false);
  const [sentCount, setSentCount] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const uploadGate = useUploadGate(editorRef);
  // Coordinator-owned uploads in the shared draft pool (SILI-5181, L2): a file
  // pasted into the prompt survives dialog close and mode switches, aborts on
  // logout, and is dropped after a reload. `gate` widens the editor gate with
  // the pool's placeholders.
  const {
    attachments: pendingAttachments,
    handleUpload: handleUploadFile,
    gate,
  } = useTaskCreateUploads("agent", uploadGate, editorRef);
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: (files) => files.forEach((f) => editorRef.current?.uploadFile(f)),
  });

  useEffect(() => {
    // Defer focus so it lands after the dialog's focus trap has settled —
    // otherwise the trap can bounce focus back to the first focusable header
    // button on the next tick.
    const id = requestAnimationFrame(() => editorRef.current?.focus());
    return () => cancelAnimationFrame(id);
  }, []);

  // Agent create runs through the shared await-then-render composer contract
  // (single-flight ref, submit-time upload re-check, lock+spin, await→boolean,
  // clear only on acceptance). The prompt IS the editor content, so this maps
  // onto the hook directly.
  // Stale-submit guard (SILI-5181 P0): the task draft is a SINGLETON store.
  // A late success from a dialog the user closed mid-submit must not clear a
  // newer draft typed after reopening — see ManualCreatePanel for the rule.
  const mountedRef = useRef(true);
  useLayoutEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const submittedDraftRef = useRef<TaskCreateDraft | null>(null);
  // Set by `onAccepted` only on the branch that keeps the panel open and wipes
  // the editor; read back by `afterAccepted`. The closing branch must not
  // refocus an editor that is about to unmount with the dialog.
  const refocusAfterAcceptRef = useRef(false);

  const composer = useComposerSubmit({
    editorRef,
    uploadGate: gate,
    onSubmit: async (md): Promise<boolean> => {
      // The button already disables on !actor / versionBlocked, but the
      // ⌘+Enter path bypasses it — re-guard here and keep the draft in place.
      if (!actor || versionBlocked) return false;
      if (workflowInstanceId && !workflowStageId) {
        toast.error(t(($) => $.create_task.stage_required));
        return false;
      }
      // Flush the prompt editor's pending debounce before snapshotting — see
      // ManualCreatePanel.
      const pendingPrompt = editorRef.current?.flushPendingUpdate?.();
      if (pendingPrompt != null) setAgent({ prompt: pendingPrompt });
      submittedDraftRef.current = useTaskDraftStore.getState().draft;
      const activeAttachmentIds = pendingAttachments
        .filter((a) => contentReferencesAttachment(md, a))
        .map((a) => a.id);
      const allAttachmentIds = [...new Set([
        ...activeAttachmentIds,
        ...attachmentIdsFromContent(md),
      ])];
      setError(null);
      try {
        await api.quickCreateTask({
          ...(actor.type === "agent"
            ? { agent_id: actor.id }
            : { squad_id: actor.id }),
          prompt: md,
          project_id: projectId ?? undefined,
          ...(workflowInstanceId && workflowStageId
            ? {
                workflow_instance_id: workflowInstanceId,
                workflow_stage_id: workflowStageId,
              }
            : {}),
          ...(priority !== "none" ? { priority } : {}),
          ...(dueDate ? { due_date: dueDate } : {}),
          parent_task_id: parentTaskId,
          ...(allAttachmentIds.length > 0 ? { attachment_ids: allAttachmentIds } : {}),
        });
        setLastActor(actor.type, actor.id);
        setLastProjectId(projectId);
        setLastMode("agent");
        toast.success(t(($) => $.create_task.agent.toast_sent), {
          duration: 4000,
        });
        return true;
      } catch (e) {
        // Server returns 422 with { code, ... } for the structured rejection
        // paths the modal cares about. Surface the reason in-modal so the
        // user can switch to a live agent / upgrade their daemon without
        // leaving the flow.
        if (e instanceof ApiError && e.body && typeof e.body === "object") {
          const body = e.body as {
            code?: string;
            reason?: string;
            current_version?: string;
            min_version?: string;
          };
          if (body.code === "agent_unavailable") {
            setError(body.reason || t(($) => $.create_task.agent.error_agent_unavailable_fallback));
            return false;
          }
          if (body.code === "daemon_version_unsupported") {
            // Race fallback: the picker pre-check should normally catch this,
            // but a runtime can silently re-register with an older CLI between
            // pre-check and submit. Same wording as the inline notice for
            // consistency.
            const cur = body.current_version || "unknown";
            setError(
              t(($) => $.create_task.agent.error_daemon_version, {
                current: cur,
                min: body.min_version || versionCheck.min,
              }),
            );
            return false;
          }
        }
        setError(
          e instanceof Error && e.message
            ? e.message
            : t(($) => $.create_task.agent.error_unknown),
        );
        return false;
      }
    },
    // Continuous-creation mode puts the caret back so the next prompt can be
    // typed immediately. Deliberately no `containerRef`: this is a dialog whose
    // own focus trap already bounds where focus can be, and reclaiming it is the
    // point of keep-open mode.
    afterAccepted: () => (refocusAfterAcceptRef.current ? "refocus" : "none"),
    onAccepted: () => {
      refocusAfterAcceptRef.current = false;
      // A successful create ends this whole draft (shared + manual + agent);
      // last-successful actor/project preferences were saved in onSubmit.
      // Success may only consume the draft it submitted: flush the editor's
      // pending debounce first, then clear only an untouched draft — edits
      // made mid-flight or by a reopened dialog survive.
      const latePrompt = editorRef.current?.flushPendingUpdate?.();
      if (latePrompt != null) setAgent({ prompt: latePrompt });
      const untouched =
        useTaskDraftStore.getState().draft === submittedDraftRef.current;
      if (untouched) clearDraft();
      // An edit made during the request survives; the panel stays open on it.
      if (!mountedRef.current || !untouched) return;
      if (keepOpen) {
        // Stay open for continuous creation — clear the editor so the user can
        // immediately type the next prompt.
        editorRef.current?.clearContent();
        setHasContent(false);
        setSentCount((c) => c + 1);
        setJustSent(true);
        setTimeout(() => setJustSent(false), 1500);
        refocusAfterAcceptRef.current = true;
      } else {
        onClose();
      }
    },
  });
  const submit = () => {
    void composer.submit();
  };
  const submitting = composer.submitting;

  // Switch to the manual form WITHOUT destroying the agent draft. The agent
  // slot (prompt + actor) is left untouched so a later manual→agent flip
  // restores it verbatim. Project / priority / due date already live in the
  // shared slot and carry across for free. Two one-time assist-inits run only
  // when the manual slot is still empty: seed the description from the prompt
  // and the assignee from the picked actor. The parent-task context is not
  // persisted (a per-invocation intent) so it rides the carry channel.
  const switchToManual = () => {
    // The prompt is copied into the manual description on assist-init; mid-upload
    // that body has already lost the pending image (see switchToAgent).
    if (gate.isBlocked()) return;
    // Commit the shared fields to the draft so the manual panel reads them from
    // there — local state can hold a value seeded from `data` that was never
    // written through a picker.
    setShared({ projectId: projectId ?? undefined, priority, dueDate });
    if (!draft.manual.description.trim()) {
      const md = editorRef.current?.getMarkdown() ?? "";
      if (md) setManual({ description: md });
    }
    if (!draft.manual.assigneeId && actor) {
      setManual({ assigneeType: actor.type, assigneeId: actor.id });
    }
    setLastMode("manual");
    setActiveMode("manual");
    const carry: Record<string, unknown> = {};
    if (parentTaskId) carry.parent_task_id = parentTaskId;
    if (parentTaskIdentifier) carry.parent_task_identifier = parentTaskIdentifier;
    if (workflowInstanceId) carry.workflow_instance_id = workflowInstanceId;
    if (workflowStageId) carry.workflow_stage_id = workflowStageId;
    onSwitchMode?.(Object.keys(carry).length > 0 ? carry : null);
  };

  // Field visibility lives in Settings → Task. Persist the prompt draft
  // before leaving so what the user typed survives the round-trip, then
  // close — the dialog would otherwise linger over the settings page.
  const openFieldSettings = () => {
    setAgent({ prompt: editorRef.current?.getMarkdown() ?? "" });
    onClose();
    navigation.push(`${workspacePaths.settings()}?tab=task`);
  };

  return (
    <>
        <DialogTitle className="sr-only">{t(($) => $.create_task.sr_agent)}</DialogTitle>

        {/* Header */}
        <div className="flex items-center justify-between px-5 pt-3 pb-2 shrink-0">
          <div className="flex items-center gap-1.5 text-caption">
            <span className="text-muted-foreground">{workspaceName}</span>
            <ChevronRight className="size-3 text-muted-foreground/50" />
            <span className="font-medium">{t(($) => $.create_task.agent_breadcrumb)}</span>
          </div>
          {/* Native `title` instead of Base UI Tooltip — Tooltip opens on
              keyboard focus, and the dialog's focus trap briefly lands focus
              on the first focusable element on mount, causing the tooltip to
              auto-pop every open. Same workaround applies to expand. */}
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => setIsExpanded(!isExpanded)}
              title={isExpanded ? t(($) => $.common.collapse_tooltip) : t(($) => $.common.expand_tooltip)}
              aria-label={isExpanded ? t(($) => $.common.collapse_tooltip) : t(($) => $.common.expand_tooltip)}
              className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
            >
              {isExpanded ? <Minimize2 className="size-4" /> : <Maximize2 className="size-4" />}
            </button>
            <button
              type="button"
              onClick={onClose}
              title={t(($) => $.common.close)}
              aria-label={t(($) => $.common.close)}
              className="rounded-sm p-1.5 opacity-70 hover:opacity-100 hover:bg-accent/60 transition-all cursor-pointer"
            >
              <XIcon className="size-4" />
            </button>
          </div>
        </div>

        {/* Actor picker — agents and squads in one searchable list. Squads
            route to their leader agent on the backend; the leader runs the
            quick-create flow with the squad's Operating Protocol layered
            on top, so a squad pick is "ask this squad to file the task". */}
        <div className="px-5 pt-1 pb-2 shrink-0">
          <ActorPicker
            actor={actor}
            visibleAgents={visibleAgents}
            visibleSquads={visibleSquads}
            selectedAgent={selectedAgent}
            selectedSquad={selectedSquad}
            onPick={(next) => {
              setActor(next);
              setAgent({ actorType: next.type, actorId: next.id });
              setError(null);
            }}
            t={t}
          />
        </div>

        {selectedAgent && versionBlocked && (
          <div className="mx-5 mb-2 shrink-0 rounded-md border border-amber-500/30 bg-amber-500/5 px-3 py-2 text-caption text-amber-700 dark:text-amber-300">
            {versionCheck.state === "missing"
              ? t(($) => $.create_task.agent.version_missing, { min: versionCheck.min })
              : t(($) => $.create_task.agent.version_below, {
                  current: versionCheck.current,
                  min: versionCheck.min,
                })}
          </div>
        )}

        {/* Prompt — same rich editor Advanced uses, so paste/drop images,
            mentions, and formatting all work. The dropZone wrapper enables
            drag-and-drop file uploads alongside paste. */}
        {/* `flex-1 min-h-0 overflow-y-auto` so the editor area absorbs the
            remaining vertical space inside the (now max-bounded) DialogContent
            and scrolls internally. Without it, pasting an image expanded the
            editor unbounded and pushed the modal past the viewport. */}
        <div
          {...dropZoneProps}
          className="relative px-5 pb-3 flex flex-1 min-h-[140px] overflow-y-auto"
        >
          <ContentEditor
            ref={editorRef}
            defaultValue={initialPrompt}
            placeholder={t(($) => $.create_task.agent.prompt_placeholder)}
            onUpdate={(md) => {
              setHasContent(md.trim().length > 0);
              setAgent({ prompt: md });
            }}
            onUploadFile={handleUploadFile}
            onUploadingChange={uploadGate.onUploadingChange}
            attachments={pendingAttachments}
            onSubmit={submit}
            debounceMs={150}
            enableResourceMentions
            className="resource-inline-content"
          />
          {isDragOver && <FileDropOverlay />}
        </div>


        {error && (
          <div className="px-5 pb-2 text-caption text-destructive">{error}</div>
        )}

        {/* Property toolbar — the project is visible by default; priority and
            due date live behind the overflow until exposed in settings or
            given a value. Unfinished picks remain workspace-persistent.
            When the modal was opened from "Add sub task" on an existing
            task, a read-only chip on the same row tells the user that the
            new task will be filed as a sub-task of that parent — the agent
            handles the wiring silently, but surfacing the relationship
            avoids "where did this end up?" surprise. We deliberately keep
            it non-editable: changing the parent is a `Set parent` action on
            the parent itself, not a knob in the quick-create flow. */}
        <div className="flex items-center gap-1.5 px-4 pb-2 shrink-0 flex-wrap">
          {(visibleFields.includes("project") ||
            projectId !== null ||
            fieldPickerOpen === "project") && (
            <ProjectPicker
              projectId={projectId}
              onUpdate={(u) => {
                const next = u.project_id ?? null;
                if (next !== projectId) {
                  setWorkflowInstanceId(undefined);
                  setWorkflowStageId(undefined);
                }
                setProjectId(next);
                setShared({ projectId: next ?? undefined });
              }}
              triggerRender={<PillButton />}
              align="start"
              open={fieldPickerOpen === "project" ? true : undefined}
              onOpenChange={(open) => setFieldPickerOpen(open ? "project" : null)}
            />
          )}
          <WorkflowAssignmentPicker
            projectId={projectId ?? undefined}
            workflowInstanceId={workflowInstanceId}
            workflowStageId={workflowStageId}
            onChange={(assignment) => {
              setWorkflowInstanceId(assignment.workflowInstanceId);
              setWorkflowStageId(assignment.workflowStageId);
            }}
          />
          {(visibleFields.includes("priority") ||
            priority !== "none" ||
            fieldPickerOpen === "priority") && (
            <PriorityPicker
              priority={priority}
              onUpdate={(updates) => {
                if (updates.priority) {
                  setPriority(updates.priority);
                  setShared({ priority: updates.priority });
                }
              }}
              triggerRender={<PillButton />}
              align="start"
              open={fieldPickerOpen === "priority" ? true : undefined}
              onOpenChange={(open) => setFieldPickerOpen(open ? "priority" : null)}
            />
          )}
          {(visibleFields.includes("due_date") ||
            dueDate !== null ||
            fieldPickerOpen === "due_date") && (
            <DueDatePicker
              dueDate={dueDate}
              onUpdate={(updates) => {
                const next = updates.due_date ?? null;
                setDueDate(next);
                setShared({ dueDate: next });
              }}
              triggerRender={<PillButton />}
              align="start"
              open={fieldPickerOpen === "due_date" ? true : undefined}
              onOpenChange={(open) => setFieldPickerOpen(open ? "due_date" : null)}
            />
          )}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <PillButton
                  aria-label={t(($) => $.create_task.agent.more_fields_aria)}
                  title={t(($) => $.create_task.agent.more_fields_aria)}
                >
                  <MoreHorizontal className="size-3.5" />
                </PillButton>
              }
            />
            <DropdownMenuContent align="start" className="w-52">
              {!visibleFields.includes("project") && projectId === null && (
                <DropdownMenuItem onClick={() => setFieldPickerOpen("project")}>
                  <FolderKanban className="size-3.5 text-muted-foreground" />
                  {t(($) => $.create_task.agent.set_project)}
                </DropdownMenuItem>
              )}
              {!visibleFields.includes("priority") && priority === "none" && (
                <DropdownMenuItem onClick={() => setFieldPickerOpen("priority")}>
                  <PriorityIcon priority="none" className="size-3.5" />
                  {t(($) => $.create_task.agent.set_priority)}
                </DropdownMenuItem>
              )}
              {!visibleFields.includes("due_date") && dueDate === null && (
                <DropdownMenuItem onClick={() => setFieldPickerOpen("due_date")}>
                  <CalendarDays className="size-3.5 text-muted-foreground" />
                  {t(($) => $.create_task.agent.set_due_date)}
                </DropdownMenuItem>
              )}
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={openFieldSettings}>
                <Settings2 className="size-3.5 text-muted-foreground" />
                {t(($) => $.create_task.agent.customize_fields)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          {parentTaskId && (
            <span
              data-testid="agent-sub-task-chip"
              className="inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-caption text-muted-foreground"
              title={t(($) => $.create_task.agent.sub_task_of, {
                identifier: parentTaskIdentifier ?? "",
              })}
            >
              {t(($) => $.create_task.agent.sub_task_of, {
                identifier: parentTaskIdentifier ?? "",
              })}
            </span>
          )}
        </div>

        {/* Footer */}
        <div className="flex flex-col gap-2 border-t px-4 py-3 shrink-0 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex min-h-7 items-center gap-2">
            {/* Deliberately NOT disabled while uploading: each file is its
                own queue entry, so queueing a second one is safe and waiting
                for the first to land just to attach the next is busywork. */}
            <FileUploadButton
              size="sm"
              multiple
              onSelect={(file) => editorRef.current?.uploadFile(file)}
            />
            {keepOpen && sentCount > 0 && (
              <span className="text-caption text-emerald-600 dark:text-emerald-400">
                {t(($) => $.create_task.agent.sent_count, { count: sentCount })}
              </span>
            )}
          </div>
          <div className="flex flex-wrap items-center justify-end gap-2">
            <button
              type="button"
              onClick={switchToManual}
              disabled={gate.uploading}
              aria-disabled={gate.uploading || undefined}
              aria-busy={gate.uploading || undefined}
              title={t(($) => $.create_task.switch_to_manual_tooltip)}
              className="flex shrink-0 items-center gap-1.5 text-caption px-2 py-1 rounded-sm text-muted-foreground hover:text-foreground hover:bg-accent/60 transition-colors cursor-pointer disabled:cursor-not-allowed disabled:opacity-50"
            >
              <ArrowLeftRight className="size-3.5" />
              {t(($) => $.create_task.switch_to_manual)}
            </button>
            <label className="flex shrink-0 items-center gap-1.5 text-caption text-muted-foreground cursor-pointer select-none">
              <Switch
                size="sm"
                checked={keepOpen}
                onCheckedChange={setKeepOpen}
              />
              {t(($) => $.create_task.create_another)}
            </label>
            <Button
              size="sm"
              onClick={submit}
              disabled={!hasContent || !actor || submitting || versionBlocked || gate.uploading}
              aria-disabled={gate.uploading || undefined}
              // Sending is a busy state too, not just uploading.
              aria-busy={gate.uploading || submitting || undefined}
              title={
                versionBlocked
                  ? t(($) => $.create_task.agent.version_blocked_tooltip, { min: versionCheck.min })
                  : undefined
              }
              className={justSent ? "min-w-28 !bg-emerald-600 !text-white" : "min-w-28"}
            >
              {submitting ? t(($) => $.create_task.agent.sending) : gate.uploading ? t(($) => $.create_task.agent.uploading) : justSent ? (
                <span className="flex items-center gap-1"><Check className="size-3.5" />{t(($) => $.create_task.agent.sent_label)}</span>
              ) : (
                <>
                  {t(($) => $.create_task.agent.submit)}
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
          </div>
        </div>
    </>
  );
}

// ActorPicker — the "Created by" trigger + searchable popover listing
// agents and squads. Lives in this file (not under tasks/components/pickers)
// because it composes the generic PropertyPicker with a quick-create-shaped
// trigger styled to match the modal header row — promoting it would invite
// reuse pressure on a UI that's deliberately tuned for this one surface.
function ActorPicker({
  actor,
  visibleAgents,
  visibleSquads,
  selectedAgent,
  selectedSquad,
  onPick,
  t,
}: {
  actor: ActorSelection | null;
  visibleAgents: Agent[];
  visibleSquads: Squad[];
  selectedAgent: Agent | undefined;
  selectedSquad: Squad | undefined;
  onPick: (next: ActorSelection) => void;
  t: ReturnType<typeof useT<"modals">>["t"];
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const query = filter.trim().toLowerCase();

  const filteredAgents = useMemo(
    () => visibleAgents.filter((a) => a.name.toLowerCase().includes(query) || matchesPinyin(a.name, query)),
    [visibleAgents, query],
  );
  const filteredSquads = useMemo(
    () => visibleSquads.filter((s) => s.name.toLowerCase().includes(query) || matchesPinyin(s.name, query)),
    [visibleSquads, query],
  );

  const displayLabel = selectedSquad?.name ?? selectedAgent?.name;
  const displayActor: ActorSelection | null = selectedSquad
    ? { type: "squad", id: selectedSquad.id }
    : selectedAgent
      ? { type: "agent", id: selectedAgent.id }
      : null;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(v: boolean) => {
        setOpen(v);
        if (!v) setFilter("");
      }}
      width="w-64"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.create_task.agent.search_placeholder)}
      onSearchChange={setFilter}
      trigger={
        <span className="flex items-center gap-2 text-caption text-muted-foreground hover:text-foreground transition-colors">
          <span>{t(($) => $.create_task.agent.created_by)}</span>
          {displayActor && displayLabel ? (
            <span className="flex items-center gap-1.5 text-foreground">
              <ActorAvatar
                actorType={displayActor.type}
                actorId={displayActor.id}
                size="sm"
              />
              {displayLabel}
            </span>
          ) : (
            <span>{t(($) => $.create_task.agent.pick_an_agent)}</span>
          )}
        </span>
      }
    >
      {filteredAgents.length === 0 && filteredSquads.length === 0 ? (
        query ? (
          <PickerEmpty />
        ) : (
          <div className="px-2 py-1.5 text-caption text-muted-foreground">
            {t(($) => $.create_task.agent.no_agents)}
          </div>
        )
      ) : (
        <>
          {filteredAgents.length > 0 && (
            <PickerSection label={t(($) => $.create_task.agent.agents_group)}>
              {filteredAgents.map((a) => (
                <PickerItem
                  key={a.id}
                  selected={actor?.type === "agent" && actor.id === a.id}
                  onClick={() => {
                    onPick({ type: "agent", id: a.id });
                    setOpen(false);
                  }}
                >
                  <ActorAvatar actorType="agent" actorId={a.id} size="sm" />
                  <span className="truncate">{a.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
          {filteredSquads.length > 0 && (
            <PickerSection label={t(($) => $.create_task.agent.squads_group)}>
              {filteredSquads.map((s) => (
                <PickerItem
                  key={s.id}
                  selected={actor?.type === "squad" && actor.id === s.id}
                  onClick={() => {
                    onPick({ type: "squad", id: s.id });
                    setOpen(false);
                  }}
                >
                  <ActorAvatar actorType="squad" actorId={s.id} size="sm" />
                  <span className="truncate">{s.name}</span>
                </PickerItem>
              ))}
            </PickerSection>
          )}
        </>
      )}
    </PropertyPicker>
  );
}
