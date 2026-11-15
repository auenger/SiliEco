import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { propertyKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import { taskKeys } from "../tasks/queries";
import {
  invalidatePropertyWindowQueries,
  onTaskPropertiesChanged,
  patchTaskProperties,
} from "../tasks/ws-updaters";
import { findTaskLocation } from "../tasks/cache-helpers";
import type { TaskFlatCache } from "../tasks/cache-coordinator";
import type {
  CreatePropertyRequest,
  UpdatePropertyRequest,
  Task,
  TaskPropertyValue,
  TaskPropertyValues,
  ListTasksCache,
} from "../types";

export function useCreateProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreatePropertyRequest) => api.createProperty(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
    },
  });
}

/**
 * Definition updates (rename, options, archive). No optimistic patch: edits
 * happen in the settings dialog where a round-trip is acceptable, and config
 * canonicalization (option id assignment) is server-side anyway.
 */
export function useUpdateProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdatePropertyRequest) =>
      api.updateProperty(id, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
      // Task caches embed the value bag; a definition change (rename,
      // option edits) changes how values render, and rows referencing an
      // archived definition must drop out of "+ Add property" menus.
      qc.invalidateQueries({ queryKey: taskKeys.all(wsId) });
    },
  });
}

/**
 * Read the task's current property bag from whichever cache holds it: the
 * detail cache first, then any list cache bucket. Board/list surfaces never
 * populate the detail cache, so stopping there would make the optimistic
 * merge below overwrite the whole bag with a single key.
 */
function readTaskProperties(qc: ReturnType<typeof useQueryClient>, wsId: string, taskId: string): TaskPropertyValues | undefined {
  const detail = qc.getQueryData<Task>(taskKeys.detail(wsId, taskId));
  if (detail) return detail.properties ?? {};
  for (const [, data] of qc.getQueriesData<ListTasksCache>({ queryKey: taskKeys.list(wsId) })) {
    if (!data) continue;
    const location = findTaskLocation(data, taskId);
    if (location) return location.task.properties ?? {};
  }
  for (const [, data] of qc.getQueriesData<TaskFlatCache>({
    queryKey: taskKeys.flatAll(wsId),
  })) {
    for (const page of data?.pages ?? []) {
      const task = page.tasks.find((candidate) => candidate.id === taskId);
      if (task) return task.properties ?? {};
    }
  }
  for (const [, data] of qc.getQueriesData<Task[]>({
    queryKey: taskKeys.childrenAll(wsId),
  })) {
    const task = data?.find((candidate) => candidate.id === taskId);
    if (task) return task.properties ?? {};
  }
  return undefined;
}

async function cancelTaskPropertyMutationQueries(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  taskId: string,
) {
  await Promise.all([
    qc.cancelQueries({ queryKey: taskKeys.detail(wsId, taskId) }),
    qc.cancelQueries({ queryKey: taskKeys.list(wsId) }),
    qc.cancelQueries({ queryKey: taskKeys.flatAll(wsId) }),
    qc.cancelQueries({ queryKey: taskKeys.childrenAll(wsId) }),
    qc.cancelQueries({ queryKey: taskKeys.childrenByParentsAll(wsId) }),
  ]);
}

/**
 * Optimistic single-property write on an task.
 *
 * Concurrency contract (SILI-4463 review round 1/2):
 *   - Mutations for the SAME task serialize via TanStack's mutation
 *     `scope`, so full-bag responses cannot land out of order and rapid
 *     multi-select toggles cannot interleave.
 *   - The optimistic patch merges into the bag read from ANY cache
 *     (detail or list) — never replaces it — so board-only surfaces keep
 *     the task's other property values.
 *   - Failure restores the snapshot; if no snapshot existed, it falls back
 *     to invalidation.
 *   - The LAST settled mutation for the task does an authoritative
 *     invalidate of the detail cache plus the definition catalog (usage
 *     counts), reconciling any raced WS snapshots under staleTime:Infinity.
 */
export function useSetTaskProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ taskId, propertyId, value }: { taskId: string; propertyId: string; value: TaskPropertyValue }) =>
      api.setTaskProperty(taskId, propertyId, value),
    scope: { id: `task-properties:${wsId}` },
    mutationKey: ["task-properties", wsId],
    onMutate: async ({ taskId, propertyId, value }) => {
      // A response snapshotted before this write must not land after the
      // optimistic patch and revert any denormalized task projection.
      await cancelTaskPropertyMutationQueries(qc, wsId, taskId);
      const prev = readTaskProperties(qc, wsId, taskId);
      patchTaskProperties(qc, wsId, taskId, { ...(prev ?? {}), [propertyId]: value });
      return { prevValue: prev?.[propertyId], hadBag: prev !== undefined, taskId, propertyId };
    },
    onError: (_err, _vars, ctx) => {
      if (!ctx) return;
      rollbackSingleKey(qc, wsId, ctx);
    },
    onSuccess: (data, { taskId }) => {
      onTaskPropertiesChanged(qc, wsId, taskId, data.properties ?? {});
    },
    onSettled: (_data, _err, { taskId }) => {
      settleTaskPropertyCaches(qc, wsId, taskId);
    },
  });
}

export function useUnsetTaskProperty() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ taskId, propertyId }: { taskId: string; propertyId: string }) =>
      api.unsetTaskProperty(taskId, propertyId),
    scope: { id: `task-properties:${wsId}` },
    mutationKey: ["task-properties", wsId],
    onMutate: async ({ taskId, propertyId }) => {
      await cancelTaskPropertyMutationQueries(qc, wsId, taskId);
      const prev = readTaskProperties(qc, wsId, taskId);
      if (prev) {
        const next = { ...prev };
        delete next[propertyId];
        patchTaskProperties(qc, wsId, taskId, next);
      }
      return { prevValue: prev?.[propertyId], hadBag: prev !== undefined, taskId, propertyId };
    },
    onError: (_err, _vars, ctx) => {
      if (!ctx) return;
      rollbackSingleKey(qc, wsId, ctx);
    },
    onSuccess: (data, { taskId }) => {
      onTaskPropertiesChanged(qc, wsId, taskId, data.properties ?? {});
    },
    onSettled: (_data, _err, { taskId }) => {
      settleTaskPropertyCaches(qc, wsId, taskId);
    },
  });
}

/**
 * Roll back exactly the key this mutation touched, against the CURRENT bag.
 * Restoring the whole onMutate snapshot would erase concurrent remote writes
 * to other keys that landed via WS while this request was in flight
 * (clean-room review F2 interleave B).
 */
function rollbackSingleKey(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  ctx: { prevValue: TaskPropertyValue | undefined; hadBag: boolean; taskId: string; propertyId: string },
) {
  if (!ctx.hadBag) {
    qc.invalidateQueries({ queryKey: taskKeys.detail(wsId, ctx.taskId) });
    return;
  }
  const current = readTaskProperties(qc, wsId, ctx.taskId) ?? {};
  const next = { ...current };
  if (ctx.prevValue === undefined) delete next[ctx.propertyId];
  else next[ctx.propertyId] = ctx.prevValue;
  patchTaskProperties(qc, wsId, ctx.taskId, next);
}

/** Authoritative reconcile once the LAST in-flight property write settles. */
function settleTaskPropertyCaches(qc: ReturnType<typeof useQueryClient>, wsId: string, taskId: string) {
  if (qc.isMutating({ mutationKey: ["task-properties", wsId] }) > 1) return;
  qc.invalidateQueries({ queryKey: taskKeys.detail(wsId, taskId) });
  qc.invalidateQueries({ queryKey: propertyKeys.all(wsId) });
  // Server-filtered/-sorted windows must refetch: the write may have moved
  // this task across pages or reordered it (onTaskPropertiesChanged
  // already fires this for the WS path; local mutations need it on settle
  // because the self-emitted WS event may be suppressed or raced).
  invalidatePropertyWindowQueries(qc, wsId);
}
