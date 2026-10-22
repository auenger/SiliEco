"use client";

import { useEffect, useId, useRef } from "react";
import { useChatStore } from "@silieco/core/chat";
import { useWorkspaceId } from "@silieco/core/hooks";
import { attachWorkflowEditor, detachWorkflowEditor, useWorkflowChatEditorStore, type WorkflowTextDraft } from "@silieco/core/workflows";

/** Registers the mounted editor as the floating conversation's fill target. */
export default function WorkflowChatBridge(props: {
  open: boolean; projectId?: string; draft: WorkflowTextDraft; selection: string;
  revision: string; onApply: (draft: WorkflowTextDraft) => void; onRestore: () => void;
}) {
  const wsId = useWorkspaceId();
  const id = useId();
  const portalId = `workflow-chat-${id}`;
  const latest = useRef(props);
  latest.current = props;
  const undo = useWorkflowChatEditorStore((s) => s.undo);
  const undoRevision = useRef<string | null>(null);
  useEffect(() => {
    // Any subsequent edit invalidates undo so it never erases newer work.
    if (!undo) undoRevision.current = null;
    else if (undoRevision.current === null) undoRevision.current = props.revision;
    else if (undoRevision.current !== props.revision) useWorkflowChatEditorStore.setState({ undo: null });
  }, [undo, props.revision]);
  useEffect(() => () => detachWorkflowEditor(id), [id, wsId]);
  useEffect(() => {
    const state = useWorkflowChatEditorStore.getState();
    const chat = useChatStore.getState();
    if (state.editor?.id !== id) {
      attachWorkflowEditor({ id, wsId, projectId: latest.current.projectId, portalId,
        read: () => latest.current.draft, selection: () => latest.current.selection,
        apply: (draft) => latest.current.onApply(draft), snapshot: () => latest.current.onRestore,
      });
      chat.setActiveSession(null);
      chat.setSelectedProjectId(latest.current.projectId ?? null);
    } else if (state.sessionId) chat.setActiveSession(state.sessionId);
    chat.setOpen(true);
  }, [id, wsId, portalId, props.open]);
  return <div id={portalId} className="pointer-events-none absolute inset-0 z-50" />;
}
