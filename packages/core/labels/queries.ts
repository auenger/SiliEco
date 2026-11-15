import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { LabelResourceType } from "../types";

export const labelKeys = {
  all: (wsId: string) => ["labels", wsId] as const,
  list: (wsId: string, resourceType: LabelResourceType = "task") =>
    [...labelKeys.all(wsId), "list", resourceType] as const,
  detail: (wsId: string, id: string) =>
    [...labelKeys.all(wsId), "detail", id] as const,
  byTask: (wsId: string, taskId: string) =>
    [...labelKeys.all(wsId), "task", taskId] as const,
  byResource: (wsId: string, resourceType: "agent" | "skill", resourceId: string) =>
    [...labelKeys.all(wsId), resourceType, resourceId] as const,
};

export function labelListOptions(wsId: string, resourceType: LabelResourceType = "task") {
  return queryOptions({
    queryKey: labelKeys.list(wsId, resourceType),
    queryFn: () => api.listLabels(resourceType),
    select: (data) => data.labels,
  });
}

export function resourceLabelsOptions(
  wsId: string,
  resourceType: "agent" | "skill",
  resourceId: string,
) {
  return queryOptions({
    queryKey: labelKeys.byResource(wsId, resourceType, resourceId),
    queryFn: () => api.listLabelsForResource(resourceType, resourceId),
    select: (data) => data.labels,
    enabled: Boolean(resourceId),
  });
}

export function taskLabelsOptions(wsId: string, taskId: string) {
  return queryOptions({
    queryKey: labelKeys.byTask(wsId, taskId),
    queryFn: () => api.listLabelsForTask(taskId),
    select: (data) => data.labels,
    enabled: Boolean(taskId),
  });
}
