"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  GitBranch,
  ListTodo,
  Plus,
  Route,
} from "lucide-react";
import { useWorkspaceId } from "@silieco/core/hooks";
import { useModalStore } from "@silieco/core/modals";
import { useWorkspacePaths } from "@silieco/core/paths";
import {
  workflowInstanceOptions,
  workflowInstancesOptions,
} from "@silieco/core/workflows";
import { Badge } from "@silieco/ui/components/ui/badge";
import { Button } from "@silieco/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@silieco/ui/components/ui/select";
import { cn } from "@silieco/ui/lib/utils";
import { IssueSurface } from "../../issues/surface/issue-surface";
import { WorkflowBoard } from "../../workflows/components";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";

type TaskManagementMode = "lifecycle" | "stage";

export function ProjectTaskSurface({
  projectId,
  workflowRunId,
}: {
  projectId: string;
  workflowRunId?: string;
}) {
  const { t } = useT("workflows");
  const workspaceId = useWorkspaceId();
  const workspacePaths = useWorkspacePaths();
  const navigation = useNavigation();
  const [mode, setMode] = useState<TaskManagementMode>("lifecycle");
  const { data: runs = [] } = useQuery(
    workflowInstancesOptions(workspaceId, undefined, projectId),
  );
  const availableRuns = useMemo(
    () =>
      runs.filter(
        (run) =>
          !run.archived_at &&
          run.status !== "completed" &&
          run.status !== "cancelled",
      ),
    [runs],
  );
  const hasAvailableRuns = availableRuns.length > 0;
  const [selectedRunId, setSelectedRunId] = useState<string>();
  const effectiveMode: TaskManagementMode =
    workflowRunId
      ? "stage"
      : mode === "stage" && !hasAvailableRuns
        ? "lifecycle"
        : mode;

  useEffect(() => {
    setMode(workflowRunId ? "stage" : "lifecycle");
    setSelectedRunId(workflowRunId);
  }, [projectId, workflowRunId]);

  useEffect(() => {
    if (!hasAvailableRuns && mode === "stage") {
      setMode("lifecycle");
    }
  }, [hasAvailableRuns, mode]);

  useEffect(() => {
    if (workflowRunId) {
      setSelectedRunId(workflowRunId);
      return;
    }
    if (
      !selectedRunId ||
      !availableRuns.some((run) => run.id === selectedRunId)
    ) {
      setSelectedRunId(availableRuns[0]?.id);
    }
  }, [availableRuns, selectedRunId, workflowRunId]);
  const { data: selectedRun } = useQuery({
    ...workflowInstanceOptions(workspaceId, selectedRunId ?? ""),
    enabled: effectiveMode === "stage" && Boolean(selectedRunId),
  });
  const openStageTask = () => {
    const stage =
      selectedRun?.stages?.find(
        (item) => item.id === selectedRun.current_stage_id,
      ) ?? selectedRun?.stages?.[0];
    if (!selectedRun || !stage) return;
    useModalStore.getState().open("create-issue", {
      project_id: projectId,
      workflow_instance_id: selectedRun.id,
      workflow_stage_id: stage.id,
    });
  };
  const openLifecycleTask = () => {
    useModalStore.getState().open("create-issue", {
      project_id: projectId,
    });
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex min-h-12 flex-wrap items-center justify-between gap-3 border-b border-border/70 px-4 py-2">
        <div className="flex items-center gap-2">
          {workflowRunId ? (
            <div className="flex min-w-0 items-center gap-2">
              <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-brand/10 text-brand">
                <Route className="size-3.5" />
              </span>
              <div className="min-w-0">
                <p className="max-w-72 truncate text-caption font-semibold text-foreground">
                  {selectedRun?.title ?? t(($) => $.board.loading)}
                </p>
                <p className="text-micro text-muted-foreground">
                  {t(($) => $.project_tasks.stage_hint)}
                </p>
              </div>
            </div>
          ) : (
          <div className="flex rounded-lg bg-muted p-0.5">
            <button
              type="button"
              className={cn(
                "flex h-7 items-center gap-1.5 rounded-md px-2.5 text-caption font-medium transition-colors",
                effectiveMode === "lifecycle"
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
              )}
              onClick={() => setMode("lifecycle")}
            >
              <ListTodo className="size-3.5" />
              {t(($) => $.project_tasks.lifecycle)}
            </button>
            <button
              type="button"
              disabled={!hasAvailableRuns}
              className={cn(
                "flex h-7 items-center gap-1.5 rounded-md px-2.5 text-caption font-medium transition-colors",
                effectiveMode === "stage"
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground",
                !hasAvailableRuns && "cursor-not-allowed opacity-45",
              )}
              onClick={() => {
                if (hasAvailableRuns) setMode("stage");
              }}
            >
              <GitBranch className="size-3.5" />
              {t(($) => $.project_tasks.stage)}
            </button>
          </div>
          )}
          {!workflowRunId && (
          <span className="hidden text-caption text-muted-foreground sm:inline">
            {effectiveMode === "lifecycle"
              ? t(($) => $.project_tasks.lifecycle_hint)
              : t(($) => $.project_tasks.stage_hint)}
          </span>
          )}
        </div>

        {effectiveMode === "lifecycle" ? (
          <Button size="sm" onClick={openLifecycleTask}>
            <Plus className="size-3.5" />
            {t(($) => $.project_tasks.create_task)}
          </Button>
        ) : hasAvailableRuns && (
          <div className="flex items-center gap-2">
            {!workflowRunId && (
            <Select
              items={availableRuns.map((run) => ({
                value: run.id,
                label: run.title,
              }))}
              value={selectedRunId}
              onValueChange={(value) => {
                if (value) setSelectedRunId(value);
              }}
            >
              <SelectTrigger
                size="sm"
                className="max-w-56"
                aria-label={t(($) => $.assignment.select_run)}
              >
                <Route className="size-3.5 text-muted-foreground" />
                <SelectValue />
              </SelectTrigger>
              <SelectContent
                align="end"
                alignItemWithTrigger={false}
                className="max-h-72"
              >
                {availableRuns.map((run) => (
                  <SelectItem key={run.id} value={run.id}>
                    <Route className="size-3.5 text-muted-foreground" />
                    <span className="truncate">{run.title}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            )}
            {selectedRun && (
              <Badge variant="secondary">{selectedRun.status}</Badge>
            )}
            <Button
              size="sm"
              disabled={!selectedRun?.stages?.length}
              onClick={openStageTask}
            >
              <Plus className="size-3.5" />
              {t(($) => $.project_tasks.create_task)}
            </Button>
          </div>
        )}
      </div>

      {effectiveMode === "lifecycle" ? (
        <IssueSurface
          scope={{ type: "project", projectId }}
          modes={["board", "list", "table", "swimlane", "gantt"]}
        />
      ) : selectedRun ? (
        <div className="flex min-h-0 flex-1 flex-col overflow-hidden px-4 py-3">
          <WorkflowBoard instance={selectedRun} mode="stage" />
        </div>
      ) : (
        <div className="flex flex-1 items-center justify-center p-6">
          <div className="max-w-sm text-center">
            <div className="mx-auto flex size-12 items-center justify-center rounded-xl bg-brand/10 text-brand">
              <GitBranch className="size-5" />
            </div>
            <h3 className="mt-4 text-title-sm font-semibold">
              {t(($) => $.project_tasks.empty_title)}
            </h3>
            <p className="mt-1.5 text-body leading-6 text-muted-foreground">
              {t(($) => $.project_tasks.empty_description)}
            </p>
            <Button
              size="sm"
              variant="outline"
              className="mt-4"
              onClick={() =>
                navigation.replace(
                  workspacePaths.projectSop(projectId),
                )
              }
            >
              <GitBranch className="size-3.5" />
              {t(($) => $.project_tasks.open_sop)}
            </Button>
          </div>
        </div>
      )}

    </div>
  );
}
