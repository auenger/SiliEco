"use client";

import { workflowOperationDialogClassName } from "./workflow-dialog";
import { useState } from "react";
import { useAuthStore } from "@silieco/core/auth";
import { useCorrectWorkflowExecutionInputs } from "@silieco/core/workflows";
import type { WorkflowExecution, WorkflowInstance } from "@silieco/core/types";
import { Button } from "@silieco/ui/components/ui/button";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@silieco/ui/components/ui/dialog";
import { useT } from "../../i18n";

export function InputRecoveryControl({instance, execution}: {instance: WorkflowInstance; execution: WorkflowExecution}) {
  const {t} = useT("workflows");
  const userId = useAuthStore((state) => state.user?.id);
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const canCorrect = typeof userId === "string" && userId.length > 0 && execution.status === "blocked" && execution.failure_code === "input_unavailable" && instance.created_by === userId && instance.status === "active" && !instance.archived_at
    && instance.current_stage_id === execution.stage_id && instance.revision === execution.plan_revision;
  return <div className="space-y-2 rounded-lg border border-border p-3">
    <p className="text-caption font-medium">{t(($) => $.input_recovery.blocked)}</p>
    <p className="break-words text-caption text-muted-foreground">{execution.failure_detail || t(($) => $.input_recovery.description)}</p>
    {canCorrect && <Button size="sm" variant="outline" onClick={() => setOpen(true)}>{t(($) => $.input_recovery.open)}</Button>}
    <Dialog open={open} onOpenChange={(value) => {if (!pending) setOpen(value);}}>
      <DialogContent className={workflowOperationDialogClassName}>
        <DialogHeader><DialogTitle>{t(($) => $.input_recovery.open)}</DialogTitle><DialogDescription>{t(($) => $.input_recovery.description)}</DialogDescription></DialogHeader>
        {open && canCorrect && <InputRecoveryForm key={`${instance.id}:${execution.id}:${JSON.stringify(instance.inputs ?? {})}`} instance={instance} execution={execution} onPending={setPending} onDone={() => setOpen(false)} />}
      </DialogContent>
    </Dialog>
  </div>;
}

function InputRecoveryForm({instance, execution, onPending, onDone}: {instance: WorkflowInstance; execution: WorkflowExecution; onPending: (value:boolean) => void; onDone: () => void}) {
  const {t} = useT("workflows");
  const mutation = useCorrectWorkflowExecutionInputs(instance.workspace_id);
  const [expected] = useState(() => instance.inputs ?? {});
  const [text, setText] = useState(() => JSON.stringify(expected, null, 2));
  const [failed, setFailed] = useState(false);
  let inputs: Record<string,unknown> | null = null;
  try {const parsed: unknown = JSON.parse(text);if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) inputs = parsed as Record<string,unknown>;} catch { /* Keep incomplete input editable. */ }
  const submit = async () => {
    if (!inputs || failed || mutation.isPending) return;
    onPending(true);
    try {await mutation.mutateAsync({instanceId:instance.id, executionId:execution.id, data:{expected_inputs:expected,inputs}});onDone();}
    catch {setFailed(true);} finally {onPending(false);}
  };
  return <div className="space-y-3">
    <label className="block space-y-2 text-caption"><span>{t(($) => $.input_recovery.inputs)}</span><Textarea className="min-h-48 font-mono" value={text} onChange={(event) => setText(event.target.value)} disabled={mutation.isPending || failed} /></label>
    {!inputs && <p role="alert" className="text-caption text-destructive">{t(($) => $.input_recovery.invalid)}</p>}
    {failed && <p role="alert" className="text-caption text-destructive">{t(($) => $.input_recovery.failed)}</p>}
    <div className="flex justify-end border-t pt-4"><Button disabled={!inputs || failed || mutation.isPending} onClick={() => void submit()}>{t(($) => $.input_recovery.submit)}</Button></div>
  </div>;
}
