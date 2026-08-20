"use client";

import { useQuery } from "@tanstack/react-query";
import { FolderOpen } from "lucide-react";
import { projectResourcesOptions } from "@silieco/core/projects";
import type {
  GithubRepoResourceRef,
  LocalDirectoryResourceRef,
  ProjectResource,
} from "@silieco/core/types";
import { useWorkspaceId } from "@silieco/core/hooks";
import { useLocalDaemonStatus } from "../../platform";
import { useT } from "../../i18n";

/**
 * Banner shown at the top of the issue's Activity section when the
 * project is pinned to a `local_directory` resource on **this** daemon.
 * Tells the user "starting an agent here will use {label} ({path}) in-
 * place" so they notice they are not getting an isolated git worktree.
 *
 * Rendered only on desktop: web has no daemon to compare against, so the
 * "this machine" check would always fail. Web users will see local_directory
 * resources read-only in the sidebar but no Activity-section hint.
 *
 * SSR-safe: the underlying hook reads `window.daemonAPI` defensively, so
 * server renders return null.
 */
export function LocalDirectoryHint({
  projectId,
}: {
  projectId: string | null | undefined;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const daemon = useLocalDaemonStatus();
  const { data: resources = [] } = useQuery({
    ...projectResourcesOptions(wsId, projectId ?? ""),
    enabled: Boolean(projectId),
  });

  if (!projectId) return null;
  if (!daemon.daemonId) return null;

  const repositories = resources.filter(
    (resource): resource is ProjectResource & { resource_ref: GithubRepoResourceRef } =>
      resource.resource_type === "github_repo",
  );
  const primaryRepositoryId =
    repositories.find((resource) => resource.resource_ref.primary)?.id ??
    repositories[0]?.id;
  const localDirectories = resources
    .filter(
      (r): r is ProjectResource & { resource_ref: LocalDirectoryResourceRef } =>
        r.resource_type === "local_directory" &&
        (r.resource_ref as LocalDirectoryResourceRef).daemon_id === daemon.daemonId,
    );
  const repositoryMatches = primaryRepositoryId
    ? localDirectories.filter(
        (resource) =>
          resource.resource_ref.repository_resource_id === primaryRepositoryId,
      )
    : [];
  const matches =
    repositoryMatches.length > 0
      ? repositoryMatches
      : localDirectories.filter(
          (resource) => !resource.resource_ref.repository_resource_id,
        );

  if (matches.length === 0) return null;

  return (
    <div className="mt-3 space-y-1 rounded-md border border-dashed bg-muted/40 px-3 py-2 text-caption text-muted-foreground">
      {matches.map((resource) => {
        const ref = resource.resource_ref;
        const label = (ref.label || resource.label || ref.local_path).trim() ||
          ref.local_path;
        return (
          <div
            key={resource.id}
            className="flex items-center gap-2"
          >
            <FolderOpen className="size-3 shrink-0" />
            <span className="truncate">
              {t(($) => $.resources.chat_hint_prefix)}
              <span className="font-medium text-foreground"> {label} </span>
              <span className="font-mono opacity-70">({ref.local_path})</span>
              <span> {t(($) => $.resources.chat_hint_suffix)}</span>
            </span>
          </div>
        );
      })}
    </div>
  );
}
