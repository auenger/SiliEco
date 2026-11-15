// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { createChatStore, registerChatStore, useChatStore, DRAFT_NEW_SESSION } from "../chat";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import { emptyFeedbackDraft, feedbackDraftKey, usePageFeedbackStore } from "./draft-store";
import { useFeedbackChat } from "./use-feedback-chat";

const auth = vi.hoisted(() => ({ user: { id: "user" } }));
vi.mock("../auth", () => ({ useAuthStore: Object.assign((selector: (state: typeof auth) => unknown) => selector(auth), { getState: () => auth }) }));

describe("feedback handoff through the existing chat draft", () => {
  const context = { userId: "user", workspaceId: "workspace", sessionId: "browser" };
  let qc: QueryClient;
  const session = { id: "feedback-chat", agent_id: "session-agent", status: "active", title: "Website changes" };
  const api = {
    listAgents: vi.fn(async () => [{ id: "session-agent", owner_id: "user" }, { id: "preference-agent", owner_id: "user" }]),
    listMembers: vi.fn(async () => [{ user_id: "user", role: "member" }]),
    listChatSessions: vi.fn(async () => [{ id: "other-chat", agent_id: "session-agent", status: "active" }]),
    createChatSession: vi.fn(async () => session),
    getChatSession: vi.fn(async () => session),
    sendChatMessage: vi.fn(), createTask: vi.fn(),
  };
  beforeEach(async () => {
    vi.clearAllMocks();
    setCurrentWorkspace("test", "workspace");
    await Promise.resolve();
    const memory = new Map<string, string>();
    registerChatStore(createChatStore({ storage: { getItem: (key) => memory.get(key) ?? null, setItem: (key, value) => { memory.set(key, value); }, removeItem: (key) => { memory.delete(key); } } }));
    const chat = useChatStore.getState();
    chat.setActiveSession("other-chat");
    chat.setSelectedAgentId("preference-agent");
    chat.setInputDraft("other-chat", "My ongoing conversation");
    chat.setInputDraft(DRAFT_NEW_SESSION, "My unsent new conversation");
    usePageFeedbackStore.setState({ drafts: {}, pending: {} });
    setApiInstance(api as unknown as ApiClient);
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });
  afterEach(() => qc.clear());
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  const input = { draft: { ...emptyFeedbackDraft, url: "https://example.com", request: "Improve contrast" }, title: "Website changes", instructions: "Analyze and create a task", noAgent: "No available agent" };

  it("uses the visible chat's agent, preserving unrelated drafts and the send pipeline", async () => {
    const { result } = renderHook(() => useFeedbackChat(context, vi.fn()), { wrapper });
    await act(async () => { await result.current.mutateAsync(input); });
    expect(api.createChatSession).toHaveBeenCalledWith(expect.objectContaining({ agent_id: "session-agent" }));
    const chat = useChatStore.getState();
    expect(chat.inputDrafts["other-chat"]).toBe("My ongoing conversation");
    expect(chat.inputDrafts[DRAFT_NEW_SESSION]).toBe("My unsent new conversation");
    expect(chat.inputDrafts["feedback-chat"]).toContain("Improve contrast");
    expect(chat.activeSessionId).toBe("other-chat");
    expect(api.sendChatMessage).not.toHaveBeenCalled();
    expect(api.createTask).not.toHaveBeenCalled();
    expect(usePageFeedbackStore.getState().drafts[feedbackDraftKey(context)]?.chatSessionId).toBe("feedback-chat");
  });
  it("does not stage a new draft into the destination workspace after a switch", async () => {
    api.createChatSession.mockImplementationOnce(async () => { setCurrentWorkspace("elsewhere", "other-workspace"); return session; });
    const { result } = renderHook(() => useFeedbackChat(context, vi.fn()), { wrapper });
    await act(async () => { await expect(result.current.mutateAsync(input)).rejects.toThrow("Workspace or account changed"); });
    expect(useChatStore.getState().inputDrafts["feedback-chat"]).toBeUndefined();
    expect(usePageFeedbackStore.getState().pending[feedbackDraftKey(context)]).toBe(false);
    expect(usePageFeedbackStore.getState().drafts[feedbackDraftKey(context)]?.chatSessionId).toBe("feedback-chat");
  });
});
