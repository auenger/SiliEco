"use client";

import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@silieco/core/auth";
import type { WorkflowAgentReviewStatus, WorkflowExecution, WorkflowInstance } from "@silieco/core/types";
import { workflowAgentReviewStatusOptions, useRecoverWorkflowAgentReview } from "@silieco/core/workflows";
import { Button } from "@silieco/ui/components/ui/button";
import { Checkbox } from "@silieco/ui/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@silieco/ui/components/ui/dialog";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { workflowOperationDialogClassName } from "./workflow-dialog";

export function AgentReviewRecoveryControl({instance, execution}: {instance: WorkflowInstance; execution: WorkflowExecution}) {
  const {t} = useT("workflows");
  const userId = useAuthStore((state) => state.user?.id);
  const query = useQuery(workflowAgentReviewStatusOptions(instance.workspace_id, instance.id, execution.id));
  const mutation = useRecoverWorkflowAgentReview(instance.workspace_id);
  const [snapshot, setSnapshot] = useState<WorkflowAgentReviewStatus | null>(null);
  const [reason, setReason] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [failed, setFailed] = useState(false);
  const submitted = useRef(false);
  const current = query.data;
  const active = instance.created_by === userId && instance.status === "active" && !instance.archived_at
    && instance.current_stage_id === execution.stage_id && execution.status === "awaiting_review";
  const recoverable = active && !query.isError && current?.execution_id === execution.id && (current.phase === "failed" || current.phase === "missing_decision")
    && current.can_recover === true && ((current.phase === "failed" && current.call?.status === "failed") || (current.phase === "missing_decision" && current.call?.status === "completed")) && current.recovery_attempts > 0
    && current.recovery_attempts <= 3 && Boolean(current.request_id && current.basis_hash)
    && current.result_revision === execution.result_revision;
  const stale = !recoverable || !snapshot || snapshot.request_id !== current?.request_id || snapshot.call?.id !== current?.call?.id
    || snapshot.basis_hash !== current?.basis_hash || snapshot.result_revision !== current?.result_revision
    || snapshot.recovery_attempts !== current?.recovery_attempts;
  const tooLong = new TextEncoder().encode(reason.trim()).length > 4000;
  const locked = stale || failed || mutation.isPending || query.isFetching;
  const phaseLabel = () => {
    if (query.isError || !current) return t(($) => $.agent_review_recovery.unavailable);
    switch (current.phase) {
      case "failed": return t(($) => $.agent_review_recovery.failed);
      case "retry_scheduled": return t(($) => $.agent_review_recovery.retry_scheduled);
      case "queued": return t(($) => $.agent_review_recovery.queued);
      case "running": return t(($) => $.agent_review_recovery.running);
      case "approved": return t(($) => $.agent_review_recovery.approved);
      case "rejected": return t(($) => $.agent_review_recovery.rejected);
      case "missing_decision": return t(($) => $.agent_review_recovery.missing_decision);
      case "preparing": case "waiting_delivery": return t(($) => $.agent_review_recovery.preparing);
      default: return t(($) => $.agent_review_recovery.unavailable);
    }
  };
  const submit = async () => {
    if (locked || tooLong || !confirmed || !reason.trim() || submitted.current || !snapshot?.call || !snapshot.request_id || !snapshot.basis_hash) return;
    submitted.current = true;
    try {
      await mutation.mutateAsync({instanceId: instance.id, executionId: execution.id, requestId: snapshot.request_id,
        data: {failed_task_id: snapshot.call.id, expected_result_revision: snapshot.result_revision, expected_basis_hash: snapshot.basis_hash, reason: reason.trim()}});
      toast.success(t(($) => $.agent_review_recovery.recovered));
      setSnapshot(null);
    } catch {setFailed(true);setConfirmed(false);} finally {submitted.current = false;}
  };
  return <>
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-muted/25 p-3">
      <div className="min-w-0 flex-1"><p className="text-caption font-semibold">{t(($) => $.agent_review_recovery.title)}</p><p role="status" className="mt-1 break-words text-micro text-muted-foreground">{phaseLabel()}</p></div>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" variant="outline" disabled={query.isFetching || mutation.isPending} onClick={() => void query.refetch()}>{t(($) => $.agent_review_recovery.refresh)}</Button>
        {recoverable && <Button size="sm" variant="outline" disabled={query.isFetching || mutation.isPending} onClick={() => {setSnapshot(current);setReason("");setConfirmed(false);setFailed(false);}}>{t(($) => $.agent_review_recovery.open)}</Button>}
      </div>
    </div>
    <Dialog open={snapshot !== null} onOpenChange={(open) => {if (!open && !mutation.isPending && !submitted.current) setSnapshot(null);}}>
      <DialogContent className={workflowOperationDialogClassName}>
        <DialogHeader><DialogTitle>{t(($) => $.agent_review_recovery.open)}</DialogTitle><DialogDescription>{t(($) => $.agent_review_recovery.description, {revision: snapshot?.result_revision ?? 0})}</DialogDescription></DialogHeader>
        <div className="rounded-lg border bg-muted/25 p-3 text-caption">{t(($) => $.agent_review_recovery.budget, {count: snapshot?.recovery_attempts ?? 0})}</div>
        <label className="space-y-2 text-caption"><span>{t(($) => $.agent_review_recovery.reason)}</span><Textarea value={reason} disabled={mutation.isPending} onChange={(event) => setReason(event.target.value)} /></label>
        {tooLong && <p role="alert" className="text-caption text-destructive">{t(($) => $.stage_review.note_limit)}</p>}
        <label className="flex items-start gap-2 text-caption"><Checkbox checked={confirmed} disabled={locked} onCheckedChange={(value) => setConfirmed(value === true)} /><span>{t(($) => $.agent_review_recovery.confirm)}</span></label>
        {(stale || failed) && <p role="alert" className="text-caption text-destructive">{t(($) => $.agent_review_recovery.changed)}</p>}
        <DialogFooter><Button variant="outline" disabled={mutation.isPending} onClick={() => setSnapshot(null)}>{t(($) => $.agent_review_recovery.close)}</Button><Button disabled={locked || tooLong || !confirmed || !reason.trim()} onClick={() => void submit()}>{t(($) => $.agent_review_recovery.submit)}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  </>;
}
