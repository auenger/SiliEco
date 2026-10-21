"use client";

import { workflowConfigurationDialogWidth } from "./workflow-dialog";
import { StageExecutionFields, validReworkPolicyDraft, validRetryPolicyDraft } from "./stage-execution-fields";
import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  GitBranch,
  History,
  LoaderCircle,
  LockKeyhole,
  Plus,
  ShieldCheck,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@silieco/core/hooks";
import { workspaceMCPServersOptions } from "@silieco/core/mcp";
import type {
  Agent,
  MemberWithUser,
  WorkflowGateType,
  WorkflowInstanceStage,
  WorkflowObject,
  SkillSummary,
} from "@silieco/core/types";
import {
  useUpdateWorkflowInstancePlan,
  workflowInstanceOptions,
} from "@silieco/core/workflows";
import {
  agentListOptions,
  memberListOptions,
  skillListOptions,
} from "@silieco/core/workspace/queries";
import { Badge } from "@silieco/ui/components/ui/badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@silieco/ui/components/ui/alert-dialog";
import { Button } from "@silieco/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@silieco/ui/components/ui/dialog";
import { Input } from "@silieco/ui/components/ui/input";
import { Label } from "@silieco/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@silieco/ui/components/ui/select";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { cn } from "@silieco/ui/lib/utils";
import { useT } from "../../i18n";
import { StageMCPSelector } from "./stage-mcp-selector";

interface RunStageDraft {
  id?: string;
  stableKey: string;
  name: string;
  description: string;
  input: string;
  output: string;
  skills: string;
  allowedMcpServerIds: string[] | null;
  gate: WorkflowObject & { type?: WorkflowGateType };
  executionMode: string;
  executorId: string | null;
  reworkPolicy?: WorkflowObject;
  retryPolicy?: WorkflowObject;
  completionRule: WorkflowObject;
  inputSpec: WorkflowObject;
  inputBindings?: WorkflowObject;
  outputSpec: WorkflowObject;
  rollbackStageKey: string | null;
}

function objectString(value: WorkflowObject, key: string): string {
  const item = value[key];
  return typeof item === "string" ? item : "";
}

function stageDraft(stage: WorkflowInstanceStage, skills: readonly SkillSummary[]): RunStageDraft {
  return {
    id: stage.id,
    stableKey: stage.stable_key,
    name: stage.name,
    description: stage.description ?? "",
    input: objectString(stage.input_spec, "default_artifact"),
    output: objectString(stage.output_spec, "default_artifact"),
    skills: stage.required_skills
      .map((ref) => skills.find((skill) => skill.id === ref)?.name ?? ref)
      .join(", "),
    allowedMcpServerIds: stage.allowed_mcp_server_ids ?? null,
    gate: { ...stage.gate },
    executionMode: stage.execution_mode ?? "manual",
    executorId: stage.executor_id ?? null,
    completionRule: { ...stage.completion_rule }, reworkPolicy:{...stage.rework_policy}, retryPolicy:{...stage.retry_policy},
    inputSpec: { ...stage.input_spec },
    inputBindings: { ...stage.input_bindings },
    outputSpec: { ...stage.output_spec },
    rollbackStageKey: stage.rollback_stage_key,
  };
}

function skillRefs(value: string, skills: readonly SkillSummary[]): string[] {
  return [
    ...new Set(
      value
        .split(/[,，]/)
        .map((item) => item.trim())
        .filter(Boolean)
        .map((ref) => skills.find((skill) => skill.id === ref || skill.name === ref)?.id ?? ref),
    ),
  ];
}

function withoutKey(value: WorkflowObject, key: string): WorkflowObject {
  const next = { ...value };
  delete next[key];
  return next;
}

export function WorkflowRunPlanDialog({
  instanceId,
  sourceName,
  open,
  onOpenChange,
}: {
  instanceId: string;
  sourceName: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("workflows");
  const workspaceId = useWorkspaceId();
  const { data: run, isLoading } = useQuery(
    workflowInstanceOptions(workspaceId, open ? instanceId : ""),
  );
  const { data: workspaceSkills = [] } = useQuery(skillListOptions(workspaceId));
  const { data: workspaceMCP } = useQuery(workspaceMCPServersOptions(workspaceId));
  const { data: members = [] } = useQuery(memberListOptions(workspaceId));
  const { data: agents = [] } = useQuery(agentListOptions(workspaceId));
  const updatePlan = useUpdateWorkflowInstancePlan();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [changeNote, setChangeNote] = useState("");
  const [stages, setStages] = useState<RunStageDraft[]>([]);
  const [confirmSave, setConfirmSave] = useState(false);

  useEffect(() => {
    if (!run || !open) return;
    setTitle(run.title);
    setDescription(run.description ?? "");
    setChangeNote("");
    setStages((run.stages ?? []).map((stage) => stageDraft(stage, workspaceSkills)));
  }, [open, run, workspaceSkills]);

  const currentPosition = useMemo(
    () =>
      run?.stages?.find((stage) => stage.id === run.current_stage_id)?.position ??
      run?.current_stage_index ??
      0,
    [run],
  );
  const taskCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const task of run?.tasks ?? []) {
      if (!task.workflow_stage_id) continue;
      counts.set(task.workflow_stage_id, (counts.get(task.workflow_stage_id) ?? 0) + 1);
    }
    return counts;
  }, [run?.tasks]);
  const decisionCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const decision of run?.decisions ?? []) {
      counts.set(decision.from_stage_id, (counts.get(decision.from_stage_id) ?? 0) + 1);
      if (decision.to_stage_id) {
        counts.set(decision.to_stage_id, (counts.get(decision.to_stage_id) ?? 0) + 1);
      }
    }
    return counts;
  }, [run?.decisions]);
  const changeCount = useMemo(() => {
    if (!run) return 0;
    let count = title.trim() === run.title ? 0 : 1;
    count += (description.trim() || "") === (run.description ?? "") ? 0 : 1;
    const originalStages = new Map(
      (run.stages ?? []).map((stage) => {
        const draft = stageDraft(stage, workspaceSkills);
        return [stage.id, draft] as const;
      }),
    );
    for (const stage of stages) {
      if (!stage.id) {
        count += 1;
        continue;
      }
      const original = originalStages.get(stage.id);
      if (!original || JSON.stringify(original) !== JSON.stringify(stage)) count += 1;
      originalStages.delete(stage.id);
    }
    return count + originalStages.size;
  }, [description, run, stages, title, workspaceSkills]);

  const updateStage = (index: number, patch: Partial<RunStageDraft>) =>
    setStages((items) =>
      items.map((item, itemIndex) => (itemIndex === index ? { ...item, ...patch } : item)),
    );

  const updateGateType = (index: number, type: WorkflowGateType) => {
    const stage = stages[index];
    if (!stage) return;
    const activeAgents = (agents as Agent[]).filter((agent) => !agent.archived_at);
    const workspaceMembers = members as MemberWithUser[];
    const currentDecider = typeof stage.gate.decider === "string" ? stage.gate.decider : "";
    const currentHuman =
      typeof stage.gate.human_decider === "string" ? stage.gate.human_decider : "";
    const agentID = activeAgents.some((agent) => agent.id === currentDecider)
      ? currentDecider
      : (activeAgents[0]?.id ?? "");
    const memberID = workspaceMembers.some(
      (member) => member.user_id === (currentHuman || currentDecider),
    )
      ? currentHuman || currentDecider
      : (workspaceMembers.find((member) => member.role === "owner")?.user_id ??
        workspaceMembers[0]?.user_id ??
        "");
    if (type === "none") {
      updateStage(index, { gate: { type: "none" } });
    } else if (type === "human") {
      updateStage(index, {
        gate: {
          type,
          decider_type: "human",
          decider: memberID,
          require_human: true,
          human_decider: memberID,
        },
      });
    } else {
      updateStage(index, {
        gate: {
          type,
          decider_type: "agent",
          decider: agentID,
          require_human: type === "hybrid",
          human_decider: type === "hybrid" ? memberID : null,
        },
      });
    }
  };

  const canMove = (index: number, target: number) => {
    const item = stages[index];
    const targetItem = stages[target];
    if (!item || !targetItem || index <= currentPosition || target <= currentPosition) return false;
    return (
      (item.id ? (taskCounts.get(item.id) ?? 0) + (decisionCounts.get(item.id) ?? 0) === 0 : true) &&
      (targetItem.id
        ? (taskCounts.get(targetItem.id) ?? 0) + (decisionCounts.get(targetItem.id) ?? 0) === 0
        : true)
    );
  };

  const moveStage = (index: number, target: number) => {
    if (!canMove(index, target)) return;
    setStages((items) => {
      const next = [...items];
      [next[index], next[target]] = [next[target]!, next[index]!];
      return next;
    });
  };

  const addStage = () => {
    const unique = `run_stage_${Date.now()}`;
    setStages((items) => [
      ...items,
      {
        stableKey: unique,
        name: t(($) => $.designer.run_plan_new_stage),
        description: "",
        input: "",
        output: "",
        skills: "",
        allowedMcpServerIds: null,
        gate: { type: "none" },
        executionMode: "manual",
        executorId: null,
        completionRule: { type: "all_tasks_terminal" },
        inputSpec: {},
        outputSpec: {},
        rollbackStageKey: null,
      },
    ]);
  };

  const removeStage = (index: number) => {
    const item = stages[index];
    if (!item || index <= currentPosition) return;
    if (item.id && ((taskCounts.get(item.id) ?? 0) > 0 || (decisionCounts.get(item.id) ?? 0) > 0)) {
      return;
    }
    setStages((items) => items.filter((_, itemIndex) => itemIndex !== index));
  };

  const invalidRollback = stages.some((stage) => !validReworkPolicyDraft(stage.reworkPolicy) || !validRetryPolicyDraft(stage.retryPolicy)) || stages.some((stage, index) => stage.rollbackStageKey && !stages.slice(0, index).some((previous) => previous.stableKey === stage.rollbackStageKey));

  const save = () => {
    if (invalidRollback || !run || !title.trim() || stages.length === 0 || stages.some((stage) => !stage.name.trim() || (stage.executionMode === "agent" && !(agents as Agent[]).some((agent) => agent.id === stage.executorId && !agent.archived_at)))) {
      return;
    }
    updatePlan.mutate(
      {
        instanceId: run.id,
        data: {
          expected_revision: run.revision,
          title: title.trim(),
          description: description.trim() || null,
          change_note: changeNote.trim() || null,
          stages: stages.map((stage) => ({
            ...(stage.id ? { id: stage.id } : {}),
            stable_key: stage.stableKey,
            name: stage.name.trim(),
            description: stage.description.trim() || null,
            execution_mode: stage.executionMode,
            executor_id: stage.executorId,
            completion_rule: stage.completionRule, rework_policy:stage.reworkPolicy ?? {}, retry_policy: stage.retryPolicy ?? {},
            input_bindings: stage.inputBindings ?? {},
            input_spec: stage.input.trim()
              ? { ...stage.inputSpec, default_artifact: stage.input.trim() }
              : withoutKey(stage.inputSpec, "default_artifact"),
            output_spec: stage.output.trim()
              ? { ...stage.outputSpec, default_artifact: stage.output.trim() }
              : withoutKey(stage.outputSpec, "default_artifact"),
            required_skills: skillRefs(stage.skills, workspaceSkills),
            allowed_mcp_server_ids: stage.allowedMcpServerIds,
            gate: stage.gate,
            rollback_stage_key: stage.rollbackStageKey,
          })),
        },
      },
      {
        onSuccess: () => {
          toast.success(t(($) => $.designer.run_plan_saved));
          onOpenChange(false);
        },
        onError: (error) =>
          toast.error(
            error instanceof Error ? error.message : t(($) => $.designer.run_plan_save_failed),
          ),
      },
    );
  };

  return (
    <>
    <Dialog open={open} onOpenChange={(next) => !updatePlan.isPending && onOpenChange(next)}>
      <DialogContent className={`flex max-h-[90vh] flex-col gap-0 overflow-hidden p-0 ${workflowConfigurationDialogWidth}`}>
        <DialogHeader className="border-b border-border/70 px-5 py-4">
          <div className="flex flex-wrap items-center gap-2">
            <DialogTitle>{t(($) => $.designer.run_plan_title)}</DialogTitle>
            {run && run.source_version > 0 && (
              <Badge variant="outline">
                {t(($) => $.designer.run_plan_source_version, {
                  version: run.source_version,
                })}
              </Badge>
            )}
            {run && run.revision > 1 && (
              <Badge variant="secondary">
                {t(($) => $.designer.run_plan_revision, {
                  revision: run.revision,
                })}
              </Badge>
            )}
          </div>
          <DialogDescription>
            {t(($) => $.designer.run_plan_description, { name: sourceName })}
          </DialogDescription>
        </DialogHeader>

        {isLoading || !run ? (
          <div className="flex min-h-64 items-center justify-center text-muted-foreground">
            <LoaderCircle className="mr-2 size-4 animate-spin" />
            {t(($) => $.designer.run_plan_loading)}
          </div>
        ) : (
          <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
            <div className="rounded-lg border border-brand/20 bg-brand/5 px-3 py-2.5 text-caption text-muted-foreground">
              <p className="flex items-center gap-2 font-medium text-foreground">
                <GitBranch className="size-3.5 text-brand" />
                {t(($) => $.designer.run_plan_scope_title)}
              </p>
              <p className="mt-1">{t(($) => $.designer.run_plan_scope_hint)}</p>
            </div>

            <div className="mt-4 grid gap-3 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="run-plan-title">{t(($) => $.designer.run_name)}</Label>
                <Input id="run-plan-title" value={title} onChange={(event) => setTitle(event.target.value)} />
              </div>
              <div className="space-y-1.5 sm:col-span-2">
                <Label htmlFor="run-plan-description">{t(($) => $.create.description)}</Label>
                <Textarea
                  id="run-plan-description"
                  rows={2}
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                />
              </div>
            </div>

            <div className="mb-2 mt-6 flex items-center justify-between gap-3">
              <div>
                <h3 className="text-body font-semibold">
                  {t(($) => $.designer.run_plan_stages, { count: stages.length })}
                </h3>
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.designer.run_plan_stages_hint)}
                </p>
              </div>
              <Button type="button" size="sm" variant="outline" onClick={addStage}>
                <Plus className="size-3.5" />
                {t(($) => $.designer.run_plan_add_stage)}
              </Button>
            </div>

            <div className="space-y-2.5">
              {stages.map((stage, index) => {
                const isPast = index < currentPosition;
                const isCurrent = index === currentPosition;
                const tasks = stage.id ? taskCounts.get(stage.id) ?? 0 : 0;
                const decisions = stage.id ? decisionCounts.get(stage.id) ?? 0 : 0;
                const structureLocked = isPast || isCurrent || tasks > 0 || decisions > 0;
                const gateLocked = isPast || (isCurrent && decisions > 0);
                return (
                  <section
                    key={stage.id ?? stage.stableKey}
                    className={cn(
                      "rounded-xl border p-3",
                      isCurrent ? "border-brand/35 bg-brand/5" : "border-border/80 bg-muted/15",
                    )}
                  >
                    <div className="flex items-start gap-3">
                      <span className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full border border-border bg-background text-caption font-semibold">
                        {index + 1}
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-2">
                          {isPast && (
                            <Badge variant="secondary">
                              <LockKeyhole className="mr-1 size-3" />
                              {t(($) => $.designer.run_plan_past)}
                            </Badge>
                          )}
                          {isCurrent && (
                            <Badge variant="outline">
                              <CheckCircle2 className="mr-1 size-3 text-brand" />
                              {t(($) => $.designer.run_plan_current)}
                            </Badge>
                          )}
                          {!isPast && !isCurrent && (
                            <Badge variant="outline">{t(($) => $.designer.run_plan_future)}</Badge>
                          )}
                          {tasks > 0 && (
                            <span className="text-micro text-muted-foreground">
                              {t(($) => $.designer.run_task_count, { count: tasks })}
                            </span>
                          )}
                          <div className="ml-auto flex items-center gap-1">
                            <Button
                              type="button"
                              size="icon-sm"
                              variant="ghost"
                              disabled={!canMove(index, index - 1)}
                              aria-label={t(($) => $.designer.move_up)}
                              onClick={() => moveStage(index, index - 1)}
                            >
                              <ArrowUp className="size-3.5" />
                            </Button>
                            <Button
                              type="button"
                              size="icon-sm"
                              variant="ghost"
                              disabled={!canMove(index, index + 1)}
                              aria-label={t(($) => $.designer.move_down)}
                              onClick={() => moveStage(index, index + 1)}
                            >
                              <ArrowDown className="size-3.5" />
                            </Button>
                            <Button
                              type="button"
                              size="icon-sm"
                              variant="ghost"
                              disabled={structureLocked}
                              aria-label={t(($) => $.designer.delete_stage)}
                              onClick={() => removeStage(index)}
                            >
                              <Trash2 className="size-3.5" />
                            </Button>
                          </div>
                        </div>

                        <div className="mt-3"><StageExecutionFields value={stage} rollbackStages={stages.slice(0, index).map((item) => ({stableKey: item.stableKey, name: item.name}))} precedingStages={stages.slice(0, index).filter((item) => item.executionMode === "agent").map((item) => ({stableKey: item.stableKey, name: item.name}))} agents={agents as Agent[]} disabled={isPast || isCurrent || updatePlan.isPending} onChange={(patch) => updateStage(index, patch)} /></div>
                        <div className="mt-3 grid gap-3 sm:grid-cols-2">
                          <div className="space-y-1.5 sm:col-span-2">
                            <Label htmlFor={`run-stage-name-${index}`}>
                              {t(($) => $.designer.run_plan_stage_name)}
                            </Label>
                            <Input
                              id={`run-stage-name-${index}`}
                              disabled={isPast}
                              value={stage.name}
                              onChange={(event) => updateStage(index, { name: event.target.value })}
                            />
                          </div>
                          <div className="space-y-1.5">
                            <Label htmlFor={`run-stage-output-${index}`}>
                              {t(($) => $.designer.run_plan_output)}
                            </Label>
                            <Input
                              id={`run-stage-output-${index}`}
                              disabled={isPast}
                              value={stage.output}
                              onChange={(event) => updateStage(index, { output: event.target.value })}
                            />
                          </div>
                          <div className="space-y-1.5">
                            <Label htmlFor={`run-stage-skills-${index}`}>
                              {t(($) => $.designer.run_plan_skills)}
                            </Label>
                            <Input
                              id={`run-stage-skills-${index}`}
                              disabled={isPast}
                              value={stage.skills}
                              placeholder={t(($) => $.designer.run_plan_skills_hint)}
                              onChange={(event) => updateStage(index, { skills: event.target.value })}
                            />
                          </div>
                          <div className="space-y-1.5">
                            <Label>{t(($) => $.designer.run_plan_gate)}</Label>
                            <Select
                              items={[
                                { value: "none", label: t(($) => $.stage.gate_none) },
                                { value: "human", label: t(($) => $.stage.gate_human) },
                                { value: "agent", label: t(($) => $.stage.gate_agent) },
                                { value: "hybrid", label: t(($) => $.stage.gate_hybrid) },
                              ]}
                              disabled={gateLocked}
                              value={stage.gate.type ?? "none"}
                              onValueChange={(value) =>
                                updateGateType(index, value as WorkflowGateType)
                              }
                            >
                              <SelectTrigger>
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                <SelectItem value="none">{t(($) => $.stage.gate_none)}</SelectItem>
                                <SelectItem value="human">{t(($) => $.stage.gate_human)}</SelectItem>
                                <SelectItem value="agent">{t(($) => $.stage.gate_agent)}</SelectItem>
                                <SelectItem value="hybrid">{t(($) => $.stage.gate_hybrid)}</SelectItem>
                              </SelectContent>
                            </Select>
                          </div>
                          {(stage.gate.type === "agent" || stage.gate.type === "hybrid") && (
                            <div className="space-y-1.5">
                              <Label>{t(($) => $.designer.agent_decider)}</Label>
                              <Select
                                items={(agents as Agent[])
                                  .filter((agent) => !agent.archived_at)
                                  .map((agent) => ({ value: agent.id, label: agent.name }))}
                                disabled={gateLocked}
                                value={
                                  typeof stage.gate.decider === "string"
                                    ? stage.gate.decider
                                    : ""
                                }
                                onValueChange={(value) =>
                                  updateStage(index, {
                                    gate: { ...stage.gate, decider: value },
                                  })
                                }
                              >
                                <SelectTrigger>
                                  <SelectValue placeholder={t(($) => $.designer.select_agent)} />
                                </SelectTrigger>
                                <SelectContent>
                                  {(agents as Agent[])
                                    .filter((agent) => !agent.archived_at)
                                    .map((agent) => (
                                      <SelectItem key={agent.id} value={agent.id}>
                                        {agent.name}
                                      </SelectItem>
                                    ))}
                                </SelectContent>
                              </Select>
                            </div>
                          )}
                          {(stage.gate.type === "human" || stage.gate.type === "hybrid") && (
                            <div className="space-y-1.5">
                              <Label>
                                {stage.gate.type === "hybrid"
                                  ? t(($) => $.designer.final_human_decider)
                                  : t(($) => $.designer.human_decider)}
                              </Label>
                              <Select
                                items={(members as MemberWithUser[]).map((member) => ({
                                  value: member.user_id,
                                  label: member.name || member.email,
                                }))}
                                disabled={gateLocked}
                                value={
                                  typeof stage.gate.human_decider === "string"
                                    ? stage.gate.human_decider
                                    : typeof stage.gate.decider === "string"
                                      ? stage.gate.decider
                                      : ""
                                }
                                onValueChange={(value) =>
                                  updateStage(index, {
                                    gate: {
                                      ...stage.gate,
                                      ...(stage.gate.type === "human" ? { decider: value } : {}),
                                      human_decider: value,
                                    },
                                  })
                                }
                              >
                                <SelectTrigger>
                                  <SelectValue placeholder={t(($) => $.designer.select_human)} />
                                </SelectTrigger>
                                <SelectContent>
                                  {(members as MemberWithUser[]).map((member) => (
                                    <SelectItem key={member.user_id} value={member.user_id}>
                                      {member.name || member.email}
                                    </SelectItem>
                                  ))}
                                </SelectContent>
                              </Select>
                            </div>
                          )}
                          <div className="space-y-1.5 sm:col-span-2">
                            <Label htmlFor={`run-stage-description-${index}`}>
                              {t(($) => $.create.description)}
                            </Label>
                            <Textarea
                              id={`run-stage-description-${index}`}
                              rows={2}
                              disabled={isPast}
                              value={stage.description}
                              onChange={(event) => updateStage(index, { description: event.target.value })}
                            />
                          </div>
                          <div className="sm:col-span-2">
                            <StageMCPSelector
                              servers={workspaceMCP?.servers ?? []}
                              selectedIds={stage.allowedMcpServerIds}
                              disabled={isPast}
                              onChange={(allowedMcpServerIds) => updateStage(index, { allowedMcpServerIds })}
                            />
                          </div>
                        </div>
                        {isCurrent && (
                          <p className="mt-2 flex items-center gap-1.5 text-caption text-muted-foreground">
                            <ShieldCheck className="size-3.5 text-brand" />
                            {t(($) => $.designer.run_plan_current_hint)}
                          </p>
                        )}
                      </div>
                    </div>
                  </section>
                );
              })}
            </div>

            <div className="mt-4 space-y-1.5">
              <Label htmlFor="run-plan-note">
                <History className="mr-1 inline size-3.5" />
                {t(($) => $.designer.run_plan_note)}
              </Label>
              <Textarea
                id="run-plan-note"
                rows={2}
                value={changeNote}
                placeholder={t(($) => $.designer.run_plan_note_hint)}
                onChange={(event) => setChangeNote(event.target.value)}
              />
            </div>
          </div>
        )}

        <DialogFooter className="border-t border-border/70 bg-muted/15 px-5 py-3">
          <Button variant="ghost" disabled={updatePlan.isPending} onClick={() => onOpenChange(false)}>
            {t(($) => $.designer.archive_cancel)}
          </Button>
          <Button
            disabled={
              !run ||
              invalidRollback ||
              updatePlan.isPending ||
              !title.trim() ||
              stages.length === 0 ||
              stages.some((stage) => !stage.name.trim() || (stage.executionMode === "agent" && !(agents as Agent[]).some((agent) => agent.id === stage.executorId && !agent.archived_at))) ||
              changeCount === 0
            }
            onClick={() => setConfirmSave(true)}
          >
            {updatePlan.isPending && <LoaderCircle className="size-3.5 animate-spin" />}
            {updatePlan.isPending
              ? t(($) => $.designer.run_plan_saving)
              : t(($) => $.designer.run_plan_save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
      <AlertDialog open={confirmSave} onOpenChange={setConfirmSave}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.designer.run_plan_confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.designer.run_plan_confirm_description, {
                count: changeCount,
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.designer.archive_cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={save}>
              {t(($) => $.designer.run_plan_save)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
