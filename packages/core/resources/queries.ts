import { useMemo } from "react";
import { infiniteQueryOptions, useInfiniteQuery, queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { issueKeys } from "../issues/queries";

export const fileResourceKeys = {
  all: (wsId: string) => ["file-resources", wsId] as const,
  workspace: (wsId: string) => [...fileResourceKeys.all(wsId), "workspace"] as const,
  project: (wsId: string, projectId: string) =>
    [...fileResourceKeys.all(wsId), "project", projectId] as const,
  references: (wsId: string, attachmentId: string) =>
    [...fileResourceKeys.all(wsId), "references", attachmentId] as const,
  workspaceReferences: (wsId: string) =>
    [...fileResourceKeys.all(wsId), "workspace-references"] as const,
  targetReferences: (wsId: string, targetType: "issue" | "project", targetId: string) =>
    [...fileResourceKeys.all(wsId), "target-references", targetType, targetId] as const,
};

export function workspaceFileResourcesOptions(wsId: string) {
  return queryOptions({
    queryKey: fileResourceKeys.workspace(wsId),
    queryFn: () => api.listFileResources({ limit: 100 }),
  });
}

export function projectFileResourcesOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: fileResourceKeys.project(wsId, projectId),
    queryFn: () => api.listProjectFiles(projectId, { limit: 100 }),
  });
}

const FILE_PAGE_SIZE = 50;

export function fileResourcesInfiniteOptions(wsId: string, projectId?: string) {
  return infiniteQueryOptions({
    queryKey: [...(projectId
      ? fileResourceKeys.project(wsId, projectId)
      : fileResourceKeys.workspace(wsId)), "infinite"],
    initialPageParam: 0,
    queryFn: ({ pageParam }) => projectId
      ? api.listProjectFiles(projectId, { limit: FILE_PAGE_SIZE, offset: pageParam })
      : api.listFileResources({ limit: FILE_PAGE_SIZE, offset: pageParam }),
    getNextPageParam: (lastPage, _pages, offset) => {
      const next = offset + lastPage.files.length;
      return lastPage.files.length > 0 && next < lastPage.total ? next : undefined;
    },
  });
}

export function useFileResources(wsId: string, projectId?: string, enabled = true) {
  const query = useInfiniteQuery({ ...fileResourcesInfiniteOptions(wsId, projectId), enabled });
  const files = useMemo(() => [...new Map(
    (query.data?.pages.flatMap((page) => page.files) ?? []).map((file) => [file.id, file]),
  ).values()], [query.data]);
  return { ...query, files, total: query.data?.pages[0]?.total };
}

export function attachmentReferencesOptions(wsId: string, attachmentId: string) {
  return queryOptions({
    queryKey: fileResourceKeys.references(wsId, attachmentId),
    queryFn: () => api.listAttachmentReferences(attachmentId),
  });
}

export function workspaceAttachmentReferencesOptions(wsId: string) {
  return queryOptions({
    queryKey: fileResourceKeys.workspaceReferences(wsId),
    queryFn: () => api.listWorkspaceAttachmentReferences(),
  });
}

export function targetAttachmentReferencesOptions(
  wsId: string,
  targetType: "issue" | "project",
  targetId: string,
) {
  return queryOptions({
    queryKey: fileResourceKeys.targetReferences(wsId, targetType, targetId),
    queryFn: () => api.listTargetAttachmentReferences(targetType, targetId),
  });
}

export function useCreateAttachmentReference(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      targetType: "issue" | "project";
      targetId: string;
      attachmentId: string;
    }) => api.createAttachmentReference(input.targetType, input.targetId, input.attachmentId),
    onSettled: (_data, _error, input) => {
      queryClient.invalidateQueries({ queryKey: fileResourceKeys.all(wsId) });
      queryClient.invalidateQueries({
        queryKey: fileResourceKeys.references(wsId, input.attachmentId),
      });
      queryClient.invalidateQueries({
        queryKey: fileResourceKeys.targetReferences(wsId, input.targetType, input.targetId),
      });
      if (input.targetType === "issue") {
        queryClient.invalidateQueries({ queryKey: issueKeys.attachments(input.targetId) });
      }
    },
  });
}

export function useDeleteAttachmentReference(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      targetType: "issue" | "project";
      targetId: string;
      attachmentId: string;
    }) => api.deleteAttachmentReference(input.targetType, input.targetId, input.attachmentId),
    onSettled: (_data, _error, input) => {
      queryClient.invalidateQueries({ queryKey: fileResourceKeys.all(wsId) });
      queryClient.invalidateQueries({
        queryKey: fileResourceKeys.references(wsId, input.attachmentId),
      });
      queryClient.invalidateQueries({
        queryKey: fileResourceKeys.targetReferences(wsId, input.targetType, input.targetId),
      });
      if (input.targetType === "issue") {
        queryClient.invalidateQueries({ queryKey: issueKeys.attachments(input.targetId) });
      }
    },
  });
}
