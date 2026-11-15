"use client";

import { ProjectDirectoryBinding } from "./project-directory-binding";
import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ChevronRight,
  FolderGit,
  FolderOpen,
  Pencil,
  Plus,
  Search,
  Star,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import {
  projectResourcesOptions,
  useCreateProjectResource,
  useDeleteProjectResource,
  useUpdateProjectResource,
} from "@silieco/core/projects";
import { useWorkspaceId } from "@silieco/core/hooks";
import { useAuthStore } from "@silieco/core/auth";
import { useCurrentWorkspace } from "@silieco/core/paths";
import { runtimeListOptions } from "@silieco/core/runtimes";
import {
  agentListOptions,
  memberListOptions,
} from "@silieco/core/workspace/queries";
import type {
  Agent,
  GithubRepoResourceRef,
  LocalDirectoryResourceRef,
  MemberWithUser,
  ProjectResource,
} from "@silieco/core/types";
import { Button } from "@silieco/ui/components/ui/button";
import { Badge } from "@silieco/ui/components/ui/badge";
import { Skeleton } from "@silieco/ui/components/ui/skeleton";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@silieco/ui/components/ui/popover";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@silieco/ui/components/ui/tooltip";
import {
  isDesktopShell,
  pickDirectory,
  useLocalDaemonStatus,
  validateLocalDirectory,
  type ValidateLocalDirectoryResult,
} from "../../platform";
import { useT } from "../../i18n";
import { githubShortLabel } from "../../common/github-url";
import {
  buildRuntimeMachines,
  type RuntimeMachine,
} from "../../runtimes/components/runtime-machines";

// Project Resources sidebar section.
//
// Type-dispatched at the row + add-flow level. Add a new resource_type by:
//   (1) extending the server validator
//   (2) extending ProjectResourceType in @silieco/core/types
//   (3) adding a render case in ResourceRow and an add-control here
function isGithubRef(r: ProjectResource): r is ProjectResource & {
  resource_ref: GithubRepoResourceRef;
} {
  return r.resource_type === "github_repo";
}

function isLocalDirectoryRef(r: ProjectResource): r is ProjectResource & {
  resource_ref: LocalDirectoryResourceRef;
} {
  return r.resource_type === "local_directory";
}

export function ProjectResourcesSection({
  projectId,
  canManage = false,
}: {
  projectId: string;
  canManage?: boolean;
}) {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const userId = useAuthStore((state) => state.user?.id);
  const workspace = useCurrentWorkspace();
  const daemonStatus = useLocalDaemonStatus();
  const [open, setOpen] = useState(true);
  const [addOpen, setAddOpen] = useState(false);
  const [repoSearch, setRepoSearch] = useState("");
  const [picking, setPicking] = useState(false);

  const { data: resources = [] } = useQuery(
    projectResourcesOptions(wsId, projectId),
  );
  const { data: runtimes = [], isPending: runtimesPending } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: agents = [], isPending: agentsPending } = useQuery(
    agentListOptions(wsId),
  );
  const { data: members = [], isPending: membersPending } = useQuery(
    memberListOptions(wsId),
  );
  const createResource = useCreateProjectResource(wsId, projectId);
  const updateResource = useUpdateProjectResource(wsId, projectId);
  const deleteResource = useDeleteProjectResource(wsId, projectId);

  // Desktop-only entry points. We hide (not just disable) on web so users
  // there don't see an action they can never complete — the spec calls for
  // read-only on web because the daemon-id check can't be performed in the
  // browser.
  const desktopMode = isDesktopShell();
  const localDaemonId = daemonStatus.daemonId;
  const runtimeMachines = useMemo(
    () =>
      buildRuntimeMachines(runtimes, {
        now: Date.now(),
        localDaemonId,
        localMachineName: daemonStatus.deviceName,
        currentUserId: userId,
        ensureLocalMachine: desktopMode,
      }),
    [daemonStatus.deviceName, desktopMode, localDaemonId, runtimes, userId],
  );
  const canConfigureLocalMachine =
    desktopMode &&
    (canManage ||
      runtimeMachines.some(
        (machine) =>
          machine.isCurrent &&
          machine.runtimes.some((runtime) => runtime.owner_id === userId),
      ));

  const repositories = resources.filter(isGithubRef);
  const localMappings = resources.filter(isLocalDirectoryRef);
  const standaloneMappings = localMappings.filter(
    (mapping) => !mapping.resource_ref.repository_resource_id,
  );
  const primaryRepositoryId =
    repositories.find((resource) => resource.resource_ref.primary)?.id ??
    repositories[0]?.id;

  const attachedUrls = new Set(
    resources.filter(isGithubRef).map((r) => r.resource_ref.url),
  );
  const attachedLocalPaths = new Set(
    localMappings
      .filter((r) => r.resource_ref.daemon_id === localDaemonId)
      .map((r) => r.resource_ref.local_path),
  );

  const repoQuery = repoSearch.trim().toLowerCase();
  const filteredRepos =
    workspace?.repos?.filter((repo) => repo.url.toLowerCase().includes(repoQuery)) ?? [];

  const handleAttach = async (url: string) => {
    try {
      await createResource.mutateAsync({
        resource_type: "github_repo",
        resource_ref: { url },
      });
      toast.success(t(($) => $.resources.toast_attached));
    } catch (err) {
      const msg = err instanceof Error ? err.message : t(($) => $.resources.toast_attach_failed);
      toast.error(msg);
    }
  };

  const handleAttachLocalDirectory = async (
    repository?: ProjectResource & { resource_ref: GithubRepoResourceRef },
  ) => {
    if (picking) return;
    setPicking(true);
    try {
      if (!localDaemonId || !daemonStatus.running) {
        toast.error(t(($) => $.resources.toast_local_daemon_not_running));
        return;
      }
      const existingMapping = localMappings.some(
        (resource) =>
          resource.resource_ref.daemon_id === localDaemonId &&
          resource.resource_ref.repository_resource_id === repository?.id,
      );
      if (existingMapping) {
        toast.error(t(($) => $.resources.toast_local_daemon_already_attached));
        return;
      }
      const picked = await pickDirectory();
      if (!picked.ok) {
        if (picked.reason && picked.reason !== "cancelled") {
          toast.error(
            picked.error ?? t(($) => $.resources.toast_local_pick_failed),
          );
        }
        return;
      }
      const path = picked.path ?? "";
      const fallbackLabel = picked.basename ?? path;
      if (attachedLocalPaths.has(path)) {
        toast.error(t(($) => $.resources.toast_local_already_attached));
        return;
      }
      const validation = await validateLocalDirectory(
        path,
        repository?.resource_ref.url,
      );
      if (!validation.ok) {
        toast.error(
          localValidationMessage(validation, {
            not_absolute: t(($) => $.resources.local_validate_not_absolute),
            not_found: t(($) => $.resources.local_validate_not_found),
            not_a_directory: t(($) => $.resources.local_validate_not_a_directory),
            not_readable: t(($) => $.resources.local_validate_not_readable),
            not_writable: t(($) => $.resources.local_validate_not_writable),
            not_git_repository: t(($) => $.resources.local_validate_not_git_repository),
            not_git_root: t(($) => $.resources.local_validate_not_git_root),
            missing_origin: t(($) => $.resources.local_validate_missing_origin),
            origin_mismatch: t(($) => $.resources.local_validate_origin_mismatch),
            git_unavailable: t(($) => $.resources.local_validate_git_unavailable),
            unsupported: t(($) => $.resources.local_validate_unsupported),
            fallback: t(($) => $.resources.toast_local_pick_failed),
          }),
        );
        return;
      }
      await createResource.mutateAsync({
        resource_type: "local_directory",
        resource_ref: {
          local_path: path,
          daemon_id: localDaemonId,
          ...(repository ? { repository_resource_id: repository.id } : {}),
          label: fallbackLabel,
        },
      });
      toast.success(t(($) => $.resources.toast_local_attached));
      setAddOpen(false);
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : t(($) => $.resources.toast_local_pick_failed);
      toast.error(msg);
    } finally {
      setPicking(false);
    }
  };

  const handleSetPrimary = async (
    repository: ProjectResource & { resource_ref: GithubRepoResourceRef },
  ) => {
    if (repository.id === primaryRepositoryId) return;
    try {
      await updateResource.mutateAsync({
        resourceId: repository.id,
        data: {
          resource_ref: { ...repository.resource_ref, primary: true },
        },
      });
      toast.success(t(($) => $.resources.toast_primary_updated));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.resources.toast_primary_update_failed),
      );
    }
  };

  const handleRemove = async (resource: ProjectResource) => {
    try {
      await deleteResource.mutateAsync(resource.id);
      toast.success(t(($) => $.resources.toast_removed));
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.resources.toast_remove_failed),
      );
    }
  };

  const handleRenameLocalDirectory = async (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
    nextLabel: string,
  ) => {
    const trimmed = nextLabel.trim();
    const previous = resource.resource_ref.label ?? resource.label ?? "";
    if (trimmed === previous.trim()) return;
    try {
      await updateResource.mutateAsync({
        resourceId: resource.id,
        data: {
          resource_ref: {
            ...resource.resource_ref,
            label: trimmed,
          },
        },
      });
      toast.success(t(($) => $.resources.toast_local_renamed));
    } catch (err) {
      const msg =
        err instanceof Error
          ? err.message
          : t(($) => $.resources.toast_local_rename_failed);
      toast.error(msg);
    }
  };

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.resources.section_header)}
        <ChevronRight
          className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div className="pl-2 space-y-1.5">
          <ProjectDirectoryBinding wsId={wsId} projectId={projectId} machines={runtimeMachines.filter(machine => machine.daemonId && (canManage || machine.runtimes.some(runtime => runtime.owner_id === userId)))} repositories={repositories.map(repository => ({id: repository.id, label: githubShortLabel(repository.resource_ref.url)}))} />
          {(repositories.length === 0 || standaloneMappings.length > 0) && (
            <ProjectLocalDirectoryCard
              mappings={standaloneMappings}
              machines={runtimeMachines}
              agents={agents}
              members={members}
              localDaemonId={localDaemonId}
              localMachineName={daemonStatus.deviceName}
              localDaemonRunning={daemonStatus.running}
              detailsLoading={
                runtimesPending || agentsPending || membersPending
              }
              canManage={canManage}
              canConfigureCurrentMachine={canConfigureLocalMachine}
              adding={picking || createResource.isPending}
              onAddLocalDirectory={() => handleAttachLocalDirectory()}
              onRemoveMapping={handleRemove}
              onRenameMapping={handleRenameLocalDirectory}
            />
          )}
          {repositories.length > 0 && (
            <div className="max-h-[28rem] space-y-2 overflow-y-auto pr-1">
              {repositories.map((repository) => (
                <RepositoryResourceCard
                  key={repository.id}
                  repository={repository}
                  primary={repository.id === primaryRepositoryId}
                  mappings={localMappings.filter(
                    (mapping) =>
                      mapping.resource_ref.repository_resource_id ===
                      repository.id,
                  )}
                  machines={runtimeMachines}
                  agents={agents}
                  members={members}
                  localDaemonId={localDaemonId}
                  localMachineName={daemonStatus.deviceName}
                  localDaemonRunning={daemonStatus.running}
                  detailsLoading={
                    runtimesPending || agentsPending || membersPending
                  }
                  canManageRepository={canManage}
                  canConfigureCurrentMachine={canConfigureLocalMachine}
                  adding={picking || createResource.isPending}
                  onAddLocalDirectory={() =>
                    handleAttachLocalDirectory(repository)
                  }
                  onSetPrimary={() => handleSetPrimary(repository)}
                  onRemoveRepository={() => handleRemove(repository)}
                  onRemoveMapping={handleRemove}
                  onRenameMapping={handleRenameLocalDirectory}
                />
              ))}
            </div>
          )}
          {canManage ? <Popover
            open={addOpen}
            onOpenChange={(v) => {
              setAddOpen(v);
              if (!v) setRepoSearch("");
            }}
          >
            <PopoverTrigger
              render={
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-caption text-muted-foreground hover:text-foreground"
                >
                  <Plus className="size-3" />
                  {t(($) => $.resources.add_button)}
                </Button>
              }
            />
            <PopoverContent align="start" className="w-72 p-2 space-y-2">
              <div className="text-caption font-medium text-muted-foreground">
                {t(($) => $.resources.popover_title)}
              </div>
              {workspace?.repos && workspace.repos.length > 0 && (
                <>
                  <div className="relative">
                    <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                    <input
                      type="text"
                      value={repoSearch}
                      onChange={(e) => setRepoSearch(e.target.value)}
                      aria-label={t(($) => $.resources.repos_search_placeholder)}
                      placeholder={t(($) => $.resources.repos_search_placeholder)}
                      className="h-8 w-full rounded-md border bg-transparent pl-7 pr-2 text-caption outline-none placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring"
                    />
                  </div>
                  <div className="max-h-48 space-y-1 overflow-y-auto">
                    {filteredRepos.length === 0 && repoQuery && (
                      <p className="py-2 text-center text-caption text-muted-foreground">
                        {t(($) => $.resources.repos_search_empty)}
                      </p>
                    )}
                    {filteredRepos.map((repo) => {
                      const isAttached = attachedUrls.has(repo.url);
                      const isDisabled = isAttached || createResource.isPending;
                      return (
                        // Use aria-disabled instead of the native `disabled` attribute so
                        // hover events still reach the tooltip trigger on attached rows
                        // (browsers suppress pointer events on disabled form controls).
                        <button
                          key={repo.url}
                          type="button"
                          aria-disabled={isDisabled}
                          onClick={async () => {
                            if (isDisabled) return;
                            await handleAttach(repo.url);
                            setAddOpen(false);
                          }}
                          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-caption text-left hover:bg-accent transition-colors aria-disabled:opacity-50 aria-disabled:cursor-not-allowed aria-disabled:hover:bg-transparent"
                        >
                          <FolderGit className="size-3.5" />
                          <Tooltip>
                            <TooltipTrigger
                              render={
                                <span className="truncate flex-1">{githubShortLabel(repo.url)}</span>
                              }
                            />
                            <TooltipContent side="top">{repo.url}</TooltipContent>
                          </Tooltip>
                          {isAttached && (
                            <span className="text-micro text-muted-foreground">
                              {t(($) => $.resources.attached_badge)}
                            </span>
                          )}
                        </button>
                      );
                    })}
                  </div>
                </>
              )}
              <CustomRepoForm
                onSubmit={async (url) => {
                  await handleAttach(url);
                  setAddOpen(false);
                }}
              />
            </PopoverContent>
          </Popover> : null}
        </div>
      )}
    </div>
  );
}

interface RepositoryResourceCardProps {
  repository: ProjectResource & { resource_ref: GithubRepoResourceRef };
  primary: boolean;
  mappings: Array<ProjectResource & { resource_ref: LocalDirectoryResourceRef }>;
  machines: RuntimeMachine[];
  agents: Agent[];
  members: MemberWithUser[];
  localDaemonId: string | null;
  localMachineName: string | null;
  localDaemonRunning: boolean;
  detailsLoading: boolean;
  canManageRepository: boolean;
  canConfigureCurrentMachine: boolean;
  adding: boolean;
  onAddLocalDirectory: () => Promise<void> | void;
  onSetPrimary: () => Promise<void> | void;
  onRemoveRepository: () => Promise<void> | void;
  onRemoveMapping: (resource: ProjectResource) => Promise<void> | void;
  onRenameMapping: (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
    nextLabel: string,
  ) => Promise<void>;
}

function RepositoryResourceCard({
  repository,
  primary,
  mappings,
  machines,
  agents,
  members,
  localDaemonId,
  localMachineName,
  localDaemonRunning,
  detailsLoading,
  canManageRepository,
  canConfigureCurrentMachine,
  adding,
  onAddLocalDirectory,
  onSetPrimary,
  onRemoveRepository,
  onRemoveMapping,
  onRenameMapping,
}: RepositoryResourceCardProps) {
  const { t } = useT("projects");
  const ref = repository.resource_ref;
  const display =
    repository.label ||
    (ref.ref
      ? `${githubShortLabel(ref.url)} @ ${ref.ref}`
      : githubShortLabel(ref.url));
  return (
    <section className="rounded-lg border bg-card/40 p-2.5" aria-label={display}>
      <div className="group flex min-w-0 items-center gap-2">
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          <FolderGit className="size-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-1.5">
            <Tooltip>
              <TooltipTrigger
                render={
                  <a
                    href={ref.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="truncate text-caption font-medium hover:underline"
                  >
                    {display}
                  </a>
                }
              />
              <TooltipContent side="top">{ref.url}</TooltipContent>
            </Tooltip>
            {primary && (
              <Badge variant="secondary" className="h-5 gap-1 px-1.5 text-micro">
                <Star className="size-2.5 fill-current" />
                {t(($) => $.resources.primary_repository)}
              </Badge>
            )}
          </div>
          <p className="truncate text-micro text-muted-foreground">
            {ref.ref || ref.default_branch_hint || t(($) => $.resources.default_branch)}
          </p>
        </div>
        {canManageRepository && !primary && (
          <Button variant="ghost" size="sm" className="h-7 px-2 text-micro" onClick={onSetPrimary}>
            {t(($) => $.resources.set_primary)}
          </Button>
        )}
        {canManageRepository && (
          <Button
            variant="ghost"
            size="icon-sm"
            className="text-muted-foreground"
            aria-label={t(($) => $.resources.remove_tooltip)}
            onClick={onRemoveRepository}
          >
            <Trash2 className="size-3" />
          </Button>
        )}
      </div>

      <div className="mt-2 border-l border-border/80 pl-3">
        <p className="mb-1.5 text-micro font-medium text-muted-foreground">
          {t(($) => $.resources.local_working_copies)}
        </p>
        <RuntimeMappingRows
          mappings={mappings}
          machines={machines}
          agents={agents}
          members={members}
          localDaemonId={localDaemonId}
          localMachineName={localMachineName}
          localDaemonRunning={localDaemonRunning}
          detailsLoading={detailsLoading}
          canManage={canManageRepository}
          canConfigureCurrentMachine={canConfigureCurrentMachine}
          adding={adding}
          onAddLocalDirectory={onAddLocalDirectory}
          onRemoveMapping={onRemoveMapping}
          onRenameMapping={onRenameMapping}
        />
      </div>
    </section>
  );
}

function ProjectLocalDirectoryCard(
  props: Omit<RuntimeMappingRowsProps, "canManage"> & { canManage: boolean },
) {
  const { t } = useT("projects");
  return (
    <section className="rounded-lg border bg-card/40 p-2.5">
      <div className="flex items-center gap-2">
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          <FolderOpen className="size-3.5" />
        </span>
        <div className="min-w-0">
          <p className="text-caption font-medium">
            {t(($) => $.resources.project_working_directory)}
          </p>
          <p className="text-micro text-muted-foreground">
            {t(($) => $.resources.project_working_directory_description)}
          </p>
        </div>
      </div>
      <div className="mt-2 border-l border-border/80 pl-3">
        <RuntimeMappingRows {...props} />
      </div>
    </section>
  );
}

interface RuntimeMappingRowsProps {
  mappings: Array<ProjectResource & { resource_ref: LocalDirectoryResourceRef }>;
  machines: RuntimeMachine[];
  agents: Agent[];
  members: MemberWithUser[];
  localDaemonId: string | null;
  localMachineName: string | null;
  localDaemonRunning: boolean;
  detailsLoading: boolean;
  canManage: boolean;
  canConfigureCurrentMachine: boolean;
  adding: boolean;
  onAddLocalDirectory: () => Promise<void> | void;
  onRemoveMapping: (resource: ProjectResource) => Promise<void> | void;
  onRenameMapping: (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
    nextLabel: string,
  ) => Promise<void>;
}

function RuntimeMappingRows({
  mappings,
  machines,
  agents,
  members,
  localDaemonId,
  localMachineName,
  localDaemonRunning,
  detailsLoading,
  canManage,
  canConfigureCurrentMachine,
  adding,
  onAddLocalDirectory,
  onRemoveMapping,
  onRenameMapping,
}: RuntimeMappingRowsProps) {
  const { t } = useT("projects");
  const knownMachines = machines.filter((machine) => machine.daemonId);
  const unknownMappings = mappings.filter(
    (mapping) =>
      !knownMachines.some(
        (machine) => machine.daemonId === mapping.resource_ref.daemon_id,
      ),
  );
  return (
    <div className="space-y-1.5">
      {knownMachines.map((machine) => {
        const mapping = mappings.find(
          (candidate) =>
            candidate.resource_ref.daemon_id === machine.daemonId,
        );
        if (!mapping) {
          return (
            <UnconfiguredMachineRow
              key={machine.daemonId || machine.id}
              machine={machine}
              current={machine.isCurrent}
              canAdd={
                machine.isCurrent &&
                canConfigureCurrentMachine &&
                localDaemonRunning &&
                !adding
              }
              onAdd={onAddLocalDirectory}
            />
          );
        }
        const runtimeIds = new Set(
          machine.runtimes.map((runtime) => runtime.id),
        );
        const affectedAgentNames = agents
          .filter(
            (agent) =>
              !agent.archived_at && runtimeIds.has(agent.runtime_id),
          )
          .map((agent) => agent.name);
        const ownerId =
          machine.runtimes.find((runtime) => runtime.owner_id)?.owner_id ??
          mapping.created_by;
        return (
          <LocalDirectoryRow
            key={mapping.id}
            resource={mapping}
            localDaemonId={localDaemonId}
            localMachineName={localMachineName}
            machineName={machine.title}
            machineOnline={
              machine.health === "online" ||
              (machine.isCurrent && localDaemonRunning)
            }
            ownerName={
              members.find((member) => member.user_id === ownerId)?.name ?? null
            }
            affectedAgentNames={affectedAgentNames}
            detailsLoading={detailsLoading}
            canEdit={machine.isCurrent && canConfigureCurrentMachine}
            canRemove={machine.isCurrent || canManage}
            onRemove={() => onRemoveMapping(mapping)}
            onRename={onRenameMapping}
          />
        );
      })}
      {unknownMappings.map((mapping) => (
        <LocalDirectoryRow
          key={mapping.id}
          resource={mapping}
          localDaemonId={localDaemonId}
          localMachineName={localMachineName}
          machineName={null}
          machineOnline={false}
          ownerName={
            members.find((member) => member.user_id === mapping.created_by)
              ?.name ?? null
          }
          affectedAgentNames={[]}
          detailsLoading={detailsLoading}
          canEdit={false}
          canRemove={canManage}
          onRemove={() => onRemoveMapping(mapping)}
          onRename={onRenameMapping}
        />
      ))}
      {knownMachines.length === 0 && mappings.length === 0 && (
        <p className="py-1 text-micro text-muted-foreground">
          {t(($) => $.resources.no_runtime_mappings)}
        </p>
      )}
    </div>
  );
}

function UnconfiguredMachineRow({
  machine,
  current,
  canAdd,
  onAdd,
}: {
  machine: RuntimeMachine;
  current: boolean;
  canAdd: boolean;
  onAdd: () => Promise<void> | void;
}) {
  const { t } = useT("projects");
  return (
    <div className="flex min-h-9 items-center gap-2 rounded-md border border-dashed px-2 py-1.5 text-caption">
      <FolderOpen className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1 truncate text-muted-foreground">
        {machine.title} · {t(($) => $.resources.local_not_configured)}
      </span>
      {current && canAdd && (
        <Button variant="ghost" size="sm" className="h-7 px-2 text-caption" onClick={onAdd}>
          <Plus className="size-3" />
          {t(($) => $.resources.add_local_directory_button)}
        </Button>
      )}
    </div>
  );
}

interface LocalDirectoryRowProps {
  resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef };
  localDaemonId: string | null;
  localMachineName: string | null;
  machineName: string | null;
  machineOnline: boolean;
  ownerName: string | null;
  affectedAgentNames: string[];
  detailsLoading: boolean;
  canEdit: boolean;
  canRemove: boolean;
  onRemove: () => void;
  onRename: (
    resource: ProjectResource & { resource_ref: LocalDirectoryResourceRef },
    nextLabel: string,
  ) => Promise<void>;
}

function LocalDirectoryRow({
  resource,
  localDaemonId,
  localMachineName,
  machineName,
  machineOnline,
  ownerName,
  affectedAgentNames,
  detailsLoading,
  canEdit,
  canRemove,
  onRemove,
  onRename,
}: LocalDirectoryRowProps) {
  const { t, i18n } = useT("projects");
  const ref = resource.resource_ref;
  const isForeignDaemon =
    localDaemonId !== null && ref.daemon_id !== localDaemonId;
  const isLocalUnknown = localDaemonId === null;
  const isCurrentMachine = !isForeignDaemon && !isLocalUnknown;
  const privateFallback = t(($) => $.resources.local_directory_fallback);
  const storedLabel = (ref.label || resource.label || "").trim();
  const display = (
    (storedLabel !== ref.local_path ? storedLabel : "") ||
    (isCurrentMachine ? localPathBasename(ref.local_path) : privateFallback)
  ).trim() || privateFallback;
  const resolvedMachineName =
    machineName ||
    (isCurrentMachine ? localMachineName : null) ||
    t(($) => $.resources.local_machine_unknown);
  const resolvedOwnerName =
    ownerName && ownerName !== "Unknown"
      ? ownerName
      : t(($) => $.resources.local_owner_unknown);
  const agentList = new Intl.ListFormat(i18n.language, {
    style: "long",
    type: "conjunction",
  }).format(affectedAgentNames);
  // Rename is hidden on foreign / unknown-daemon rows because the label
  // belongs to the owning device. Delete stays available so a manager can
  // drop a stale registration from any device.
  const mismatch = isForeignDaemon || isLocalUnknown;

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(display);

  const startEdit = () => {
    setDraft(display);
    setEditing(true);
  };
  const commit = async () => {
    setEditing(false);
    await onRename(resource, draft);
  };
  const cancel = () => {
    setEditing(false);
    setDraft(display);
  };

  return (
    <div
      className={`group rounded-md border bg-muted/20 px-2 py-2 text-caption ${
        mismatch ? "border-dashed" : ""
      }`}
    >
      <div className="flex items-center gap-2">
        <FolderOpen className="size-3.5 shrink-0 text-muted-foreground" />
        {editing ? (
          <input
            autoFocus
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onBlur={() => void commit()}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                void commit();
              } else if (e.key === "Escape") {
                e.preventDefault();
                cancel();
              }
            }}
            className="min-w-0 flex-1 rounded-sm border bg-transparent px-1 py-0.5 text-caption outline-none focus-visible:ring-1 focus-visible:ring-ring"
            aria-label={t(($) => $.resources.local_rename_label)}
          />
        ) : (
          <span className="min-w-0 flex-1 truncate font-medium text-foreground">
            {display}
          </span>
        )}
        {canEdit && !mismatch && !editing && (
          <button
            type="button"
            onClick={startEdit}
            className="rounded-sm p-0.5 opacity-0 transition-opacity hover:bg-accent focus-visible:opacity-100 group-hover:opacity-100"
            title={t(($) => $.resources.local_rename_tooltip)}
          >
            <Pencil className="size-3 text-muted-foreground" />
          </button>
        )}
        {canRemove ? <button
          type="button"
          onClick={onRemove}
          className="rounded-sm p-0.5 opacity-0 transition-opacity hover:bg-accent focus-visible:opacity-100 group-hover:opacity-100"
          title={t(($) => $.resources.remove_tooltip)}
        >
          <Trash2 className="size-3 text-muted-foreground" />
        </button> : null}
      </div>

      {detailsLoading ? (
        <div
          className="mt-1.5 space-y-1 pl-5"
          aria-label={t(($) => $.resources.local_details_loading)}
        >
          <Skeleton className="h-2.5 w-4/5" />
          <Skeleton className="h-2.5 w-full" />
        </div>
      ) : (
        <>
          <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5 pl-5 text-micro text-muted-foreground">
            <span>{resolvedOwnerName}</span>
            <span aria-hidden="true">·</span>
            <span className="truncate">{resolvedMachineName}</span>
            <span aria-hidden="true">·</span>
            <span>{t(($) => $.resources.local_execution_directory)}</span>
            <span aria-hidden="true">·</span>
            <span className="inline-flex items-center gap-1">
              <span
                className={`size-1.5 rounded-full ${
                  machineOnline ? "bg-success" : "bg-muted-foreground/40"
                }`}
              />
              {machineOnline
                ? t(($) => $.resources.local_status_online)
                : t(($) => $.resources.local_status_offline)}
            </span>
          </div>

          <div className="mt-1 space-y-0.5 pl-5 text-micro text-muted-foreground">
            <p>
              {affectedAgentNames.length > 0
                ? t(($) => $.resources.local_scope_agents, { agents: agentList })
                : t(($) => $.resources.local_scope_machine)}
            </p>
            <p>{t(($) => $.resources.local_not_synced)}</p>
            {isCurrentMachine && (
              <p className="truncate font-mono" title={ref.local_path}>
                {ref.local_path}
              </p>
            )}
          </div>
        </>
      )}
    </div>
  );
}

function localPathBasename(path: string): string {
  const parts = path.split(/[\\/]/).filter(Boolean);
  return parts.at(-1) ?? path;
}

function CustomRepoForm({
  onSubmit,
}: {
  onSubmit: (url: string) => Promise<void> | void;
}) {
  const { t } = useT("projects");
  const [url, setUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const handle = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = url.trim();
    if (!trimmed) return;
    setSubmitting(true);
    try {
      await onSubmit(trimmed);
      setUrl("");
    } finally {
      setSubmitting(false);
    }
  };
  return (
    <form onSubmit={handle} className="flex items-center gap-1.5 pt-1 border-t">
      <input
        type="text"
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        placeholder={t(($) => $.resources.url_placeholder)}
        className="flex-1 bg-transparent text-caption px-2 py-1 outline-none placeholder:text-muted-foreground"
      />
      <Button
        type="submit"
        size="sm"
        variant="ghost"
        className="h-6 px-2 text-caption"
        disabled={!url.trim() || submitting}
      >
        {t(($) => $.resources.url_submit)}
      </Button>
    </form>
  );
}

function localValidationMessage(
  result: ValidateLocalDirectoryResult,
  strings: {
    not_absolute: string;
    not_found: string;
    not_a_directory: string;
    not_readable: string;
    not_writable: string;
    not_git_repository: string;
    not_git_root: string;
    missing_origin: string;
    origin_mismatch: string;
    git_unavailable: string;
    unsupported: string;
    fallback: string;
  },
): string {
  switch (result.reason) {
    case "not_absolute":
      return strings.not_absolute;
    case "not_found":
      return strings.not_found;
    case "not_a_directory":
      return strings.not_a_directory;
    case "not_readable":
      return strings.not_readable;
    case "not_writable":
      return strings.not_writable;
    case "not_git_repository":
      return strings.not_git_repository;
    case "not_git_root":
      return strings.not_git_root;
    case "missing_origin":
      return strings.missing_origin;
    case "origin_mismatch":
      return strings.origin_mismatch;
    case "git_unavailable":
      return strings.git_unavailable;
    case "unsupported":
      return strings.unsupported;
    case "error":
    default:
      return result.error ?? strings.fallback;
  }
}
