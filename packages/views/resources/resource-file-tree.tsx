"use client";

import { useMemo, useState, type ReactNode } from "react";
import { ChevronRight, Folder, FolderKanban } from "lucide-react";
import type { FileResource } from "@silieco/core/types";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@silieco/ui/components/ui/collapsible";
import { useT } from "../i18n";

function FileBranch({ title, project = false, children }: { title: string; project?: boolean; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const Icon = project ? FolderKanban : Folder;
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full min-w-0 items-center gap-2 rounded-md px-3 py-2 text-left text-body hover:bg-muted/50">
        <ChevronRight className={`size-3.5 shrink-0 transition-transform ${open ? "rotate-90" : ""}`} />
        <Icon className="size-4 shrink-0 text-muted-foreground" />
        <span className="truncate">{title}</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        {open ? <div className="ml-4 min-w-0 border-l pl-2">{children}</div> : null}
      </CollapsibleContent>
    </Collapsible>
  );
}

export function ResourceFileTree({ files, renderFiles }: { files: FileResource[]; renderFiles: (files: FileResource[]) => ReactNode }) {
  const { t } = useT("resources");
  const projects = useMemo(() => {
    const groups = new Map<string, { title: string | null; tasks: Map<string, { title: string | null; number: number | null; files: FileResource[] }> }>();
    for (const file of files) {
      const projectId = file.source_project_id ?? "unassigned";
      let project = groups.get(projectId);
      if (!project) {
        project = { title: file.source_project_title, tasks: new Map() };
        groups.set(projectId, project);
      }
      const taskId = file.issue_id ?? "direct";
      let task = project.tasks.get(taskId);
      if (!task) {
        task = { title: file.source_issue_title, number: file.source_issue_number, files: [] };
        project.tasks.set(taskId, task);
      }
      task.files.push(file);
    }
    return groups;
  }, [files]);

  return (
    <div className="min-w-0 p-3">
      {[...projects].map(([projectId, project]) => (
        <FileBranch key={projectId} project title={project.title ?? t(($) => $.unassigned_project)}>
          {[...project.tasks].map(([taskId, task]) => (
            <FileBranch key={taskId} title={taskId === "direct"
              ? t(($) => $.project_files)
              : `${t(($) => $.source_task, { number: task.number ?? "–" })}${task.title ? ` · ${task.title}` : ""}`}>
              {renderFiles(task.files)}
            </FileBranch>
          ))}
        </FileBranch>
      ))}
    </div>
  );
}
