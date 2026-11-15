import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useAuthStore } from "../auth";
import { getCurrentWsId } from "../platform/workspace-storage";
import { taskKeys } from "../tasks/queries";
import { submitFeedbackTask } from "./submit";
import { usePageFeedbackStore, feedbackDraftKey, type FeedbackDraft, type FeedbackItem } from "./draft-store";
import type { BrowserContext } from "./protocol";

export function useSubmitFeedback(context: BrowserContext, image: (item: FeedbackItem) => Promise<File>) {
  const qc = useQueryClient();
  const key = feedbackDraftKey(context);
  return useMutation({
    mutationFn: async (draft: FeedbackDraft) => {
      if (usePageFeedbackStore.getState().pending[key]) throw new Error("This feedback is already being submitted");
      usePageFeedbackStore.getState().setPending(key, true);
      try {
        return await submitFeedbackTask(draft, {
          api, image,
          guard: () => { if (getCurrentWsId() !== context.workspaceId || useAuthStore.getState().user?.id !== context.userId) throw new Error("Workspace or account changed. Return to the original workspace and retry."); },
          checkpoint: (patch) => usePageFeedbackStore.getState().patch(key, patch),
        });
      } finally { usePageFeedbackStore.getState().setPending(key, false); }
    },
    onSettled: () => { void qc.invalidateQueries({ queryKey: taskKeys.all(context.workspaceId) }); },
  });
}
