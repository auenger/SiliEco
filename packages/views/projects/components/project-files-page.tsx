"use client";

import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { useWorkspaceId } from "@silieco/core/hooks";
import { useWorkspacePaths } from "@silieco/core/paths";
import { projectDetailOptions } from "@silieco/core/projects/queries";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { ProjectDirectoryBrowser } from "./project-directory-browser";

export function ProjectFilesPage({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { t } = useT("resources");
  const project = useQuery(projectDetailOptions(wsId, projectId));
  return (
    <div className="flex h-full min-h-0 flex-1 flex-col bg-background">
      <header className="flex min-w-0 items-center gap-3 border-b px-5 py-3">
        <AppLink href={paths.projectResources(projectId)} aria-label={t(($) => $.directory_back)} title={t(($) => $.directory_back)} className="rounded-md p-2 hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
          <ArrowLeft className="size-4" />
        </AppLink>
        <h1 className="truncate text-body font-medium">{project.data?.title ?? t(($) => $.directory_title)}</h1>
        <span className="text-caption text-muted-foreground">{t(($) => $.directory_title)}</span>
      </header>
      <div className="flex min-h-0 flex-1 flex-col p-4">
        <ProjectDirectoryBrowser key={projectId} projectId={projectId} fullPage />
      </div>
    </div>
  );
}
