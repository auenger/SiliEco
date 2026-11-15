/**
 * Comment creation mutation. Mirrors the optimistic + invalidate pattern of
 * apps/mobile/data/mutations/inbox.ts:17 — operates on the flat
 * `TimelineEntry[]` timeline cache (ASC, oldest first; server-side
 * pagination was dropped in #2322).
 *
 * Optimistic strategy:
 *   - Cancel timeline refetches.
 *   - Snapshot the current cache.
 *   - Append a synthetic comment-typed TimelineEntry to the end of the list
 *     (newest position, since the array is ASC).
 *   - On error: roll back to the snapshot.
 *   - On settled: invalidate so the server's real comment row replaces the
 *     synthetic one (real id, real created_at).
 */
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type {
  CreateTaskRequest,
  Task,
  TaskReaction,
  Label,
  Reaction,
  TimelineEntry,
  UpdateTaskRequest,
} from "@silieco/core/types";
import { api } from "@/data/api";
import { agentRunsKeys } from "@/data/queries/agent-runs";
import { taskKeys } from "@/data/queries/tasks";
import { inboxKeys } from "@/data/queries/inbox";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useFailedCommentsStore } from "@/data/stores/failed-comments-store";

export type ToggleCommentReactionVars = {
  commentId: string;
  emoji: string;
  /** Pass the existing Reaction from the entry to indicate this is a remove.
   *  Undefined means "add". Mirrors web's ToggleCommentReactionVars shape so
   *  call sites stay portable across clients. */
  existing?: Reaction;
};

export type ToggleTaskReactionVars = {
  emoji: string;
  /** See above. */
  existing?: TaskReaction;
};

export type CreateCommentVars = {
  content: string;
  /** When set, the new comment is a threaded reply to this comment id. */
  parentId?: string;
  /** Attachment ids previously returned by `useFileAttach.pickAndUpload*`,
   *  filtered by the caller to only those whose `url` is still referenced
   *  in `content`. The server re-parents each attachment from task-scoped
   *  to comment-scoped so a `DELETE /comment/:id` cascades the attachments
   *  too. Mirrors web's `CommentInput.handleSubmit` `activeIds` derivation
   *  (`packages/views/tasks/components/comment-input.tsx:76-78`). */
  attachmentIds?: string[];
};

export function useCreateComment(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationFn: ({ content, parentId, attachmentIds }: CreateCommentVars) =>
      api.createComment(taskId, content, { parentId, attachmentIds }),
    onMutate: async ({ content, parentId }) => {
      const key = taskKeys.timeline(wsId, taskId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      if (!userId) return { prev, key, optimisticId: null };

      const optimisticId = `optimistic-${Date.now()}`;
      const optimistic: TimelineEntry = {
        type: "comment",
        id: optimisticId,
        actor_type: "member",
        actor_id: userId,
        content,
        parent_id: parentId ?? null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        comment_type: "comment",
        reactions: [],
        attachments: [],
      };

      // ASC list: new comment goes to the end (newest position on screen).
      qc.setQueryData<TimelineEntry[]>(key, (old) =>
        old ? [...old, optimistic] : [optimistic],
      );

      return { prev, key, optimisticId };
    },
    onError: (err, vars, ctx) => {
      // Deliberately do NOT roll back here — the optimistic entry stays in
      // the cache so the user sees an inline "Failed · Retry · Discard"
      // affordance instead of having their typed comment vanish into a
      // toast. The matching failed-comments store entry carries the
      // payload needed to re-fire the same submission. Note: this is the
      // only mutation in the tasks file that intentionally avoids
      // rollback — every other path treats error as "undo the optimism".
      if (ctx?.optimisticId) {
        useFailedCommentsStore.getState().markFailed(ctx.optimisticId, {
          content: vars.content,
          parentId: vars.parentId,
          attachmentIds: vars.attachmentIds,
          error: err instanceof Error ? err.message : "Send failed",
        });
      }
    },
    // Invalidate only on success — the failed-comment path needs the cache
    // to keep its optimistic entry so the inline retry UI has something to
    // render against. The success refetch replaces the synthetic id with
    // the server-issued one (same ASC bottom position).
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: taskKeys.timeline(wsId, taskId),
      });
    },
  });
}

/**
 * Drop a failed-but-still-rendered optimistic comment from both the cache
 * and the failed-comments store. Mirrors the Discard half of the inline
 * "Failed · Retry · Discard" affordance on a failed CommentBody.
 */
export function discardFailedComment(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  taskId: string,
  optimisticId: string,
) {
  qc.setQueryData<TimelineEntry[]>(
    taskKeys.timeline(wsId, taskId),
    (old) => (old ? old.filter((e) => e.id !== optimisticId) : old),
  );
  useFailedCommentsStore.getState().clear(optimisticId);
}

/**
 * Toggle a reaction on a comment. Cache target is the timeline infinite
 * query — comment reactions ride on `TimelineEntry.reactions[]` inside each
 * page.entries, so no separate query is involved.
 *
 * Optimistic strategy mirrors useCreateComment: cancel → snapshot → mutate
 * cache → on error rollback → on settle invalidate (so the synthetic
 * reaction id is replaced by the server's real id).
 *
 * Argument shape mirrors web's `ToggleCommentReactionVars` so the eventual
 * migration to the web pattern (useMutationState-derived optimistic state,
 * once WS lands) does not require changing trigger code.
 */
export function useToggleCommentReaction(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationKey: ["toggleCommentReaction", taskId] as const,
    mutationFn: async ({
      commentId,
      emoji,
      existing,
    }: ToggleCommentReactionVars) => {
      if (existing) {
        await api.removeReaction(commentId, emoji);
        return null;
      }
      return api.addReaction(commentId, emoji);
    },
    onMutate: async ({ commentId, emoji, existing }) => {
      const key = taskKeys.timeline(wsId, taskId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      if (!userId) return { prev, key };

      qc.setQueryData<TimelineEntry[]>(key, (old) => {
        if (!old) return old;
        return old.map((entry) => {
          if (entry.id !== commentId) return entry;
          const reactions = entry.reactions ?? [];
          if (existing) {
            return {
              ...entry,
              reactions: reactions.filter((r) => r.id !== existing.id),
            };
          }
          const optimistic: Reaction = {
            id: `optimistic-${emoji}-${Date.now()}`,
            comment_id: commentId,
            actor_type: "member",
            actor_id: userId,
            emoji,
            created_at: new Date().toISOString(),
          };
          return { ...entry, reactions: [...reactions, optimistic] };
        });
      });
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.timeline(wsId, taskId) });
    },
  });
}

/**
 * Edit an existing comment. Replaces `content` (and optionally
 * `attachment_ids`) on the server and patches the matching TimelineEntry
 * in the timeline cache. Mirrors web `useEditComment` semantics.
 *
 * Server returns the full updated Comment; we map it into the timeline's
 * TimelineEntry shape (`type: "comment"`, the rest of Comment fields flat).
 */
export function useEditComment(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: ({
      commentId,
      content,
      attachmentIds,
    }: {
      commentId: string;
      content: string;
      attachmentIds?: string[];
    }) => api.updateComment(commentId, content, attachmentIds),
    onMutate: async ({ commentId, content }) => {
      const key = taskKeys.timeline(wsId, taskId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      qc.setQueryData<TimelineEntry[]>(key, (old) =>
        old?.map((entry) =>
          entry.type === "comment" && entry.id === commentId
            ? {
                ...entry,
                content,
                updated_at: new Date().toISOString(),
              }
            : entry,
        ),
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.timeline(wsId, taskId) });
    },
  });
}

/**
 * Delete a comment. Strips the matching TimelineEntry (and any replies
 * with parent_id === commentId) from the timeline cache optimistically.
 * Backend cascades reply deletion server-side; we mirror the cascade
 * locally so the optimistic patch leaves no orphans on screen.
 */
export function useDeleteComment(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (commentId: string) => api.deleteComment(commentId),
    onMutate: async (commentId) => {
      const key = taskKeys.timeline(wsId, taskId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      qc.setQueryData<TimelineEntry[]>(key, (old) =>
        old?.filter(
          (entry) =>
            !(
              entry.type === "comment" &&
              (entry.id === commentId || entry.parent_id === commentId)
            ),
        ),
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.timeline(wsId, taskId) });
    },
  });
}

/**
 * Resolve / unresolve a comment thread (root comments only — server rejects
 * the call on a reply). Toggle is driven by the `resolved` boolean param:
 * true → POST /resolve, false → DELETE /resolve. Mirrors web
 * `useResolveComment(commentId, resolved)` semantics.
 *
 * Optimistic patch sets `resolved_at` to now (or null on unresolve) so the
 * UI flips immediately; server response replaces with authoritative values.
 */
export function useResolveComment(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: ({
      commentId,
      resolved,
    }: {
      commentId: string;
      resolved: boolean;
    }) =>
      resolved
        ? api.resolveComment(commentId)
        : api.unresolveComment(commentId),
    onMutate: async ({ commentId, resolved }) => {
      const key = taskKeys.timeline(wsId, taskId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<TimelineEntry[]>(key);
      const now = resolved ? new Date().toISOString() : null;
      qc.setQueryData<TimelineEntry[]>(key, (old) =>
        old?.map((entry) =>
          entry.type === "comment" && entry.id === commentId
            ? { ...entry, resolved_at: now }
            : entry,
        ),
      );
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.timeline(wsId, taskId) });
    },
  });
}

/**
 * Toggle a reaction on the task itself. Cache target is the task detail
 * query — Task.reactions is an optional array on the Task object.
 *
 * Mobile reads task reactions directly off the detail cache (no separate
 * query like web's taskReactionsOptions). Single source of truth, less
 * code, fewer requests.
 */
export function useToggleTaskReaction(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  return useMutation({
    mutationKey: ["toggleTaskReaction", taskId] as const,
    mutationFn: async ({ emoji, existing }: ToggleTaskReactionVars) => {
      if (existing) {
        await api.removeTaskReaction(taskId, emoji);
        return null;
      }
      return api.addTaskReaction(taskId, emoji);
    },
    onMutate: async ({ emoji, existing }) => {
      const key = taskKeys.detail(wsId, taskId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Task>(key);
      if (!userId || !prev) return { prev, key };

      const reactions = prev.reactions ?? [];
      let nextReactions: TaskReaction[];
      if (existing) {
        nextReactions = reactions.filter((r) => r.id !== existing.id);
      } else {
        const optimistic: TaskReaction = {
          id: `optimistic-${emoji}-${Date.now()}`,
          task_id: taskId,
          actor_type: "member",
          actor_id: userId,
          emoji,
          created_at: new Date().toISOString(),
        };
        nextReactions = [...reactions, optimistic];
      }
      qc.setQueryData<Task>(key, { ...prev, reactions: nextReactions });
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.detail(wsId, taskId) });
    },
  });
}

/**
 * Update a task's editable fields (status / priority / assignee / due_date /
 * project_id / etc). Optimistic merge into the detail cache; settle invalidates
 * the my-tasks list so a status change re-buckets the SectionList in
 * (tabs)/my-tasks.tsx automatically.
 *
 * Mobile cache is flat `Task[]` (not bucketed `byStatus`), so we DON'T mirror
 * web's `patchTaskInBuckets` rebalancing — settling via `invalidate` is
 * cheaper and produces the same end state.
 */
export function useUpdateTask(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["updateTask", taskId] as const,
    mutationFn: (patch: UpdateTaskRequest) => api.updateTask(taskId, patch),
    onMutate: async (patch) => {
      const key = taskKeys.detail(wsId, taskId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Task>(key);
      if (prev) {
        qc.setQueryData<Task>(key, { ...prev, ...patch });
      }
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (server) => {
      qc.setQueryData<Task>(taskKeys.detail(wsId, taskId), server);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.detail(wsId, taskId) });
      qc.invalidateQueries({ queryKey: taskKeys.myAll(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.list(wsId) });
    },
  });
}

/**
 * Attach a label to the task. Caller already has the full Label object
 * from the picker, so we don't need to fetch it. Optimistic append to
 * `task.labels[]`; on success, replace with server-returned full array
 * (handles ordering / dup safety).
 */
export function useAttachLabel(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["attachLabel", taskId] as const,
    mutationFn: ({ label }: { label: Label }) =>
      api.attachLabel(taskId, label.id),
    onMutate: async ({ label }) => {
      const key = taskKeys.detail(wsId, taskId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Task>(key);
      if (prev) {
        const existing = prev.labels ?? [];
        // Skip dup append — the optimistic case must be idempotent because
        // the picker can fire twice on rapid taps before the request lands.
        if (!existing.some((l) => l.id === label.id)) {
          qc.setQueryData<Task>(key, {
            ...prev,
            labels: [...existing, label],
          });
        }
      }
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (server) => {
      const key = taskKeys.detail(wsId, taskId);
      const current = qc.getQueryData<Task>(key);
      if (current) {
        qc.setQueryData<Task>(key, { ...current, labels: server.labels });
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.detail(wsId, taskId) });
    },
  });
}

/** Detach a label. Mirror of useAttachLabel — same invalidation surface. */
export function useDetachLabel(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationKey: ["detachLabel", taskId] as const,
    mutationFn: ({ labelId }: { labelId: string }) =>
      api.detachLabel(taskId, labelId),
    onMutate: async ({ labelId }) => {
      const key = taskKeys.detail(wsId, taskId);
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<Task>(key);
      if (prev) {
        const existing = prev.labels ?? [];
        qc.setQueryData<Task>(key, {
          ...prev,
          labels: existing.filter((l) => l.id !== labelId),
        });
      }
      return { prev, key };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prev !== undefined && ctx.key) {
        qc.setQueryData(ctx.key, ctx.prev);
      }
    },
    onSuccess: (server) => {
      const key = taskKeys.detail(wsId, taskId);
      const current = qc.getQueryData<Task>(key);
      if (current) {
        qc.setQueryData<Task>(key, { ...current, labels: server.labels });
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: taskKeys.detail(wsId, taskId) });
    },
  });
}

/**
 * Task creation mutation. No optimistic insert — the my-tasks list is
 * status-bucketed + scope-filtered (assigned/created/agents), so optimism
 * needs to decide which bucket + scope the row lands in, with rollback.
 * Invalidation is simpler and the hosted server returns in <300ms.
 *
 * Invalidates:
 *  - taskKeys.myAll(wsId)        my-tasks list (all three scopes)
 *  - inboxKeys.all(wsId)          inbox (assignment notification if any) —
 *                                 prefix-matches the inbox list key
 */
export function useCreateTask() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (body: CreateTaskRequest) => api.createTask(body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: taskKeys.myAll(wsId) });
      qc.invalidateQueries({ queryKey: inboxKeys.all(wsId) });
    },
  });
}

/**
 * Cancel an in-flight agent task. Optimistically removes the task from the
 * active-runs cache so the RunRow disappears immediately; the WS
 * `run:cancelled` event then invalidates both task queries (see
 * `use-task-realtime.ts`) so the task reappears in the past list with the
 * authoritative server state. On error we restore the snapshot.
 */
/**
 * Delete a task. Mirrors `useDeleteProject` (mutations/projects.ts:103-128)
 * but the cache surface is wider:
 *   - taskKeys.list(wsId)             — workspace-wide flat list
 *   - taskKeys.myList(wsId, ...)      — three scopes × N filter combos
 *
 * Both are flat `Task[]` caches. We use `setQueriesData` with the
 * `myAll(wsId)` prefix to filter the task out of every `myList` key in one
 * pass, snapshot the previous data for rollback, and remove the detail /
 * timeline / tasks caches on settle so a stale return-trip can't surface
 * 404-y data.
 *
 * The WS `task:deleted` event is already handled in `use-task-realtime.ts`
 * (callers like the detail screen pass `() => router.back()`), so the other
 * tab/client case is covered.
 */
export function useDeleteTask() {
  const qc = useQueryClient();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  return useMutation({
    mutationFn: (id: string) => api.deleteTask(id),
    onMutate: async (id) => {
      const listKey = taskKeys.list(wsId);
      const myAllKey = taskKeys.myAll(wsId);
      await Promise.all([
        qc.cancelQueries({ queryKey: listKey }),
        qc.cancelQueries({ queryKey: myAllKey }),
      ]);

      // Snapshot every matching cache (flat list + each my-tasks scope×filter)
      // so we can roll back per-key on error.
      const prevList = qc.getQueryData<Task[]>(listKey);
      const prevMy = qc.getQueriesData<Task[]>({ queryKey: myAllKey });

      qc.setQueryData<Task[]>(listKey, (old) =>
        old ? old.filter((i) => i.id !== id) : old,
      );
      qc.setQueriesData<Task[]>({ queryKey: myAllKey }, (old) =>
        old ? old.filter((i) => i.id !== id) : old,
      );

      return { prevList, prevMy, listKey, myAllKey };
    },
    onError: (_err, _id, ctx) => {
      if (!ctx) return;
      if (ctx.prevList !== undefined) {
        qc.setQueryData(ctx.listKey, ctx.prevList);
      }
      for (const [key, value] of ctx.prevMy) {
        qc.setQueryData(key, value);
      }
    },
    onSettled: (_data, _err, id) => {
      qc.invalidateQueries({ queryKey: taskKeys.list(wsId) });
      qc.invalidateQueries({ queryKey: taskKeys.myAll(wsId) });
      qc.removeQueries({ queryKey: taskKeys.detail(wsId, id) });
      qc.removeQueries({ queryKey: taskKeys.timeline(wsId, id) });
      qc.removeQueries({ queryKey: agentRunsKeys.activeForTask(wsId, id) });
      qc.removeQueries({ queryKey: agentRunsKeys.historyForTask(wsId, id) });
    },
  });
}
