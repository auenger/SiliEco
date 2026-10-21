"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@silieco/core/auth";
import { api } from "@silieco/core/api";
import type { WorkflowHumanReview, WorkflowInstance, WorkflowResult } from "@silieco/core/types";
import { workflowExecutionsOptions, workflowHumanReviewOptions, workflowResultOptions, useRecordWorkflowHumanDecision } from "@silieco/core/workflows";
import { Button } from "@silieco/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@silieco/ui/components/ui/dialog";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { useT } from "../../i18n";
import { ExecutionReworkControl } from "./execution-rework-control";
import { workflowOperationDialogClassName } from "./workflow-dialog";
import { AgentReviewRecoveryControl } from "./agent-review-recovery-control";
import { InputRecoveryControl } from "./input-recovery-control";

export function ExecutionReviewControl({instance}: {instance: WorkflowInstance}) {
  const {t} = useT("workflows");
  const userId = useAuthStore((state) => state.user?.id);
  const [open, setOpen] = useState(false);
  const executions = useQuery(workflowExecutionsOptions(instance.workspace_id, instance.id));
  const stage = instance.stages?.find((item) => item.id === instance.current_stage_id);
  const execution = executions.data?.filter((item) => item.stage_id === stage?.id && item.plan_revision === instance.revision)
    .sort((a,b) => b.attempt - a.attempt)[0];
  const gate = stage?.gate ?? {};
  const reviewer = gate.type === "hybrid" ? gate.human_decider : gate.human_decider || gate.decider;
  const canReview = stage?.execution_mode === "agent" && (gate.type === "human" || gate.type === "hybrid") && typeof reviewer === "string" && reviewer === userId
    && instance.status === "active" && !instance.archived_at && execution?.status === "awaiting_review";
  const review = useQuery({...workflowHumanReviewOptions(instance.workspace_id, instance.id, execution?.id ?? ""),
    enabled: open && canReview === true, refetchOnWindowFocus: false, refetchOnReconnect: false});
  const revision = canReview && review.data ? review.data.result_revision : execution?.result_revision ?? 0;
  const result = useQuery({...workflowResultOptions(instance.workspace_id, instance.id, execution?.id ?? "", revision), enabled: open && Boolean(execution?.id) && revision > 0});
  const [pending, setPending] = useState(false);
  const evidence = review.data;
  const delivery = result.data;
  const validDelivery = delivery && delivery.execution_id === execution?.id && delivery.revision === revision;
  const validReview = canReview && !review.isFetching && !review.isError && evidence?.execution_id === execution?.id
    && /^[0-9a-f]{64}$/.test(evidence.basis_hash) && evidence.result_revision > 0;

  return <>
    {execution && (gate.type === "agent" || gate.type === "hybrid") && <AgentReviewRecoveryControl key={execution.id} instance={instance} execution={execution} />}
    {execution?.status === "blocked" && execution.failure_code === "input_unavailable" && <InputRecoveryControl instance={instance} execution={execution} />}
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border bg-muted/25 p-3">
      <div className="min-w-0">
        <p className="text-caption font-semibold">{t(($) => $.execution_review.title)}</p>
        <p className="text-micro text-muted-foreground">{executions.isError ? t(($) => $.execution_review.failed) : execution
          ? t(($) => $.execution_review.version, {attempt: execution.attempt, revision: execution.result_revision}) : t(($) => $.execution_review.waiting)}</p>
      </div>
      <div className="flex shrink-0 flex-wrap items-center gap-2">
        {execution && <ExecutionReworkControl instance={instance} execution={execution} disabled={executions.isFetching || executions.isError || pending} />}
      <Button size="sm" variant="outline" disabled={!execution || execution.result_revision < 1} onClick={() => setOpen(true)}>{t(($) => $.execution_review.open)}</Button>
      </div>
    </div>
    <Dialog open={open} onOpenChange={(value) => {if (!pending) setOpen(value);}}>
      <DialogContent className={workflowOperationDialogClassName}>
        <DialogHeader><DialogTitle>{t(($) => $.execution_review.title)}</DialogTitle><DialogDescription>{t(($) => $.execution_review.description, {revision})}</DialogDescription></DialogHeader>
        {result.isPending ? <p role="status">{t(($) => $.execution_review.loading)}</p> : !validDelivery || result.isError ? <p role="alert">{t(($) => $.execution_review.failed)}</p> : <DeliveryResult instanceId={instance.id} result={delivery} />}
        {gate.type === "hybrid" && <p className="text-caption text-muted-foreground">{t(($) => $.execution_review.hybrid_sequence)}</p>}
        {canReview ? review.isPending ? <p role="status">{t(($) => $.execution_review.loading)}</p> : !validReview || !validDelivery || result.isFetching || result.isError ? <div className="space-y-2">
          <p role="alert">{t(($) => $.execution_review.unavailable)}</p>
          <Button variant="outline" disabled={review.isFetching} onClick={() => {void review.refetch();void result.refetch();}}>{t(($) => $.execution_review.refresh)}</Button>
        </div> : <HumanDecisionForm key={`${instance.id}:${execution?.id}:${evidence.basis_hash}`} instance={instance} evidence={evidence} onPending={setPending} onRecorded={() => setOpen(false)} />
        : <p className="text-caption text-muted-foreground">{((gate.type === "human" || gate.type === "hybrid") ? t(($) => $.execution_review.waiting_reviewer) : t(($) => $.execution_review.read_only))}</p>}
      </DialogContent>
    </Dialog>
  </>;
}

function DeliveryResult({instanceId,result}: {instanceId: string; result: WorkflowResult}) {
  const {t} = useT("workflows");
  const [downloading, setDownloading] = useState<string | null>(null);
  const download = async (artifact: WorkflowResult["artifacts"][number]) => {
    setDownloading(artifact.id);
    try {
      // Construct the authenticated route from identities, never a server URL.
      const blob = await api.getWorkflowArtifactBlob(instanceId, result.execution_id, result.revision, artifact.id);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");link.href = url;link.download = artifact.name;link.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch {toast.error(t(($) => $.execution_review.download_failed));} finally {setDownloading(null);}
  };
  return <div className="space-y-3">
    <p className="text-caption font-medium">{t(($) => $.execution_review.result)}</p>
    <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted/30 p-3 text-caption">{JSON.stringify(result.data, null, 2)}</pre>
    <p className="text-caption font-medium">{t(($) => $.execution_review.artifacts)}</p>
    {result.artifacts.length === 0 ? <p className="text-caption text-muted-foreground">{t(($) => $.execution_review.no_artifacts)}</p> : <ul className="space-y-2">{result.artifacts.map((artifact) => <li key={artifact.id} className="flex items-center justify-between gap-3 rounded-md border p-2">
      <span className="min-w-0 break-all text-caption">{artifact.name}</span>
      <Button size="sm" variant="outline" aria-label={t(($) => $.execution_review.download_named, {name: artifact.name})} disabled={downloading !== null} onClick={() => void download(artifact)}>{t(($) => $.execution_review.download)}</Button>
    </li>)}</ul>}
  </div>;
}

function HumanDecisionForm({instance,evidence,onPending,onRecorded}: {instance: WorkflowInstance; evidence: WorkflowHumanReview; onPending: (pending:boolean) => void; onRecorded: () => void}) {
  const {t} = useT("workflows");
  const mutation = useRecordWorkflowHumanDecision(instance.workspace_id);
  const [checked, setChecked] = useState(false);
  const [note, setNote] = useState("");
  const [failed, setFailed] = useState(false);
  const submit = async (outcome: "approved" | "rejected") => {
    if (!checked || failed || mutation.isPending) return;
    onPending(true);
    try {
      await mutation.mutateAsync({instanceId: instance.id, executionId: evidence.execution_id, data: {expected_result_revision: evidence.result_revision, expected_basis_hash: evidence.basis_hash, outcome, note}});
      toast.success(t(($) => $.execution_review.recorded));onRecorded();
    } catch {setFailed(true);setChecked(false);} finally {onPending(false);}
  };
  return <div className="space-y-3 border-t pt-3">
    <p className="text-caption font-medium">{t(($) => $.execution_review.tasks)}</p>
    <ul className="space-y-1 text-caption">{evidence.issues.map((issue) => <li key={issue.id} className="break-words">{issue.title}</li>)}</ul>
    <p className="text-micro text-muted-foreground">{t(($) => $.execution_review.calls, {count:evidence.calls.length})}</p>
    <label className="block space-y-1 text-caption"><span>{t(($) => $.execution_review.note)}</span><Textarea value={note} onChange={(event) => setNote(event.target.value)} disabled={mutation.isPending} /></label>
    <label className="flex items-start gap-2 text-caption"><input type="checkbox" className="mt-0.5" checked={checked} disabled={mutation.isPending || failed} onChange={(event) => setChecked(event.target.checked)} />{t(($) => $.execution_review.confirm, {revision:evidence.result_revision})}</label>
    {failed && <p role="alert" className="text-caption text-destructive">{t(($) => $.execution_review.conflict)}</p>}
    <div className="flex justify-end gap-2"><Button variant="outline" disabled={!checked || failed || mutation.isPending} onClick={() => void submit("rejected")}>{t(($) => $.execution_review.reject)}</Button><Button disabled={!checked || failed || mutation.isPending} onClick={() => void submit("approved")}>{t(($) => $.execution_review.approve)}</Button></div>
  </div>;
}
