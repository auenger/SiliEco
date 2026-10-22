import { create } from "zustand";
import { parseWorkflowDraft, workflowDesignerPrompt, type WorkflowTextDraft } from "./designer";

type Editor = {
  id: string; wsId: string; projectId?: string; portalId: string;
  read: () => WorkflowTextDraft;
  selection: () => string;
  apply: (draft: WorkflowTextDraft) => void;
  snapshot: () => () => void;
};
type Request = { editorId: string; baseline: WorkflowTextDraft; sessionId: string; taskId?: string };
type State = {
  editor: Editor | null; sessionId: string | null; request: Request | null;
  taskIds: string[]; status: "ready" | "applied" | "conflict" | "invalid";
  undo: (() => void) | null;
};
export const useWorkflowChatEditorStore = create<State>(() => ({
  editor: null, sessionId: null, request: null, taskIds: [], status: "ready", undo: null,
}));
export function attachWorkflowEditor(editor: Editor) {
  useWorkflowChatEditorStore.setState({ editor, sessionId: null, request: null, taskIds: [], status: "ready", undo: null });
}
export function detachWorkflowEditor(id: string) {
  if (useWorkflowChatEditorStore.getState().editor?.id === id) {
    useWorkflowChatEditorStore.setState({ editor: null, request: null, undo: null, taskIds: [], sessionId: null });
  }
}
export function prepareWorkflowChat(wsId: string, sessionId: string, content: string) {
  const state = useWorkflowChatEditorStore.getState();
  if (!state.editor || state.editor.wsId !== wsId || (state.sessionId && state.sessionId !== sessionId)) return null;
  const baseline = state.editor.read();
  const request: Request = { editorId: state.editor.id, baseline, sessionId };
  useWorkflowChatEditorStore.setState({ sessionId, request, status: "ready", undo: null });
  return { request, content: workflowDesignerPrompt(`${content}\n\nSelected stage: ${state.editor.selection()}`, baseline) };
}
export function acceptWorkflowChat(request: Request, taskId: string) {
  const state = useWorkflowChatEditorStore.getState();
  if (state.request !== request) return;
  useWorkflowChatEditorStore.setState({ request: { ...request, taskId }, taskIds: [...state.taskIds, taskId] });
}
export function failWorkflowChat(request: Request) {
  if (useWorkflowChatEditorStore.getState().request === request) useWorkflowChatEditorStore.setState({ request: null });
}

/** Three-way merge: preserve manual edits to fields the agent did not change. */
export function mergeWorkflowDraft(base: WorkflowTextDraft, current: WorkflowTextDraft, proposed: WorkflowTextDraft): WorkflowTextDraft | null {
  if (current.stages.length !== base.stages.length || current.stages.some((s, i) => s.id !== base.stages[i]?.id)) return null;
  if (proposed.stages.some((s) => s.id && !base.stages.some((b) => b.id === s.id))) return null;
  let conflict = false;
  const field = (b: string, c: string, p: string) => {
    if (p === b) return c;
    if (c !== b && c !== p) conflict = true;
    return p;
  };
  const result = {
    name: field(base.name, current.name, proposed.name),
    description: field(base.description, current.description, proposed.description),
    stages: current.stages.map((s, i) => {
      const b = base.stages[i]!;
      const p = proposed.stages.find((item) => item.id === s.id);
      if (!p) return s;
      return { ...s, name: field(b.name, s.name, p.name), description: field(b.description, s.description, p.description), input: field(b.input, s.input, p.input), output: field(b.output, s.output, p.output) };
    }).concat(proposed.stages.filter((s) => !s.id)),
  };
  return conflict ? null : result;
}
export function finishWorkflowChat(wsId: string, sessionId: string, taskId: string, content: string) {
  const state = useWorkflowChatEditorStore.getState();
  const { editor, request } = state;
  if (!editor || editor.wsId !== wsId || !request || request.editorId !== editor.id || request.sessionId !== sessionId || request.taskId !== taskId) return;
  useWorkflowChatEditorStore.setState({ request: null });
  const proposed = parseWorkflowDraft(content);
  if (!proposed) {
    if (content.includes("<sop_draft>")) useWorkflowChatEditorStore.setState({ status: "invalid" });
    return;
  }
  const current = editor.read();
  const merged = mergeWorkflowDraft(request.baseline, current, proposed);
  if (!merged) { useWorkflowChatEditorStore.setState({ status: "conflict" }); return; }
  // Capture immediately before applying, including unrelated manual settings.
  const restore = editor.snapshot();
  editor.apply(merged);
  useWorkflowChatEditorStore.setState({ status: "applied", undo: restore });
}
