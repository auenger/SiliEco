"use client";

import { useRef, useState } from "react";
import { useAuthStore } from "@silieco/core/auth";
import { useReworkWorkflowExecution } from "@silieco/core/workflows";
import type { WorkflowExecution, WorkflowInstance, WorkflowInstanceStage } from "@silieco/core/types";
import { Button } from "@silieco/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@silieco/ui/components/ui/dialog";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { workflowOperationDialogClassName } from "./workflow-dialog";

export function ExecutionReworkControl({instance, execution, disabled = false}: {instance: WorkflowInstance; execution: WorkflowExecution; disabled?: boolean}) {
  const {t} = useT("workflows");
  const userId = useAuthStore((state) => state.user?.id);
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const current = instance.stages?.find((stage) => stage.id === instance.current_stage_id);
  const canRework = Boolean(userId) && userId === instance.created_by && !instance.archived_at
    && ["active", "waiting", "completed"].includes(instance.status) && current?.execution_mode === "agent"
    && execution.stage_id === current.id && execution.plan_revision === instance.revision
    && execution.failure_code !== "rework_invalidated"
    && ["pending", "queued", "running", "awaiting_review", "succeeded", "blocked", "failed", "cancelled"].includes(execution.status);
  if (!canRework || !current) return null;
  const limit=current.rework_policy?.max_reworks;
  const used=(instance.reworks ?? []).filter(record=>record.source_stage_id===current.id).length;
  if (typeof limit === "number" && used >= limit) return <p className="text-caption text-muted-foreground">{t(($) => $.rework.limit_reached,{count:used,limit})}</p>;
  const targets = [current, ...(instance.stages ?? []).filter((stage) => stage.stable_key === current.rollback_stage_key && stage.position < current.position && (stage.execution_mode === "agent" || stage.execution_mode === "manual"))];
  return <>
    <Button size="sm" variant="outline" disabled={disabled || pending} onClick={() => setOpen(true)}>{t(($) => $.rework.open)}</Button>
    <Dialog open={open} onOpenChange={(value) => {if (!pending) setOpen(value);}}>
      <DialogContent className={workflowOperationDialogClassName}>
        <DialogHeader>
          <DialogTitle>{t(($) => $.rework.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.rework.description)}</DialogDescription>
        </DialogHeader>
        {open && disabled && <p role="alert" className="text-caption text-muted-foreground">{t(($) => $.rework.unavailable)}</p>}
        {open && <ReworkForm key={`${instance.id}:${instance.revision}:${execution.id}:${JSON.stringify(targets.map((stage) => [stage.id, stage.name, stage.position]))}`} instance={instance} execution={execution} targets={targets} disabled={disabled} onPending={setPending} onClose={() => setOpen(false)} />}

      </DialogContent>
    </Dialog>
  </>;
}

function ReworkForm({instance, execution, targets, disabled, onPending, onClose}: {instance: WorkflowInstance; execution: WorkflowExecution; targets: WorkflowInstanceStage[]; disabled: boolean; onPending: (value: boolean) => void; onClose: () => void}) {
  const {t} = useT("workflows");
  const mutation = useReworkWorkflowExecution(instance.workspace_id);
  const [targetId, setTargetId] = useState(execution.stage_id);
  const [reason, setReason] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [failed, setFailed] = useState(false);
  const [pending, setPending] = useState(false);
  const submitting = useRef(false);
  const target = targets.find((stage) => stage.id === targetId);
  const affected = (instance.stages ?? []).filter((stage) => target && stage.position >= target.position).sort((a, b) => a.position - b.position);
  const reasonBytes = new TextEncoder().encode(reason).length;
  const valid = Boolean(target) && reason.trim().length > 0 && reasonBytes <= 4000 && confirmed && !failed && !pending && !disabled;
  const submit = async () => {
    if (!valid || submitting.current) return;
    submitting.current = true;setPending(true);onPending(true);
    try {
      await mutation.mutateAsync({instanceId: instance.id, sourceExecutionId: execution.id, data: {expected_revision: instance.revision, target_stage_id: targetId, reason}});
      onClose();
    } catch {setFailed(true);setConfirmed(false);} finally {submitting.current = false;setPending(false);onPending(false);}
  };
  return <div className="space-y-5">
    <fieldset disabled={pending || failed || disabled} className="space-y-2">
      <legend className="mb-2 text-caption font-medium">{t(($) => $.rework.target)}</legend>
      {targets.map((stage) => <label key={stage.id} className={`flex cursor-pointer items-start gap-3 rounded-lg border p-3 ${stage.id === targetId ? "border-primary/40 bg-muted/40" : "border-border"}`}>
        <input className="mt-0.5 accent-primary" type="radio" name="rework-target" value={stage.id} checked={stage.id === targetId} onChange={() => {setTargetId(stage.id);setConfirmed(false);}} />
        <span className="min-w-0"><span className="block break-words text-caption font-medium">{stage.name}</span><span className="text-micro text-muted-foreground">{stage.id === execution.stage_id ? t(($) => $.rework.current) : t(($) => $.rework.rollback)}</span></span>
      </label>)}
    </fieldset>
    <div className="space-y-2 rounded-lg border border-border bg-muted/25 p-3">
      <p className="text-caption font-medium">{t(($) => $.rework.affected)}</p>
      <ol className="list-inside list-decimal space-y-1 text-caption text-muted-foreground">{affected.map((stage) => <li key={stage.id} className="break-words">{stage.name}</li>)}</ol>
      <p className="text-micro text-muted-foreground">{t(($) => $.rework.history_effect)}</p>
    </div>
    <label className="block space-y-2 text-caption font-medium"><span>{t(($) => $.rework.reason)}</span><Textarea className="min-h-28" value={reason} onChange={(event) => setReason(event.target.value)} disabled={pending || failed || disabled} /></label>
    {reasonBytes > 4000 && <p role="alert" className="text-caption text-destructive">{t(($) => $.rework.reason_too_long)}</p>}
    <label className="flex items-start gap-2 text-caption"><input className="mt-0.5 accent-primary" type="checkbox" checked={confirmed} disabled={pending || failed || disabled} onChange={(event) => setConfirmed(event.target.checked)} />{t(($) => $.rework.confirm)}</label>
    {failed && <p role="alert" className="text-caption text-destructive">{t(($) => $.rework.failed)}</p>}
    <DialogFooter className="border-t pt-4">
      <Button variant="outline" disabled={pending} onClick={onClose}>{t(($) => $.rework.cancel)}</Button>
      <Button disabled={!valid} onClick={() => void submit()}>{pending ? t(($) => $.rework.submitting) : t(($) => $.rework.submit)}</Button>
    </DialogFooter>
  </div>;
}
