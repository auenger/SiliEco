"use client";
import { useMemo, useState } from "react";
import { applyWorkflowChatHistory, parseWorkflowDraft, useWorkflowChatEditorStore, workflowDraftFromPrompt, type WorkflowFillResult } from "@silieco/core/workflows";
import { useChatStore } from "@silieco/core/chat";
import { Button } from "@silieco/ui/components/ui/button";
import { useT } from "../../i18n";

export function WorkflowChatReplyAction({ wsId, sessionId, taskId, prompt, content, pending }: {
  wsId: string; sessionId: string; taskId: string; prompt: string; content: string; pending: boolean;
}) {
  const { t } = useT("workflows");
  const editor = useWorkflowChatEditorStore((s) => s.editor);
  const request = useWorkflowChatEditorStore((s) => s.request);
  const [result, setResult] = useState<WorkflowFillResult | null>(null);
  const valid = useMemo(() => !!parseWorkflowDraft(content) && !!workflowDraftFromPrompt(prompt), [content, prompt]);
  if (editor?.wsId !== wsId || !content.includes("<sop_draft>")) return null;
  const fill = (resolve = false) => {
    const next = applyWorkflowChatHistory(wsId, sessionId, taskId, prompt, content, resolve && result?.status === "conflict" ? result.current : undefined);
    setResult(next);
    if (next.status === "applied") useChatStore.getState().setExpanded(false);
  };
  const feedback = result?.status === "applied" ? t(($) => $.editor.filled)
    : result?.status === "unchanged" ? t(($) => $.editor.fill_unchanged)
    : result?.status === "conflict" ? (result.conflicts.length ? t(($) => $.editor.fill_review_conflicts) : t(($) => $.editor.fill_structure_changed))
    : result?.status === "unavailable" ? t(($) => $.editor.fill_unavailable)
    : result?.status === "invalid" || !valid ? t(($) => $.editor.fill_invalid) : t(($) => $.editor.reply_draft);
  const labels = { name: t(($) => $.editor.field_name), description: t(($) => $.editor.field_description), input: t(($) => $.editor.field_input), output: t(($) => $.editor.field_output) };
  return <div className="mt-2 space-y-2 rounded-md border bg-muted/30 p-2 text-caption">
    <div className="flex items-center gap-2">
      <span className="flex-1" role="status">{feedback}</span>
      <Button size="sm" variant="outline" disabled={!valid || pending || !!request} onClick={() => fill()}>{t(($) => $.editor.fill_reply)}</Button>
    </div>
    {result?.status === "conflict" && result.conflicts.length > 0 && <>
      <div className="max-h-64 space-y-3 overflow-y-auto">
        {result.conflicts.map((conflict, index) => <div key={index} className="space-y-1">
          <p className="font-medium">{conflict.stage} · {labels[conflict.field]}</p>
          <p>{t(($) => $.editor.current_content)}</p><pre className="whitespace-pre-wrap break-words rounded bg-background p-2 font-sans">{conflict.current}</pre>
          <p>{t(($) => $.editor.reply_content)}</p><pre className="whitespace-pre-wrap break-words rounded bg-background p-2 font-sans">{conflict.proposed}</pre>
        </div>)}
      </div>
      <Button size="sm" disabled={pending || !!request} onClick={() => fill(true)}>{t(($) => $.editor.use_reply)}</Button>
    </>}
  </div>;
}
