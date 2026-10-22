import { act, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import WorkflowChatBridge from "./workflow-chat-bridge";
import { acceptWorkflowChat, detachWorkflowEditor, finishWorkflowChat, prepareWorkflowChat, useWorkflowChatEditorStore } from "@silieco/core/workflows";
const chat = vi.hoisted(() => ({ setActiveSession: vi.fn(), setSelectedProjectId: vi.fn(), setOpen: vi.fn() }));
vi.mock("@silieco/core/chat", () => ({ useChatStore: { getState: () => chat } }));
vi.mock("@silieco/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
const draft = { name: "SOP", description: "", stages: [{ id: "stage", name: "BA", description: "", input: "", output: "" }] };
describe("SOP floating chat bridge", () => {
  beforeEach(() => { vi.clearAllMocks(); useWorkflowChatEditorStore.setState({ editor: null, request: null, undo: null }); });
  it("opens the existing floating chat with the project and releases its target on unmount", () => {
    const { unmount } = render(<WorkflowChatBridge open projectId="project" draft={draft} revision="1" selection="stage" onApply={vi.fn()} onRestore={vi.fn()} />);
    expect(chat.setActiveSession).toHaveBeenCalledWith(null);
    expect(chat.setSelectedProjectId).toHaveBeenCalledWith("project");
    expect(chat.setOpen).toHaveBeenCalledWith(true);
    expect(useWorkflowChatEditorStore.getState().editor?.read()).toEqual(draft);
    unmount();
    expect(useWorkflowChatEditorStore.getState().editor).toBeNull();
  });
  it("reconnects after disconnect and removes undo when newer edits occur", async () => {
    const onApply = vi.fn(); const onRestore = vi.fn();
    const props = { open: true, draft, revision: "1", selection: "stage", onApply, onRestore };
    const { rerender } = render(<WorkflowChatBridge {...props} />);
    act(() => {
      const sent = prepareWorkflowChat("ws", "session", "Edit")!;
      acceptWorkflowChat(sent.request, "task");
      finishWorkflowChat("ws", "session", "task", `<sop_draft>${JSON.stringify({ ...draft, name: "Updated" })}</sop_draft>`);
    });
    expect(onApply).toHaveBeenCalledOnce();
    expect(useWorkflowChatEditorStore.getState().undo).not.toBeNull();
    rerender(<WorkflowChatBridge {...props} revision="manual-edit" />);
    await waitFor(() => expect(useWorkflowChatEditorStore.getState().undo).toBeNull());
    act(() => detachWorkflowEditor(useWorkflowChatEditorStore.getState().editor!.id));
    rerender(<WorkflowChatBridge {...props} open={false} />);
    expect(useWorkflowChatEditorStore.getState().editor).not.toBeNull();
  });
});
