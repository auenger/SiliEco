"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, CornerDownRight, Download, FileText, FolderGit, Link2, Loader2, PackageOpen, Search, X } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@silieco/core/hooks";
import { useCurrentWorkspace, useWorkspacePaths } from "@silieco/core/paths";
import {
  useFileResources,
  useCreateAttachmentReference,
  useDeleteAttachmentReference,
  targetAttachmentReferencesOptions,
  workspaceAttachmentReferencesOptions,
} from "@silieco/core/resources";
import { issueAttachmentsOptions } from "@silieco/core/issues/queries";
import type { AttachmentReference, AttachmentReferenceTargetType, FileResource } from "@silieco/core/types";
import { Button } from "@silieco/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@silieco/ui/components/ui/dialog";
import { Skeleton } from "@silieco/ui/components/ui/skeleton";
import { CollectionPageHeader, CollectionPageState } from "../layout";
import { AppLink } from "../navigation";
import { useDownloadAttachment } from "../editor/use-download-attachment";
import { useT } from "../i18n";
import { ResourceFileTree } from "./resource-file-tree";
import { FileResourceLeadingVisual } from "./file-resource-leading-visual";

function formatBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let size = value / 1024;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size.toFixed(size >= 10 ? 0 : 1)} ${units[index]}`;
}

function ResourceList({
  files,
  referencedIds,
  onRemoveReference,
  referencesByAttachment,
}: {
  files: FileResource[];
  referencedIds?: Set<string>;
  onRemoveReference?: (attachmentId: string) => void;
  referencesByAttachment?: Map<string, AttachmentReference[]>;
}) {
  const { t } = useT("resources");
  const paths = useWorkspacePaths();
  const download = useDownloadAttachment();

  if (files.length === 0) {
    return (
      <CollectionPageState
        icon={PackageOpen}
        title={t(($) => $.empty_title)}
        description={t(($) => $.empty_description)}
      />
    );
  }

  return (
    <div className="divide-y border-y">
      {files.map((file) => {
        const references = referencesByAttachment?.get(file.id) ?? [];
        return (
        <div key={file.id} className="flex min-w-0 items-start gap-3 px-5 py-3 hover:bg-muted/30">
          <FileResourceLeadingVisual file={file} />
          <div className="min-w-0 flex-1">
            <AppLink
              href={paths.attachmentPreview(file.id)}
              className="block truncate text-body font-medium hover:underline"
            >
              {file.filename}
            </AppLink>
            <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-caption text-muted-foreground">
              <span>{formatBytes(file.size_bytes)}</span>
              {file.source_issue_number !== null && file.issue_id ? (
                <AppLink href={paths.taskDetail(file.issue_id)} className="hover:text-foreground hover:underline">
                  {t(($) => $.source_task, { number: file.source_issue_number })}
                </AppLink>
              ) : null}
              {file.source_project_title && file.source_project_id ? (
                <AppLink href={paths.projectDetail(file.source_project_id)} className="hover:text-foreground hover:underline">
                  {file.source_project_title}
                </AppLink>
              ) : null}
              {file.reference_count > 0 ? (
                <span>{t(($) => $.reference_count, { count: file.reference_count })}</span>
              ) : null}
            </div>
            {referencesByAttachment ? (
              <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
                <span className="inline-flex items-center gap-1">
                  <CornerDownRight className="size-3" aria-hidden="true" />
                  {references.length > 0
                    ? t(($) => $.referenced_by)
                    : t(($) => $.no_references)}
                </span>
                {references.map((reference) => (
                  <AppLink
                    key={reference.id}
                    href={reference.target_type === "issue"
                      ? paths.taskDetail(reference.target_id)
                      : paths.projectDetail(reference.target_id)}
                    className="rounded bg-muted px-1.5 py-0.5 text-foreground/80 hover:bg-accent hover:text-foreground"
                  >
                    {reference.target_type === "issue"
                      ? t(($) => $.reference_task, {
                          number: reference.target_issue_number ?? "–",
                          title: reference.target_title ?? "",
                          project: reference.target_project_title ?? "",
                        })
                      : t(($) => $.reference_project, {
                          title: reference.target_title ?? reference.target_project_title ?? "",
                        })}
                  </AppLink>
                ))}
              </div>
            ) : null}
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t(($) => $.download)}
            title={t(($) => $.download)}
            onClick={() => void download(file.id)}
          >
            <Download className="size-4" />
          </Button>
          {referencedIds?.has(file.id) && onRemoveReference ? (
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t(($) => $.remove_reference)}
              title={t(($) => $.remove_reference)}
              onClick={() => onRemoveReference(file.id)}
            >
              <X className="size-4" />
            </Button>
          ) : null}
        </div>
        );
      })}
    </div>
  );
}

function FilePageControl({ query }: { query: ReturnType<typeof useFileResources> }) {
  const { t } = useT("resources");
  if (!query.hasNextPage && !query.isError) return null;
  return (
    <div className="flex shrink-0 flex-col items-center gap-2 p-4">
      {query.isError && <p role="alert" className="text-caption text-destructive">{t(($) => $.load_failed)}</p>}
      <Button variant="outline" size="sm" disabled={query.isFetching}
        onClick={() => void (query.hasNextPage ? query.fetchNextPage() : query.refetch())}>
        {query.isFetching && <Loader2 className="size-4 animate-spin" />}
        {query.isError ? t(($) => $.retry) : t(($) => $.load_more)}
      </Button>
    </div>
  );
}

function LoadingList() {
  return (
    <div className="space-y-4 px-5 py-5">
      {[0, 1, 2].map((item) => <Skeleton key={item} className="h-12 w-full" />)}
    </div>
  );
}

export function AttachExistingFileButton({
  workspaceId,
  targetType,
  targetId,
  contextProjectId,
  compact = false,
  onReferenced,
}: {
  workspaceId: string;
  targetType: AttachmentReferenceTargetType;
  targetId: string;
  contextProjectId?: string | null;
  compact?: boolean;
  onReferenced?: (files: FileResource[]) => void;
}) {
  const { t } = useT("resources");
  const wsId = workspaceId;
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [scope, setScope] = useState<"related" | "all">(
    contextProjectId && targetType === "issue" ? "related" : "all",
  );
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const fileQuery = useFileResources(wsId, undefined, open);
  const { files, isLoading } = fileQuery;
  const { data: targetReferences } = useQuery({
    ...targetAttachmentReferencesOptions(wsId, targetType, targetId),
    enabled: open,
  });
  const createReference = useCreateAttachmentReference(wsId);
  const directReferenceIds = useMemo(
    () =>
      new Set(
        targetReferences?.references.map((reference) => reference.attachment_id) ?? [],
      ),
    [targetReferences?.references],
  );

  const isAlreadyAvailable = (file: FileResource) =>
    directReferenceIds.has(file.id) ||
    (targetType === "issue" && file.issue_id === targetId) ||
    (targetType === "project" && file.source_project_id === targetId);

  const visibleFiles = useMemo(() => {
    const query = search.trim().toLocaleLowerCase();
    return [...files]
      .filter((file) => {
        if (
          scope === "related" &&
          contextProjectId &&
          file.source_project_id !== contextProjectId
        ) {
          return false;
        }
        if (!query) return true;
        return [
          file.filename,
          file.source_issue_title,
          file.source_issue_number?.toString(),
          file.source_project_title,
        ].some((value) => value?.toLocaleLowerCase().includes(query));
      })
      .sort((left, right) => {
        const leftRelated = left.source_project_id === contextProjectId ? 1 : 0;
        const rightRelated = right.source_project_id === contextProjectId ? 1 : 0;
        if (leftRelated !== rightRelated) return rightRelated - leftRelated;
        return Date.parse(right.created_at) - Date.parse(left.created_at);
      });
  }, [contextProjectId, files, scope, search]);

  const toggleSelected = (attachmentId: string) => {
    setSelectedIds((current) => {
      const next = new Set(current);
      if (next.has(attachmentId)) next.delete(attachmentId);
      else next.add(attachmentId);
      return next;
    });
  };

  const attachSelected = async () => {
    if (selectedIds.size === 0) return;
    const selectedFiles = files.filter((file) => selectedIds.has(file.id));
    try {
      for (const attachmentId of selectedIds) {
        await createReference.mutateAsync({ targetType, targetId, attachmentId });
      }
      toast.success(t(($) => $.references_added, { count: selectedIds.size }));
      onReferenced?.(selectedFiles);
      setOpen(false);
      setSelectedIds(new Set());
      setSearch("");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.reference_failed));
    }
  };

  const handleOpenChange = (nextOpen: boolean) => {
    setOpen(nextOpen);
    if (!nextOpen) {
      setSelectedIds(new Set());
      setSearch("");
      setScope(contextProjectId && targetType === "issue" ? "related" : "all");
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          <Button
            variant={compact ? "ghost" : "outline"}
            size={compact ? "icon-sm" : "sm"}
            className={compact ? undefined : "gap-1.5"}
            aria-label={compact ? t(($) => $.reference_existing) : undefined}
            title={compact ? t(($) => $.reference_existing) : undefined}
          />
        }
      >
        <Link2 className="size-3.5" />
        {compact ? null : t(($) => $.reference_existing)}
      </DialogTrigger>
      <DialogContent className="flex max-h-[82vh] max-w-2xl flex-col overflow-hidden p-0">
        <DialogHeader className="px-5 pt-5">
          <DialogTitle>{t(($) => $.picker_title)}</DialogTitle>
          <DialogDescription>{t(($) => $.picker_description)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-3 px-5">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <input
              autoFocus
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t(($) => $.search_placeholder)}
              className="h-9 w-full rounded-md border bg-background pl-8 pr-3 text-body outline-none placeholder:text-muted-foreground focus-visible:ring-1 focus-visible:ring-ring"
            />
          </div>
          {contextProjectId ? (
            <div className="flex items-center gap-1">
              <Button
                type="button"
                size="sm"
                variant={scope === "related" ? "secondary" : "ghost"}
                className="h-7 px-2 text-caption"
                onClick={() => setScope("related")}
              >
                {t(($) => $.related_files)}
              </Button>
              <Button
                type="button"
                size="sm"
                variant={scope === "all" ? "secondary" : "ghost"}
                className="h-7 px-2 text-caption"
                onClick={() => setScope("all")}
              >
                {t(($) => $.all_files)}
              </Button>
            </div>
          ) : null}
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto pb-2">
          {isLoading ? <LoadingList /> : files.length === 0 ? (
            <CollectionPageState icon={PackageOpen} title={t(($) => $.empty_title)} />
          ) : visibleFiles.length === 0 ? (
            <CollectionPageState
              icon={Search}
              title={t(($) => $.no_matching_files)}
              description={t(($) => $.try_all_files)}
            />
          ) : (
            <div className="divide-y border-y">
              {visibleFiles.map((file) => {
                const alreadyAvailable = isAlreadyAvailable(file);
                const selected = selectedIds.has(file.id);
                return (
                <button
                  key={file.id}
                  type="button"
                  className="flex w-full items-center gap-3 px-5 py-3 text-left hover:bg-muted/50 disabled:cursor-default disabled:opacity-60"
                  disabled={alreadyAvailable || createReference.isPending}
                  onClick={() => toggleSelected(file.id)}
                >
                  <span className={`flex size-4 shrink-0 items-center justify-center rounded border ${selected ? "border-primary bg-primary text-primary-foreground" : "border-muted-foreground/40"}`}>
                    {selected ? <Check className="size-3" /> : null}
                  </span>
                  <FileResourceLeadingVisual file={file} compact />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-body">{file.filename}</span>
                    <span className="mt-0.5 flex flex-wrap gap-x-2 text-caption text-muted-foreground">
                      {file.source_issue_number !== null ? (
                        <span>{t(($) => $.source_task, { number: file.source_issue_number })}</span>
                      ) : null}
                      {file.source_issue_title ? <span className="truncate">{file.source_issue_title}</span> : null}
                      {file.source_project_title ? <span>{file.source_project_title}</span> : null}
                    </span>
                  </span>
                  <span className="shrink-0 text-caption text-muted-foreground">
                    {alreadyAvailable ? t(($) => $.already_available) : formatBytes(file.size_bytes)}
                  </span>
                </button>
                );
              })}
            </div>
          )}
        </div>
        {fileQuery.hasNextPage && search.trim() && (
          <p className="px-5 text-caption text-muted-foreground">{t(($) => $.search_loaded)}</p>
        )}
        <FilePageControl query={fileQuery} />
        <DialogFooter className="m-0 shrink-0 px-5 py-3">
          <span className="mr-auto self-center text-caption text-muted-foreground">
            {t(($) => $.selected_count, { count: selectedIds.size })}
          </span>
          <Button
            type="button"
            disabled={selectedIds.size === 0 || createReference.isPending}
            onClick={() => void attachSelected()}
          >
            {createReference.isPending ? <Loader2 className="size-4 animate-spin" /> : <Link2 className="size-4" />}
            {t(($) => $.add_references)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function ResourcesPage() {
  const { t } = useT("resources");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();
  const fileQuery = useFileResources(wsId);
  const { files, isLoading } = fileQuery;
  const { data: workspaceReferences, isLoading: referencesLoading } = useQuery(
    workspaceAttachmentReferencesOptions(wsId),
  );
  const referencesByAttachment = useMemo(() => {
    const map = new Map<string, AttachmentReference[]>();
    for (const reference of workspaceReferences?.references ?? []) {
      const list = map.get(reference.attachment_id) ?? [];
      list.push(reference);
      map.set(reference.attachment_id, list);
    }
    return map;
  }, [workspaceReferences?.references]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      <CollectionPageHeader
        icon={FileText}
        title={t(($) => $.title)}
        count={fileQuery.total}
        description={t(($) => $.description)}
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        {(workspace?.repos?.length ?? 0) > 0 ? (
          <section className="border-b px-5 py-4">
            <h2 className="mb-2 text-body font-medium">{t(($) => $.shared_repositories)}</h2>
            <div className="flex flex-wrap gap-2">
              {workspace?.repos?.map((repo) => (
                <a
                  key={repo.url}
                  href={repo.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex max-w-full items-center gap-2 rounded-md border px-3 py-2 text-caption hover:bg-muted/50"
                >
                  <FolderGit className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="truncate">{repo.url}</span>
                </a>
              ))}
            </div>
          </section>
        ) : null}
        <div className="px-5 py-3 text-body font-medium">{t(($) => $.shared_files)}</div>
        {isLoading || referencesLoading ? <LoadingList /> : files.length === 0 ? (
          <ResourceList files={[]} />
        ) : (
          <ResourceFileTree files={files} renderFiles={(group) => (
            <ResourceList files={group} referencesByAttachment={referencesByAttachment} />
          )} />
        )}
        <FilePageControl query={fileQuery} />
      </div>
    </div>
  );
}

export function IssueFilesSection({ issueId }: { issueId: string }) {
  const { t } = useT("resources");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const download = useDownloadAttachment();
  const [page, setPage] = useState({ issueId, limit: 50 });
  const limit = page.issueId === issueId ? page.limit : 50;
  const { data: attachments = [] } = useQuery(issueAttachmentsOptions(issueId));
  const { data: references } = useQuery(
    targetAttachmentReferencesOptions(wsId, "issue", issueId),
  );
  const deleteReference = useDeleteAttachmentReference(wsId);
  const referencedIds = new Set(
    references?.references.map((reference) => reference.attachment_id) ?? [],
  );

  if (attachments.length === 0) return null;

  const removeReference = async (attachmentId: string) => {
    try {
      await deleteReference.mutateAsync({
        targetType: "issue",
        targetId: issueId,
        attachmentId,
      });
      toast.success(t(($) => $.reference_removed));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.reference_remove_failed));
    }
  };

  return (
    <div>
      <div className="mb-2 px-2 py-1 text-caption font-medium">
        {t(($) => $.task_files)}
      </div>
      <div className="space-y-1 pl-2">
        {attachments.slice(0, limit).map((file) => (
          <div key={file.id} className="group flex min-w-0 items-center gap-2 rounded-md px-2 py-1 hover:bg-accent/50">
            <FileResourceLeadingVisual file={file} compact />
            <AppLink
              href={paths.attachmentPreview(file.id)}
              className="min-w-0 flex-1 truncate text-caption hover:underline"
            >
              {file.filename}
            </AppLink>
            <button
              type="button"
              className="rounded p-0.5 text-muted-foreground opacity-0 hover:text-foreground group-hover:opacity-100"
              title={t(($) => $.download)}
              onClick={() => void download(file.id)}
            >
              <Download className="size-3" />
            </button>
            {referencedIds.has(file.id) ? (
              <button
                type="button"
                className="rounded p-0.5 text-muted-foreground opacity-0 hover:text-destructive group-hover:opacity-100"
                title={t(($) => $.remove_reference)}
                onClick={() => void removeReference(file.id)}
              >
                <X className="size-3" />
              </button>
            ) : null}
          </div>
        ))}
      </div>
      {attachments.length > limit && (
        <Button variant="ghost" size="sm" onClick={() => setPage({ issueId, limit: limit + 50 })}>
          {t(($) => $.load_more)}
        </Button>
      )}
    </div>
  );
}

export function ProjectFilesPage({
  projectId,
  canManage = false,
}: {
  projectId: string;
  canManage?: boolean;
}) {
  const { t } = useT("resources");
  const wsId = useWorkspaceId();
  const fileQuery = useFileResources(wsId, projectId);
  const { files, isLoading } = fileQuery;
  const { data: references } = useQuery(
    targetAttachmentReferencesOptions(wsId, "project", projectId),
  );
  const deleteReference = useDeleteAttachmentReference(wsId);
  const referencedIds = new Set(
    references?.references.map((reference) => reference.attachment_id) ?? [],
  );

  const removeReference = async (attachmentId: string) => {
    try {
      await deleteReference.mutateAsync({
        targetType: "project",
        targetId: projectId,
        attachmentId,
      });
      toast.success(t(($) => $.reference_removed));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.reference_remove_failed));
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center justify-between border-b px-5 py-3">
        <div>
          <h2 className="text-body font-medium">{t(($) => $.project_files)}</h2>
          <p className="text-caption text-muted-foreground">{t(($) => $.project_description)}</p>
        </div>
        {canManage ? (
          <AttachExistingFileButton
            workspaceId={wsId}
            targetType="project"
            targetId={projectId}
            contextProjectId={projectId}
          />
        ) : null}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {isLoading ? (
          <LoadingList />
        ) : (
          files.length === 0 ? <ResourceList files={[]} /> :
          <ResourceFileTree key={projectId} files={files} renderFiles={(group) => (
            <ResourceList
              files={group}
              referencedIds={canManage ? referencedIds : undefined}
              onRemoveReference={canManage ? (id) => void removeReference(id) : undefined}
            />
          )} />
        )}
        <FilePageControl query={fileQuery} />
      </div>
    </div>
  );
}
