import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "../api";
import { useAuthStore } from "../auth";
import { useChatStore } from "../chat";
import { chatKeys, chatSessionsOptions } from "../chat/queries";
import { agentListOptions, memberListOptions } from "../workspace/queries";
import { canAssignAgentToTask } from "../permissions";
import { getCurrentWsId } from "../platform/workspace-storage";
import type { ChatSession } from "../types";
import { feedbackDraftKey, usePageFeedbackStore, type FeedbackDraft, type FeedbackItem } from "./draft-store";
import { prepareFeedbackChat } from "./prepare-chat";
import type { BrowserContext } from "./protocol";

export function useFeedbackChat(context: BrowserContext, image: (item: FeedbackItem) => Promise<File>) {
  const qc = useQueryClient();
  const key = feedbackDraftKey(context);
  return useMutation({
    mutationFn: async ({ draft, instructions, title, noAgent }: { draft: FeedbackDraft; instructions: string; title: string; noAgent: string }) => {
      const store = usePageFeedbackStore.getState();
      if (store.pending[key]) throw new Error("This feedback is already being prepared");
      store.setPending(key, true);
      const guard = () => {
        if (getCurrentWsId() !== context.workspaceId || useAuthStore.getState().user?.id !== context.userId) throw new Error("Workspace or account changed. Return to the original workspace and retry.");
      };
      try {
        guard();
        const [agents, members, sessions] = await Promise.all([
          qc.fetchQuery(agentListOptions(context.workspaceId)),
          qc.fetchQuery(memberListOptions(context.workspaceId)),
          qc.fetchQuery(chatSessionsOptions(context.workspaceId)),
        ]);
        guard();
        const role = members.find((member) => member.user_id === context.userId)?.role ?? null;
        const available = agents.filter((agent) => !agent.archived_at && canAssignAgentToTask(agent, { userId: context.userId, role }).allowed);
        const chat = useChatStore.getState();
        const activeAgentId = sessions.find((session) => session.id === chat.activeSessionId)?.agent_id;
        const agent = available.find((a) => a.id === activeAgentId) ?? available.find((a) => a.id === chat.selectedAgentId) ?? available[0];
        if (!agent) throw new Error(noAgent);
        let session: ChatSession | null = null;
        if (draft.chatSessionId) {
          try { session = await api.getChatSession(draft.chatSessionId); }
          catch (error) { if (!(error instanceof ApiError && error.status === 404)) throw error; }
          guard();
          if (session && (session.status === "archived" || !available.some((a) => a.id === session!.agent_id))) session = null;
        }
        const result = await prepareFeedbackChat(draft, {
          api, image, session, agentId: agent.id, title, instructions, guard,
          checkpoint: (patch) => usePageFeedbackStore.getState().patch(key, patch),
          stage: (sessionId, content, attachments) => {
            const live = useChatStore.getState();
            for (const attachment of attachments) live.addInputDraftAttachment(sessionId, attachment);
            live.appendToInputDraft(sessionId, content);
          },
        });
        guard();
        // Seed the session before selecting it so chat's stale-session guard
        // does not erase the freshly-created conversation while refetching.
        qc.setQueryData<ChatSession[]>(chatKeys.sessions(context.workspaceId), (current) => [result, ...(current ?? []).filter((item) => item.id !== result.id)]);
        return { sessionId: result.id, agentId: result.agent_id };
      } finally {
        usePageFeedbackStore.getState().setPending(key, false);
        void qc.invalidateQueries({ queryKey: chatKeys.sessions(context.workspaceId) });
      }
    },
  });
}
