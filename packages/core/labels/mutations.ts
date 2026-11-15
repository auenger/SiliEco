import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { labelKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import { taskKeys } from "../tasks/queries";
import {
  invalidateTaskLabelDerivatives,
  onTaskLabelsChanged,
  patchTaskLabels,
} from "../tasks/ws-updaters";
import type {
  Label,
  CreateLabelRequest,
  UpdateLabelRequest,
  ListLabelsResponse,
  TaskLabelsResponse,
  LabelResourceType,
  ResourceLabelsResponse,
} from "../types";

export function useCreateLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateLabelRequest) => api.createLabel(data),
    onSuccess: (label) => {
      qc.setQueryData<ListLabelsResponse>(labelKeys.list(wsId, label.resource_type ?? "task"), (old) =>
        old && !old.labels.some((l) => l.id === label.id)
          ? { ...old, labels: [...old.labels, label], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
    },
  });
}

/**
 * Optimistic rename/recolor. Matches the useUpdateProject pattern: apply the
 * change locally, snapshot for rollback, invalidate on settle. Without this
 * the UI freezes for the round-trip on every edit.
 */
export function useUpdateLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, resource_type: _resourceType, ...data }: { id: string; resource_type?: LabelResourceType } & UpdateLabelRequest) =>
      api.updateLabel(id, data),
    onMutate: async ({ id, resource_type = "task", ...data }) => {
      const listKey = labelKeys.list(wsId, resource_type);
      await qc.cancelQueries({ queryKey: listKey });
      const prevList = qc.getQueryData<ListLabelsResponse>(listKey);
      qc.setQueryData<ListLabelsResponse>(listKey, (old) =>
        old
          ? {
              ...old,
              labels: old.labels.map((l) => (l.id === id ? { ...l, ...data } : l)),
            }
          : old,
      );
      return { prevList, listKey };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList && ctx.listKey) qc.setQueryData(ctx.listKey, ctx.prevList);
    },
    onSettled: () => {
      // Invalidate the entire labels scope so any byTask cache holding a
      // stale copy of this label is refetched. The list cache is the source
      // of truth; byTask views will re-render with the fresh data.
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
      // Tasks now embed labels (denormalized snapshot), so a rename/recolor
      // also has to refresh the tasks caches that hold those snapshots.
      qc.invalidateQueries({ queryKey: taskKeys.all(wsId) });
    },
  });
}

export function useDeleteLabel() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (input: string | { id: string; resource_type: LabelResourceType }) =>
      api.deleteLabel(typeof input === "string" ? input : input.id),
    onMutate: async (input) => {
      const id = typeof input === "string" ? input : input.id;
      const resourceType = typeof input === "string" ? "task" : input.resource_type;
      const listKey = labelKeys.list(wsId, resourceType);
      await qc.cancelQueries({ queryKey: listKey });
      const prev = qc.getQueryData<ListLabelsResponse>(listKey);
      qc.setQueryData<ListLabelsResponse>(listKey, (old) =>
        old
          ? { ...old, labels: old.labels.filter((l) => l.id !== id), total: old.total - 1 }
          : old,
      );
      return { prev, listKey };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev && ctx.listKey) qc.setQueryData(ctx.listKey, ctx.prev);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
      // A deleted label still lives in cached task.labels arrays until we
      // refetch — invalidate so list/board chips drop the orphan.
      qc.invalidateQueries({ queryKey: taskKeys.all(wsId) });
    },
  });
}

export function useAttachResourceLabel(
  resourceType: "agent" | "skill",
  resourceId: string,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) =>
      api.attachLabelToResource(resourceType, resourceId, labelId),
    onSuccess: (data: ResourceLabelsResponse) => {
      qc.setQueryData(
        labelKeys.byResource(wsId, resourceType, resourceId),
        data,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeysForLabels(resourceType, wsId) });
    },
  });
}

export function useDetachResourceLabel(
  resourceType: "agent" | "skill",
  resourceId: string,
) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) =>
      api.detachLabelFromResource(resourceType, resourceId, labelId),
    onSuccess: (data: ResourceLabelsResponse) => {
      qc.setQueryData(
        labelKeys.byResource(wsId, resourceType, resourceId),
        data,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: workspaceKeysForLabels(resourceType, wsId) });
    },
  });
}

function workspaceKeysForLabels(resourceType: "agent" | "skill", wsId: string) {
  return ["workspaces", wsId, resourceType === "agent" ? "agents" : "skills"] as const;
}

async function cancelTaskLabelMutationQueries(
  qc: ReturnType<typeof useQueryClient>,
  wsId: string,
  taskId: string,
) {
  await Promise.all([
    qc.cancelQueries({ queryKey: labelKeys.byTask(wsId, taskId) }),
    qc.cancelQueries({ queryKey: taskKeys.list(wsId) }),
    qc.cancelQueries({ queryKey: taskKeys.flatAll(wsId) }),
    qc.cancelQueries({ queryKey: taskKeys.childrenAll(wsId) }),
    qc.cancelQueries({ queryKey: taskKeys.childrenByParentsAll(wsId) }),
  ]);
}

export function useAttachLabel(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) => api.attachLabel(taskId, labelId),
    onMutate: async (labelId) => {
      await cancelTaskLabelMutationQueries(qc, wsId, taskId);
      const prev = qc.getQueryData<TaskLabelsResponse>(labelKeys.byTask(wsId, taskId));
      // Only patch when we already know the current label set — otherwise
      // appending `[label]` to an empty array would wipe denormalized
      // labels in task list/detail caches and rollback couldn't restore
      // them. If byTask isn't cached yet (user clicked before the picker
      // fetched), skip the optimistic patch and rely on onSettled refetch.
      if (!prev) return { prev };
      if (prev.labels.some((l) => l.id === labelId)) return { prev };
      const list = qc.getQueryData<ListLabelsResponse>(labelKeys.list(wsId));
      const label = list?.labels.find((l) => l.id === labelId);
      if (!label) return { prev };
      const next: TaskLabelsResponse = { ...prev, labels: [...prev.labels, label] };
      qc.setQueryData<TaskLabelsResponse>(labelKeys.byTask(wsId, taskId), next);
      patchTaskLabels(qc, wsId, taskId, next.labels);
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(labelKeys.byTask(wsId, taskId), ctx.prev);
        patchTaskLabels(qc, wsId, taskId, ctx.prev.labels);
      }
    },
    onSuccess: (data: TaskLabelsResponse) => {
      // Backend may return an empty object when the post-mutation read fails
      // (it logs a warning and skips the broadcast). Only apply the list
      // when the backend gave us one — otherwise the optimistic patch from
      // onMutate stands until onSettled's invalidation refetches.
      if (data && Array.isArray(data.labels)) {
        qc.setQueryData<TaskLabelsResponse>(labelKeys.byTask(wsId, taskId), data);
        onTaskLabelsChanged(qc, wsId, taskId, data.labels);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.byTask(wsId, taskId) });
      invalidateTaskLabelDerivatives(qc, wsId);
    },
  });
}

/**
 * Attach a label to an task identified by mutation variables rather than a
 * closed-over `taskId`. `useAttachLabel` binds one taskId at hook-call time
 * for the optimistic task-detail flow; this variant defers the id to call
 * time so a caller can attach labels to an task that doesn't exist yet at
 * render — e.g. labels chosen in the create-task dialog, attached right after
 * the task is created. No optimistic patch: the create flow closes the dialog
 * and relies on the settle-time invalidation to surface the new labels.
 */
export function useAttachLabelToTask() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ taskId, labelId }: { taskId: string; labelId: string }) =>
      api.attachLabel(taskId, labelId),
    onSettled: (_data, _err, { taskId }) => {
      qc.invalidateQueries({ queryKey: labelKeys.byTask(wsId, taskId) });
      // Tasks embed a denormalized labels snapshot, so refresh the tasks
      // caches that hold it (list / board / detail) once the attach settles.
      qc.invalidateQueries({ queryKey: taskKeys.all(wsId) });
    },
  });
}

export function useDetachLabel(taskId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (labelId: string) => api.detachLabel(taskId, labelId),
    onMutate: async (labelId) => {
      await cancelTaskLabelMutationQueries(qc, wsId, taskId);
      const prev = qc.getQueryData<TaskLabelsResponse>(labelKeys.byTask(wsId, taskId));
      const next = prev
        ? { ...prev, labels: prev.labels.filter((l: Label) => l.id !== labelId) }
        : undefined;
      if (next) {
        qc.setQueryData<TaskLabelsResponse>(labelKeys.byTask(wsId, taskId), next);
        patchTaskLabels(qc, wsId, taskId, next.labels);
      }
      return { prev };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prev) {
        qc.setQueryData(labelKeys.byTask(wsId, taskId), ctx.prev);
        patchTaskLabels(qc, wsId, taskId, ctx.prev.labels);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: labelKeys.byTask(wsId, taskId) });
      invalidateTaskLabelDerivatives(qc, wsId);
    },
  });
}
