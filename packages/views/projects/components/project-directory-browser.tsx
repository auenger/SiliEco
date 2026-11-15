"use client";
import { HtmlBlockPreview } from "../../editor/html-block-preview";
import { ReadonlyContent } from "../../editor/readonly-content";
import { useState } from "react";
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { ChevronRight, Folder, FolderOpen, RefreshCw, ExternalLink } from "lucide-react";
import {
  projectResourcesOptions,
  projectDirectoryKey,
  projectDirectoryOptions,
  projectPreviewOptions,
} from "@silieco/core/projects";
import type {
  ProjectResource,
  LocalDirectoryResourceRef,
} from "@silieco/core/types";
import { useWorkspaceId } from "@silieco/core/hooks";
import { Button } from "@silieco/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@silieco/core/paths";
import { FileTypeIcon } from "../../common/file-type-icon";
import { Select, SelectTrigger, SelectValue, SelectContent, SelectItem } from "@silieco/ui/components/ui/select";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@silieco/ui/components/ui/resizable";
import { useT } from "../../i18n";

interface Scope {
  wsId: string;
  projectId: string;
  resourceId: string;
}
function Directory({
  scope,
  path,
  onSelect,
  selectedPath,
}: {
  scope: Scope;
  path: string;
  onSelect: (path: string) => void;
  selectedPath: string | null;
}) {
  const { t } = useT("resources");
  const query = useInfiniteQuery(
    projectDirectoryOptions(
      scope.wsId,
      scope.projectId,
      scope.resourceId,
      path,
    ),
  );
  const entries = query.data?.pages.flatMap((page) => page.entries) ?? [];
  return (
    <div className="space-y-0.5">
      {query.isPending && (
        <p className="p-2 text-caption text-muted-foreground">
          {t(($) => $.directory_loading)}
        </p>
      )}
      {query.isError && (
        <div role="alert" className="p-2 text-caption text-destructive">
          {query.error.message}
          <Button
            size="sm"
            variant="ghost"
            onClick={() => void query.refetch()}
          >
            {t(($) => $.directory_retry)}
          </Button>
        </div>
      )}
      {query.isSuccess && entries.length === 0 && (
        <p className="p-2 text-caption text-muted-foreground">
          {t(($) => $.directory_empty)}
        </p>
      )}
      {entries.map((entry) => (
        <DirectoryEntry
          key={entry.name}
          scope={scope}
          path={path ? `${path}/${entry.name}` : entry.name}
          name={entry.name}
          directory={entry.directory}
          onSelect={onSelect}
          selectedPath={selectedPath}
        />
      ))}
      {query.hasNextPage && (
        <Button
          size="sm"
          variant="ghost"
          disabled={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
        >
          {t(($) => $.directory_more)}
        </Button>
      )}
    </div>
  );
}
function DirectoryEntry({
  scope,
  path,
  name,
  directory,
  onSelect,
  selectedPath,
}: {
  scope: Scope;
  path: string;
  name: string;
  directory: boolean;
  onSelect: (path: string) => void;
  selectedPath: string | null;
}) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div>
      <button
        type="button"
        aria-expanded={directory ? expanded : undefined}
        title={path}
        aria-current={!directory && path === selectedPath ? "true" : undefined}
        className="flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-caption hover:bg-accent aria-[current=true]:bg-accent aria-[current=true]:font-medium focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        onClick={() => (directory ? setExpanded(!expanded) : onSelect(path))}
      >
        {directory ? (
          <>
            <ChevronRight
              className={`size-3 shrink-0 ${expanded ? "rotate-90" : ""}`}
            />
            {expanded ? <FolderOpen className="size-4 shrink-0 text-primary" /> : <Folder className="size-4 shrink-0 text-primary" />}
          </>
        ) : (
          <FileTypeIcon name={name} className="ml-4 size-4 shrink-0 text-muted-foreground" />
        )}
        <span className="truncate">{name}</span>
      </button>
      {directory && expanded && (
        <div className="ml-3 border-l pl-1">
          <Directory scope={scope} path={path} onSelect={onSelect} selectedPath={selectedPath} />
        </div>
      )}
    </div>
  );
}
function Preview({ scope, path }: { scope: Scope; path: string }) {
  const { t } = useT("resources");
  const query = useQuery(
    projectPreviewOptions(scope.wsId, scope.projectId, scope.resourceId, path),
  );
  const data = query.data;
  const image =
    data &&
    ["image/png", "image/jpeg", "image/gif", "image/webp"].includes(data.mime);
  return (
    <div className="h-full min-w-0 flex-1 overflow-auto p-5">
      <p className="mb-3 break-all text-caption text-muted-foreground">
        {path}
      </p>
      {query.isPending && <p>{t(($) => $.directory_loading)}</p>}
      {query.isError && (
        <p role="alert" className="text-caption text-destructive">
          {query.error.message}
        </p>
      )}
      {image ? (
        <img
          src={`data:${data.mime};base64,${data.content}`}
          alt={path}
          className="max-h-full max-w-full object-contain"
        />
      ) : data?.mime === "text/html" ? (
        <HtmlBlockPreview html={data.content} />
      ) : data?.mime === "text/markdown" ? (
        <ReadonlyContent content={data.content} />
      ) : data?.mime === "text/plain" ? (
        <pre className="whitespace-pre-wrap break-all font-mono text-caption">
          {data.content}
        </pre>
      ) : null}
    </div>
  );
}
export function ProjectDirectoryBrowser({ projectId, fullPage = false }: { projectId: string; fullPage?: boolean }) {
  const paths = useWorkspacePaths();
  const { t } = useT("resources");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const resourcesQuery = useQuery(
    projectResourcesOptions(wsId, projectId),
  );
  const resources = resourcesQuery.data ?? [];
  const mappings = resources.filter(
    (r): r is ProjectResource & { resource_ref: LocalDirectoryResourceRef } =>
      r.resource_type === "local_directory",
  );
  const [selectedId, setSelectedId] = useState("");
  const [selectedFile, setSelectedFile] = useState<{ resourceId: string; path: string } | null>(null);
  const selected = mappings.find((r) => r.id === selectedId) ?? mappings[0];
  if (!selected) return fullPage ? <div className="p-6 text-body text-muted-foreground" role={resourcesQuery.isError ? "alert" : undefined}>
    {resourcesQuery.isError ? resourcesQuery.error.message : t(($) => resourcesQuery.isPending ? $.directory_loading : $.directory_unbound)}
  </div> : null;
  const path = selectedFile?.resourceId === selected.id ? selectedFile.path : null;
  const scope = { wsId, projectId, resourceId: selected.id };
  return (
    <section className={fullPage ? "flex min-h-0 flex-1 flex-col" : "border-b p-4"}>
      <div className="flex min-w-0 flex-wrap items-center gap-3 pb-3">
        <FolderOpen className="size-5 shrink-0 text-primary" />
        <div className="min-w-0 flex-1">
          <h3 className="text-body font-medium">{t(($) => $.directory_title)}</h3>
          {mappings.length === 1 ? (
            <p className="truncate font-mono text-caption text-muted-foreground" title={selected.resource_ref.local_path}>{selected.resource_ref.local_path}</p>
          ) : (
            <Select value={selected.id} items={mappings.map((r) => ({ value: r.id, label: r.resource_ref.label || r.resource_ref.local_path || r.id }))}
              onValueChange={(value) => { if (value) { setSelectedId(value); setSelectedFile(null); } }}>
              <SelectTrigger aria-label={t(($) => $.directory_title)} className="mt-1 w-full max-w-lg"><SelectValue /></SelectTrigger>
              <SelectContent>{mappings.map((r) => <SelectItem key={r.id} value={r.id}>{r.resource_ref.label || r.resource_ref.local_path || r.id}</SelectItem>)}</SelectContent>
            </Select>
          )}
        </div>
        <Button size="icon" variant="ghost" aria-label={t(($) => $.directory_refresh)} title={t(($) => $.directory_refresh)}
          onClick={() => void qc.invalidateQueries({ queryKey: projectDirectoryKey(wsId, projectId) })}>
          <RefreshCw className="size-4" />
        </Button>
        {!fullPage && <AppLink href={paths.projectFiles(projectId)} target="_blank" newTabTitle={t(($) => $.directory_title)}
          className="inline-flex items-center gap-2 rounded-md border px-3 py-2 text-caption font-medium hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
          <ExternalLink className="size-4" />{t(($) => $.directory_open)}
        </AppLink>}
      </div>
      <div key={`${selected.id}:${selected.resource_ref.local_path}`}
        className={fullPage ? "min-h-0 flex-1 overflow-hidden rounded-lg border" : "h-[min(65vh,640px)] min-h-80 overflow-hidden rounded-lg border"}>
        <ResizablePanelGroup orientation="horizontal">
          <ResizablePanel defaultSize="28%" minSize="160px" maxSize="50%">
            <div className="h-full overflow-auto bg-muted/20 p-2">
              <Directory scope={scope} path="" selectedPath={path} onSelect={(path) => setSelectedFile({ resourceId: selected.id, path })} />
            </div>
          </ResizablePanel>
          <ResizableHandle withHandle />
          <ResizablePanel minSize="180px">
            {path ? <Preview key={`${selected.id}:${path}`} scope={scope} path={path} /> : (
              <div className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center text-muted-foreground">
                <FolderOpen className="size-10 opacity-40" />
                <p className="text-body">{t(($) => $.directory_select)}</p>
                <p className="max-w-sm text-caption">{t(($) => $.directory_hint)}</p>
              </div>
            )}
          </ResizablePanel>
        </ResizablePanelGroup>
      </div>
    </section>
  );
}
