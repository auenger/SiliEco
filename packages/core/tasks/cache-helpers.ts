import type {
  Task,
  TaskStatus,
  TaskStatusBucket,
  ListTasksCache,
} from "../types";
import { PAGINATED_STATUSES } from "./queries";

const EMPTY_BUCKET: TaskStatusBucket = { tasks: [], total: 0 };

export function getBucket(
  resp: ListTasksCache,
  status: TaskStatus,
): TaskStatusBucket {
  return resp.byStatus[status] ?? EMPTY_BUCKET;
}

export function setBucket(
  resp: ListTasksCache,
  status: TaskStatus,
  bucket: TaskStatusBucket,
): ListTasksCache {
  return { ...resp, byStatus: { ...resp.byStatus, [status]: bucket } };
}

/** Locate which status bucket holds `id`, if any. */
export function findTaskLocation(
  resp: ListTasksCache,
  id: string,
): { status: TaskStatus; task: Task } | null {
  for (const status of PAGINATED_STATUSES) {
    const bucket = resp.byStatus[status];
    const found = bucket?.tasks.find((i) => i.id === id);
    if (found) return { status, task: found };
  }
  return null;
}

/** Add an task to its status bucket (no-op if already present). */
export function addTaskToBuckets(
  resp: ListTasksCache,
  task: Task,
): ListTasksCache {
  const bucket = getBucket(resp, task.status);
  if (bucket.tasks.some((i) => i.id === task.id)) return resp;
  return setBucket(resp, task.status, {
    tasks: [...bucket.tasks, task],
    total: bucket.total + 1,
  });
}

/** Remove an task from whichever bucket contains it. */
export function removeTaskFromBuckets(
  resp: ListTasksCache,
  id: string,
): ListTasksCache {
  const loc = findTaskLocation(resp, id);
  if (!loc) return resp;
  const bucket = getBucket(resp, loc.status);
  return setBucket(resp, loc.status, {
    tasks: bucket.tasks.filter((i) => i.id !== id),
    total: Math.max(0, bucket.total - 1),
  });
}

/**
 * Count-only reconcile for an task BEYOND the loaded window: its status
 * changed server-side, so one unit of `total` moves between buckets while
 * the loaded arrays stay untouched. `hasMore` (loaded < total) stays
 * consistent for free.
 */
export function moveBucketTotal(
  resp: ListTasksCache,
  from: TaskStatus,
  to: TaskStatus,
): ListTasksCache {
  if (from === to) return resp;
  const fromBucket = getBucket(resp, from);
  const toBucket = getBucket(resp, to);
  let next = setBucket(resp, from, {
    ...fromBucket,
    total: Math.max(0, fromBucket.total - 1),
  });
  next = setBucket(next, to, { ...toBucket, total: toBucket.total + 1 });
  return next;
}

/**
 * Count-only reconcile for an task BEYOND the loaded window that LEFT the
 * list (reassigned / moved project): the bucket it was counted in loses one.
 */
export function decrementBucketTotal(
  resp: ListTasksCache,
  status: TaskStatus,
): ListTasksCache {
  const bucket = getBucket(resp, status);
  return setBucket(resp, status, {
    ...bucket,
    total: Math.max(0, bucket.total - 1),
  });
}

/**
 * Insert `task` into `tasks` at the slot implied by `position ASC` — the same
 * ordering the board renders (server `ORDER BY position ASC`). Returns a new
 * array; the input is not mutated.
 *
 * Inserting at the right slot (instead of appending to the end) is what keeps an
 * optimistic move from snapping: the card lands where it will be after the
 * server confirms, so no later cache refresh teleports it to the column tail.
 */
export function insertByPosition(tasks: Task[], task: Task): Task[] {
  const idx = tasks.findIndex((i) => i.position > task.position);
  if (idx === -1) return [...tasks, task];
  return [...tasks.slice(0, idx), task, ...tasks.slice(idx)];
}

/**
 * Merge `patch` into the task with `id`. If `patch.status` differs from the
 * current bucket, the task moves to the new bucket and both buckets' totals
 * are adjusted. The moved card — and a same-column card whose `position`
 * changed — is re-inserted at its `position`-sorted slot rather than appended,
 * so the cache order stays consistent with what the board renders. A plain
 * field update (no status/position change) keeps the card in place.
 */
export function patchTaskInBuckets(
  resp: ListTasksCache,
  id: string,
  patch: Partial<Task>,
): ListTasksCache {
  const loc = findTaskLocation(resp, id);
  if (!loc) return resp;
  const merged: Task = { ...loc.task, ...patch };
  const nextStatus = patch.status ?? loc.status;

  if (nextStatus === loc.status) {
    const bucket = getBucket(resp, loc.status);
    const positionChanged =
      patch.position !== undefined && patch.position !== loc.task.position;
    if (!positionChanged) {
      // Plain field update (labels, metadata, title, …): keep the slot so a
      // remote edit never reorders an otherwise-untouched column.
      return setBucket(resp, loc.status, {
        ...bucket,
        tasks: bucket.tasks.map((i) => (i.id === id ? merged : i)),
      });
    }
    // Same-column reorder: lift the card out and re-insert at its new slot.
    return setBucket(resp, loc.status, {
      ...bucket,
      tasks: insertByPosition(
        bucket.tasks.filter((i) => i.id !== id),
        merged,
      ),
    });
  }

  const fromBucket = getBucket(resp, loc.status);
  const toBucket = getBucket(resp, nextStatus);
  let next = setBucket(resp, loc.status, {
    tasks: fromBucket.tasks.filter((i) => i.id !== id),
    total: Math.max(0, fromBucket.total - 1),
  });
  next = setBucket(next, nextStatus, {
    tasks: insertByPosition(toBucket.tasks, merged),
    total: toBucket.total + 1,
  });
  return next;
}
