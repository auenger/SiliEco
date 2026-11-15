/**
 * Mobile-owned WS cache patchers for the task domain. These are pure
 * functions over `QueryClient` — no React, no WS plumbing. The hooks in
 * `use-task-realtime.ts` and `use-my-tasks-realtime.ts` translate WS
 * events into calls into this module.
 *
 * Why mobile-owned (and not importing from packages/core/tasks/ws-updaters):
 *   - Web's updaters reference `taskKeys` from `packages/core/tasks/queries`,
 *     a different runtime instance than mobile's `data/queries/task-keys.ts`.
 *     TanStack Query keys are compared structurally so this would *appear*
 *     to work, but binding cache mutation to a foreign key factory invites
 *     drift the moment either side adjusts its key shape.
 *   - Mobile cache shapes are simpler: no status-bucketed list, no children
 *     subtree, no childProgress, no label-byTask cache. A direct mirror
 *     would be ~120 lines of conditional dead-code paths.
 *
 * Cache shapes (the design contract here):
 *   - Task detail:    Task                                  (keyed by detail(wsId, id))
 *   - Task timeline:  TimelineEntry[]                        (keyed by timeline(wsId, id))
 *                      ASC oldest-first; new entries inserted at sorted position.
 *   - My Tasks list:  Task[]                                (keyed by myList(wsId, scope, filter))
 *                      Multiple list caches per wsId (one per scope/filter combo).
 *                      Patch ALL of them via setQueriesData on myAll(wsId).
 *   - Workspace list:  Task[]                                (keyed by list(wsId))
 *                      Single cache per wsId (no scope/filter in the key —
 *                      filtering happens client-side off the same list).
 */
import type { QueryClient } from "@tanstack/react-query";
import type {
  Comment,
  Task,
  TaskReaction,
  Label,
  Reaction,
  TimelineEntry,
} from "@silieco/core/types";
import { taskKeys } from "@/data/queries/task-keys";

type TimelinePredicate = (entry: TimelineEntry) => boolean;
type TimelineMutate = (entry: TimelineEntry) => TimelineEntry;

// =====================================================
// Task detail cache (single Task per id)
// =====================================================

export function patchTaskDetail(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Task> & { id: string },
) {
  qc.setQueryData<Task>(taskKeys.detail(wsId, partial.id), (old) =>
    old ? { ...old, ...partial } : old,
  );
}

export function clearTaskDetail(
  qc: QueryClient,
  wsId: string,
  taskId: string,
) {
  qc.removeQueries({ queryKey: taskKeys.detail(wsId, taskId) });
  qc.removeQueries({ queryKey: taskKeys.timeline(wsId, taskId) });
}

// =====================================================
// Task timeline (flat TimelineEntry[], ASC oldest-first)
// =====================================================

export function appendTimelineEntry(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  entry: TimelineEntry,
) {
  qc.setQueryData<TimelineEntry[]>(
    taskKeys.timeline(wsId, taskId),
    (old) => {
      if (!old) return old;
      // Skip if the entry is already present — backend can re-emit on
      // reconnect or two clients can echo the same comment.
      if (old.some((e) => e.id === entry.id && e.type === entry.type)) {
        return old;
      }
      const next = [...old, entry];
      next.sort((a, b) => {
        if (a.created_at !== b.created_at) return a.created_at < b.created_at ? -1 : 1;
        return a.id < b.id ? -1 : 1;
      });
      return next;
    },
  );
}

export function patchTimelineEntry(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  predicate: TimelinePredicate,
  mutate: TimelineMutate,
) {
  qc.setQueryData<TimelineEntry[]>(
    taskKeys.timeline(wsId, taskId),
    (old) => (old ? old.map((e) => (predicate(e) ? mutate(e) : e)) : old),
  );
}

export function removeTimelineEntry(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  predicate: TimelinePredicate,
) {
  qc.setQueryData<TimelineEntry[]>(
    taskKeys.timeline(wsId, taskId),
    (old) => (old ? old.filter((e) => !predicate(e)) : old),
  );
}

/**
 * Cascade-delete a comment and every descendant reply (reply-to-reply
 * chains included). Mirrors the server's cascade in `comment.go:DeleteComment`
 * and web's `comment:deleted` handler at
 * `packages/views/tasks/hooks/use-task-timeline.ts:164-194`.
 *
 * Without this, removing only the root entry leaves the replies as
 * "orphans" — `buildTimelineRows` then promotes them to top-level rows
 * (its orphan-rescue branch), so the user would see ghost replies after a
 * thread delete on another client. Same-N rule violation.
 *
 * BFS rather than recursive Set-union: cheaper for arbitrary depth chains
 * and avoids recursion-depth concerns on large threads.
 */
export function removeCommentCascade(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  commentId: string,
) {
  qc.setQueryData<TimelineEntry[]>(
    taskKeys.timeline(wsId, taskId),
    (old) => {
      if (!old) return old;
      const removed = new Set<string>([commentId]);
      // Iterate to fixed point — a single forward pass catches direct
      // children; later passes catch reply-to-reply chains. Bounded by
      // the timeline length, so worst case O(N²) on a degenerate chain
      // but N is p99 30 and chains are typically depth 1-2.
      let changed = true;
      while (changed) {
        changed = false;
        for (const e of old) {
          if (
            e.type === "comment" &&
            e.parent_id &&
            removed.has(e.parent_id) &&
            !removed.has(e.id)
          ) {
            removed.add(e.id);
            changed = true;
          }
        }
      }
      return old.filter(
        (e) => !(e.type === "comment" && removed.has(e.id)),
      );
    },
  );
}

// =====================================================
// My Tasks list (flat Task[] across many keys)
// =====================================================

export function patchMyTasksList(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Task> & { id: string },
) {
  // myList is keyed by (wsId, scope, filter); we don't know which entries
  // the task belongs to, so update every cached one. Any not-yet-loaded
  // list will fetch fresh on mount.
  qc.setQueriesData<Task[]>({ queryKey: taskKeys.myAll(wsId) }, (old) =>
    old ? old.map((i) => (i.id === partial.id ? { ...i, ...partial } : i)) : old,
  );
}

export function removeFromMyTasksList(
  qc: QueryClient,
  wsId: string,
  taskId: string,
) {
  qc.setQueriesData<Task[]>({ queryKey: taskKeys.myAll(wsId) }, (old) =>
    old ? old.filter((i) => i.id !== taskId) : old,
  );
}

// =====================================================
// Workspace Tasks list (flat Task[] under list(wsId))
// =====================================================

export function patchTasksList(
  qc: QueryClient,
  wsId: string,
  partial: Partial<Task> & { id: string },
) {
  qc.setQueryData<Task[]>(taskKeys.list(wsId), (old) =>
    old ? old.map((i) => (i.id === partial.id ? { ...i, ...partial } : i)) : old,
  );
}

export function prependToTasksList(
  qc: QueryClient,
  wsId: string,
  task: Task,
) {
  qc.setQueryData<Task[]>(taskKeys.list(wsId), (old) => {
    if (!old) return old;
    if (old.some((i) => i.id === task.id)) return old;
    return [task, ...old];
  });
}

export function removeFromTasksList(
  qc: QueryClient,
  wsId: string,
  taskId: string,
) {
  qc.setQueryData<Task[]>(taskKeys.list(wsId), (old) =>
    old ? old.filter((i) => i.id !== taskId) : old,
  );
}

// =====================================================
// Reactions
// =====================================================

export function addCommentReaction(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  commentId: string,
  reaction: Reaction,
) {
  patchTimelineEntry(
    qc,
    wsId,
    taskId,
    (e) => e.type === "comment" && e.id === commentId,
    (e) => ({
      ...e,
      reactions: [
        ...(e.reactions ?? []).filter((r) => r.id !== reaction.id),
        reaction,
      ],
    }),
  );
}

export function removeCommentReaction(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  commentId: string,
  emoji: string,
  actorId: string,
) {
  patchTimelineEntry(
    qc,
    wsId,
    taskId,
    (e) => e.type === "comment" && e.id === commentId,
    (e) => ({
      ...e,
      reactions: (e.reactions ?? []).filter(
        (r) => !(r.emoji === emoji && r.actor_id === actorId),
      ),
    }),
  );
}

export function addTaskReaction(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  reaction: TaskReaction,
) {
  qc.setQueryData<Task>(taskKeys.detail(wsId, taskId), (old) => {
    if (!old) return old;
    const existing = old.reactions ?? [];
    if (existing.some((r) => r.id === reaction.id)) return old;
    return { ...old, reactions: [...existing, reaction] };
  });
}

export function removeTaskReaction(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  emoji: string,
  actorId: string,
) {
  qc.setQueryData<Task>(taskKeys.detail(wsId, taskId), (old) =>
    old
      ? {
          ...old,
          reactions: (old.reactions ?? []).filter(
            (r) => !(r.emoji === emoji && r.actor_id === actorId),
          ),
        }
      : old,
  );
}

// =====================================================
// Labels
// =====================================================

export function patchTaskLabels(
  qc: QueryClient,
  wsId: string,
  taskId: string,
  labels: Label[],
) {
  qc.setQueryData<Task>(taskKeys.detail(wsId, taskId), (old) =>
    old ? { ...old, labels } : old,
  );
  qc.setQueriesData<Task[]>({ queryKey: taskKeys.myAll(wsId) }, (old) =>
    old
      ? old.map((i) => (i.id === taskId ? { ...i, labels } : i))
      : old,
  );
  qc.setQueryData<Task[]>(taskKeys.list(wsId), (old) =>
    old
      ? old.map((i) => (i.id === taskId ? { ...i, labels } : i))
      : old,
  );
}

// =====================================================
// Helpers — payload normalization
// =====================================================

/**
 * Convert a Comment WS payload into a TimelineEntry. The two types share
 * most fields but use different actor-key names (Comment uses
 * `author_type/author_id`; TimelineEntry uses `actor_type/actor_id`).
 */
export function commentToTimelineEntry(comment: Comment): TimelineEntry {
  return {
    type: "comment",
    id: comment.id,
    actor_type: comment.author_type,
    actor_id: comment.author_id,
    created_at: comment.created_at,
    content: comment.content,
    parent_id: comment.parent_id,
    updated_at: comment.updated_at,
    comment_type: comment.type,
    reactions: comment.reactions,
    attachments: comment.attachments,
    // Carry resolve state through. Web's commentToTimelineEntry includes
    // these (`packages/views/tasks/hooks/use-task-timeline.ts:42-58`); the
    // earlier mobile copy dropped them, so a `comment:updated` event on a
    // resolved comment would silently strip the resolved flag from the
    // cache and the UI would un-dim.
    resolved_at: comment.resolved_at,
    resolved_by_type: comment.resolved_by_type,
    resolved_by_id: comment.resolved_by_id,
    source_run_id: comment.source_run_id,
  };
}
