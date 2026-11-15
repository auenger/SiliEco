import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import type { BrowserContext, PageCapture } from "./protocol";

export interface FeedbackMark { x: number; y: number; width: number; height: number }
export interface FeedbackItem { capture: PageCapture; note: string; marks: FeedbackMark[] }
export interface FeedbackDraft {
  url: string; title: string; request: string; items: FeedbackItem[];
  taskId?: string;
  uploads?: Record<string, { id: string; markdown: string }>;
  chatSessionId?: string;
  chatSignature?: string;
  chatUploads?: Record<string, string>;
}
export const emptyFeedbackDraft: FeedbackDraft = { url: "", title: "", request: "", items: [] };
export const feedbackDraftKey = (context: BrowserContext) => JSON.stringify([context.userId, context.workspaceId, context.sessionId]);
interface FeedbackStore {
  drafts: Record<string, FeedbackDraft>;
  pending: Record<string, boolean>;
  setPending: (key: string, value: boolean) => void;
  patch: (key: string, patch: Partial<FeedbackDraft>) => void;
  clear: (key: string) => void;
}
export const usePageFeedbackStore = create<FeedbackStore>()(persist((set) => ({
  drafts: {},
  pending: {},
  setPending: (key, value) => set((state) => ({ pending: { ...state.pending, [key]: value } })),
  patch: (key, patch) => set((state) => ({ drafts: { ...state.drafts, [key]: { ...(state.drafts[key] ?? emptyFeedbackDraft), ...patch } } })),
  clear: (key) => set((state) => { const drafts = { ...state.drafts }; delete drafts[key]; return { drafts }; }),
}), { name: "silieco_tr1_page_feedback", storage: createJSONStorage(() => defaultStorage), partialize: (state) => ({ drafts: state.drafts }), version: 1 }));
