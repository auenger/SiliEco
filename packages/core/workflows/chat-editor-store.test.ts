import { beforeEach, describe, expect, it, vi } from "vitest";
import { attachWorkflowEditor, detachWorkflowEditor, prepareWorkflowChat, acceptWorkflowChat, finishWorkflowChat, mergeWorkflowDraft, useWorkflowChatEditorStore } from "./chat-editor-store";
import type { WorkflowTextDraft } from "./designer";
const base: WorkflowTextDraft = { name: "SOP", description: "Goal", stages: [{ id: "ba", name: "BA", description: "Analyze", input: "Sources", output: "Requirements" }] };
const proposed = { ...base, stages: [{ ...base.stages[0]!, output: "Cited requirements" }] };
const block = (draft: WorkflowTextDraft) => `<sop_draft>${JSON.stringify(draft)}</sop_draft>`;
describe("floating chat SOP fill", () => {
  const apply = vi.fn(); const restore = vi.fn();
  beforeEach(() => { vi.clearAllMocks(); attachWorkflowEditor({ id: "editor", wsId: "ws", portalId: "target", read: () => base, selection: () => "ba", apply, snapshot: () => restore }); });
  it("fills only the matching workspace, session and task, once", () => {
    const sent = prepareWorkflowChat("ws", "session", "Add citations")!;
    expect(sent.content).toContain("Selected stage: ba");
    acceptWorkflowChat(sent.request, "task");
    finishWorkflowChat("other", "session", "task", block(proposed));
    finishWorkflowChat("ws", "other", "task", block(proposed));
    finishWorkflowChat("ws", "session", "old-task", block(proposed));
    expect(apply).not.toHaveBeenCalled();
    finishWorkflowChat("ws", "session", "task", block(proposed));
    finishWorkflowChat("ws", "session", "task", block(proposed));
    expect(apply).toHaveBeenCalledExactlyOnceWith(proposed);
    useWorkflowChatEditorStore.getState().undo?.();
    expect(restore).toHaveBeenCalledOnce();
  });
  it("preserves unrelated manual edits and rejects conflicting edits", () => {
    expect(mergeWorkflowDraft(base, { ...base, name: "My title" }, proposed)?.name).toBe("My title");
    const manual = { ...base, stages: [{ ...base.stages[0]!, output: "Manual criteria" }] };
    expect(mergeWorkflowDraft(base, manual, proposed)).toBeNull();
    expect(mergeWorkflowDraft(base, base, { ...proposed, stages: [{ ...proposed.stages[0]!, id: "unknown" }] })).toBeNull();
  });
  it("ignores late responses after editor unmount and other conversations", () => {
    const sent = prepareWorkflowChat("ws", "session", "Edit")!;
    acceptWorkflowChat(sent.request, "task");
    expect(prepareWorkflowChat("ws", "another-session", "Hi")).toBeNull();
    detachWorkflowEditor("editor");
    finishWorkflowChat("ws", "session", "task", block(proposed));
    expect(apply).not.toHaveBeenCalled();
  });
  it("rejects malformed replies without changing the form", () => {
    const sent = prepareWorkflowChat("ws", "session", "Edit")!;
    acceptWorkflowChat(sent.request, "task");
    finishWorkflowChat("ws", "session", "task", "<sop_draft>{}</sop_draft>");
    expect(apply).not.toHaveBeenCalled();
    expect(useWorkflowChatEditorStore.getState().status).toBe("invalid");
  });
});
