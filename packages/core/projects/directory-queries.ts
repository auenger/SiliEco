import {
  infiniteQueryOptions,
  queryOptions,
  useMutation,
} from "@tanstack/react-query";
import { api } from "../api";
export const projectDirectoryKey = (wsId: string, projectId: string) =>
  ["project-directory", wsId, projectId] as const;
export function projectDirectoryOptions(
  wsId: string,
  projectId: string,
  resourceId: string,
  path: string,
) {
  return infiniteQueryOptions({
    queryKey: [
      ...projectDirectoryKey(wsId, projectId),
      resourceId,
      "list",
      path,
    ],
    initialPageParam: 0,
    queryFn: ({ pageParam, signal }) =>
      api.browseProjectDirectory(
        projectId,
        resourceId,
        path,
        "list",
        pageParam,
        signal,
      ),
    getNextPageParam: (page) =>
      page.next_offset > 0 ? page.next_offset : undefined,
    staleTime: 30_000,
    gcTime: 60_000,
    retry: false,
    refetchOnWindowFocus: false,
  });
}
export function projectPreviewOptions(
  wsId: string,
  projectId: string,
  resourceId: string,
  path: string,
) {
  return queryOptions({
    queryKey: [
      ...projectDirectoryKey(wsId, projectId),
      resourceId,
      "preview",
      path,
    ],
    queryFn: async ({ signal }) => {
      const decoder = new TextDecoder();
      const parts: string[] = [];
      let offset = 0;
      let mime = "";
      do {
        const page = await api.browseProjectDirectory(
          projectId,
          resourceId,
          path,
          "preview",
          offset,
          signal,
        );
        mime = page.mime;
        const binary = atob(page.content);
        if (mime.startsWith("image/")) parts.push(binary);
        else
          parts.push(
            decoder.decode(
              Uint8Array.from(binary, (c) => c.charCodeAt(0)),
              { stream: page.next_offset > 0 },
            ),
          );
        if (page.next_offset !== 0 && page.next_offset <= offset)
          throw new Error("Invalid preview continuation");
        offset = page.next_offset;
      } while (offset > 0);
      const content = parts.join("");
      return {
        mime,
        content: mime.startsWith("image/") ? btoa(content) : content,
      };
    },
    staleTime: 15_000,
    gcTime: 30_000,
    retry: false,
    refetchOnWindowFocus: false,
  });
}

export function useCheckProjectDirectory(projectId: string) {
  return useMutation({
    mutationFn: (input: { daemonId: string; path: string }) =>
      api.checkProjectDirectory(projectId, input.daemonId, input.path),
  });
}
