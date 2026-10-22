"use client";
import { useWorkflowChatEditorStore, detachWorkflowEditor } from "@silieco/core/workflows";
import { Button } from "@silieco/ui/components/ui/button";
import { useT } from "../../i18n";

export function WorkflowChatStatus() {
  const { t } = useT("workflows");
  const { editor, status, undo } = useWorkflowChatEditorStore();
  return <div className="flex shrink-0 items-center gap-2 border-b bg-muted/40 px-3 py-2 text-caption" role="status">
    <span className="min-w-0 flex-1">{status === "applied" ? t(($) => $.editor.filled) : status === "conflict" ? t(($) => $.editor.fill_conflict) : status === "invalid" ? t(($) => $.editor.fill_invalid) : t(($) => $.editor.chat_context)}</span>
    {undo && <Button size="sm" variant="ghost" onClick={() => { undo(); useWorkflowChatEditorStore.setState({ undo: null, status: "ready" }); }}>{t(($) => $.editor.undo)}</Button>}
    <Button size="sm" variant="ghost" onClick={() => { if (editor) detachWorkflowEditor(editor.id); }}>{t(($) => $.editor.detach)}</Button>
  </div>;
}
