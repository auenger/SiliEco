import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import type {
  TaskStatus,
  TaskPriority,
  TaskAssigneeType,
  TaskPropertyValues,
} from "../../types";
import type { CreateMode } from "./create-mode-store";
import type { QuickCreateActorType } from "./quick-create-store";
import { createWorkspaceAwareStorage, registerForWorkspaceRehydration } from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
import { registerDraftCleanup } from "../../drafts/cleanup-registry";
import { normalizeStoredUploads, type DraftUpload } from "../../drafts/draft-upload";

// One logical Task-Create draft (SILI-5181), split so switching between the
// manual form and the agent form never destroys the other side's content.
//
//   shared  — belongs to the task no matter how it is filed: project,
//             priority, due date, attachments.
//   manual  — the manual form's own state: title, description, status, start
//             date, assignee, labels, custom properties.
//   agent   — the agent form's own state: the free-text prompt and the picked
//             actor (agent or squad).
//   activeMode — which form the draft is currently being edited in.
//
// Before this split, `switchToAgent` concatenated title + description into the
// prompt and then CLEARED the manual fields, and `switchToManual` copied the
// prompt into the description but left the old prompt behind — so a round-trip
// both lost manual content and resurfaced a stale prompt. With separate slots
// a switch is a no-op on the other side's data; the only cross-write is a
// one-time assist-init the panels perform when the target slot is still empty.

export interface TaskCreateShared {
  projectId?: string;
  priority: TaskPriority;
  dueDate: string | null;
  /** Uploads for the dialog (placeholders + completed), referenced by the
   *  manual description OR the agent prompt markdown. A single pool so an
   *  image survives a mode switch from either side; each submit path sends
   *  only the ids its own content references. Coordinator-owned (SILI-5181 L2):
   *  a placeholder written at pick time survives dialog close, and one still
   *  `uploading` at load time is dropped on rehydrate. */
  attachments: DraftUpload[];
}

export interface TaskCreateManual {
  title: string;
  description: string;
  status: TaskStatus;
  startDate: string | null;
  assigneeType?: TaskAssigneeType;
  assigneeId?: string;
  /** Label IDs chosen in the create dialog. Attached to the task right after
   *  it is created (the create endpoint takes no labels), so they are kept as
   *  a plain id list rather than full Label objects. */
  labelIds: string[];
  propertyValues: TaskPropertyValues;
}

export interface TaskCreateAgent {
  prompt: string;
  actorType?: QuickCreateActorType;
  actorId?: string;
}

export interface TaskCreateDraft {
  shared: TaskCreateShared;
  manual: TaskCreateManual;
  agent: TaskCreateAgent;
  activeMode: CreateMode;
}

const emptyShared = (): TaskCreateShared => ({
  projectId: undefined,
  priority: "none",
  dueDate: null,
  attachments: [],
});

const emptyManual = (): TaskCreateManual => ({
  title: "",
  description: "",
  status: "todo",
  startDate: null,
  assigneeType: undefined,
  assigneeId: undefined,
  labelIds: [],
  propertyValues: {},
});

const emptyAgent = (): TaskCreateAgent => ({
  prompt: "",
  actorType: undefined,
  actorId: undefined,
});

interface TaskDraftStore {
  draft: TaskCreateDraft;
  // Last assignee picked at submit time. Persisted across drafts so the
  // create-task modal can prefill the picker with the user's most recent
  // choice instead of always opening with no assignee.
  lastAssigneeType?: TaskAssigneeType;
  lastAssigneeId?: string;
  setShared: (patch: Partial<TaskCreateShared>) => void;
  setManual: (patch: Partial<TaskCreateManual>) => void;
  setAgent: (patch: Partial<TaskCreateAgent>) => void;
  setActiveMode: (mode: CreateMode) => void;
  clearDraft: () => void;
  setLastAssignee: (type?: TaskAssigneeType, id?: string) => void;
  hasDraft: () => boolean;
}

// Fill missing fields within the current storage generation. Old flat drafts
// are deliberately not interpreted as Task drafts.
function normalizeDraft(raw: unknown): TaskCreateDraft {
  const d = (raw && typeof raw === "object" ? raw : {}) as Record<string, unknown>;

  const sharedRaw = (d.shared as Partial<TaskCreateShared> & { attachments?: unknown }) ?? {};
  return {
    shared: {
      ...emptyShared(),
      ...sharedRaw,
      // Pre-L2 nested drafts stored bare Attachment rows; every load also
      // drops `uploading` placeholders (bytes are gone).
      attachments: normalizeStoredUploads(sharedRaw.attachments),
    },
    manual: { ...emptyManual(), ...((d.manual as Partial<TaskCreateManual>) ?? {}) },
    agent: { ...emptyAgent(), ...((d.agent as Partial<TaskCreateAgent>) ?? {}) },
    activeMode: d.activeMode === "agent" ? "agent" : "manual",
  };
}

export const useTaskDraftStore = create<TaskDraftStore>()(
  persist(
    (set, get) => ({
      draft: normalizeDraft(undefined),
      lastAssigneeType: undefined,
      lastAssigneeId: undefined,
      setShared: (patch) =>
        set((s) => ({ draft: { ...s.draft, shared: { ...s.draft.shared, ...patch } } })),
      setManual: (patch) =>
        set((s) => ({ draft: { ...s.draft, manual: { ...s.draft.manual, ...patch } } })),
      setAgent: (patch) =>
        set((s) => ({ draft: { ...s.draft, agent: { ...s.draft.agent, ...patch } } })),
      setActiveMode: (mode) =>
        set((s) => ({ draft: { ...s.draft, activeMode: mode } })),
      clearDraft: () =>
        set((s) => ({
          draft: {
            shared: emptyShared(),
            manual: {
              ...emptyManual(),
              assigneeType: s.lastAssigneeType,
              assigneeId: s.lastAssigneeId,
            },
            agent: emptyAgent(),
            activeMode: s.draft.activeMode,
          },
        })),
      setLastAssignee: (type, id) =>
        set({ lastAssigneeType: type, lastAssigneeId: id }),
      hasDraft: () => {
        const { manual, agent, shared } = get().draft;
        return !!(
          manual.title ||
          manual.description ||
          agent.prompt ||
          Object.keys(manual.propertyValues).length > 0 ||
          // Recoverable uploads only: a failed/interrupted remnant the user
          // never dismissed must not pin the sidebar's draft dot forever.
          shared.attachments.some((u) => u.status === "uploaded" || u.status === "uploading")
        );
      },
    }),
    {
      name: "silieco_tr1_task_draft",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
      merge: (persistedState, currentState) => {
        const persisted = (persistedState ?? {}) as Partial<TaskDraftStore> & {
          draft?: unknown;
        };
        return {
          ...currentState,
          ...persisted,
          draft: normalizeDraft(persisted.draft),
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useTaskDraftStore.persist.rehydrate());

registerDraftCleanup({
  storageKey: "silieco_tr1_task_draft",
  workspaceScoped: true,
  // Full reset, NOT clearDraft(): clearDraft deliberately keeps the
  // last-assignee preference and re-seeds it into the fresh draft's manual
  // slot — correct between drafts of one user, but on logout it would hand
  // the previous user's last-picked assignee to the next login on this tab.
  resetInMemory: () =>
    useTaskDraftStore.setState({
      draft: normalizeDraft(undefined),
      lastAssigneeType: undefined,
      lastAssigneeId: undefined,
    }),
});
