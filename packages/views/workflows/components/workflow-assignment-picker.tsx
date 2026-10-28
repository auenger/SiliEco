"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, GitBranch, Milestone, Route } from "lucide-react";
import { useWorkspaceId } from "@silieco/core/hooks";
import {
  workflowInstanceOptions,
  workflowInstancesOptions,
  workflowListOptions,
  useCreateWorkflowInstance,
} from "@silieco/core/workflows";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@silieco/ui/components/ui/dropdown-menu";
import { PillButton } from "../../common/pill-button";
import { useT } from "../../i18n";

export interface WorkflowAssignment {
  workflowInstanceId?: string;
  workflowStageId?: string;
}

export function WorkflowAssignmentPicker({
  projectId,
  workflowInstanceId,
  workflowStageId,
  onChange,
}: {
  projectId?: string;
  workflowInstanceId?: string;
  workflowStageId?: string;
  onChange: (assignment: WorkflowAssignment) => void;
}) {
  const { t } = useT("workflows");
  const workspaceId = useWorkspaceId();
  const createInstance = useCreateWorkflowInstance();
  const [workflowId, setWorkflowId] = useState<string>();
  const { data: workflows = [] } = useQuery({
    ...workflowListOptions(workspaceId, projectId),
    enabled: Boolean(projectId),
  });
  const { data: projectRuns = [] } = useQuery({
    ...workflowInstancesOptions(workspaceId, undefined, projectId),
    enabled: Boolean(projectId),
  });
  const assignableRuns = useMemo(
    () =>
      projectRuns.filter(
        (run) =>
          !run.archived_at &&
          (run.status !== "completed" || run.accepts_new_tasks === true) &&
          run.status !== "cancelled",
      ),
    [projectRuns],
  );
  const selectedRun = assignableRuns.find(
    (run) => run.id === workflowInstanceId,
  );
  const selectedWorkflow = workflows.find(
    (workflow) => workflow.id === workflowId,
  );
  const workflowRuns = assignableRuns.filter(
    (run) => run.workflow_id === workflowId,
  );
  const { data: runDetail } = useQuery({
    ...workflowInstanceOptions(workspaceId, workflowInstanceId ?? ""),
    enabled: Boolean(workflowInstanceId),
  });
  const stages = useMemo(() => runDetail?.stages ?? [], [runDetail?.stages]);
  const selectedStage = stages.find((stage) => stage.id === workflowStageId);

  useEffect(() => {
    if (selectedRun && selectedRun.workflow_id !== workflowId) {
      setWorkflowId(selectedRun.workflow_id);
    }
  }, [selectedRun, workflowId]);

  useEffect(() => {
    if (!workflowInstanceId || stages.length === 0) return;
    if (stages.some((stage) => stage.id === workflowStageId)) return;
    const defaultStage =
      stages.find((stage) => runDetail?.status !== "completed" && stage.id === runDetail?.current_stage_id) ??
      stages[0];
    if (defaultStage) {
      onChange({
        workflowInstanceId,
        workflowStageId: defaultStage.id,
      });
    }
  }, [
    onChange,
    runDetail?.current_stage_id,
    runDetail?.status,
    stages,
    workflowInstanceId,
    workflowStageId,
  ]);

  useEffect(() => {
    if (projectId) return;
    setWorkflowId(undefined);
    if (workflowInstanceId || workflowStageId) onChange({});
  }, [onChange, projectId, workflowInstanceId, workflowStageId]);

  const selectWorkflow = (nextWorkflowId?: string) => {
    setWorkflowId(nextWorkflowId);
    if (!nextWorkflowId) {
      onChange({});
      return;
    }
    const runs = assignableRuns.filter(
      (run) => run.workflow_id === nextWorkflowId,
    );
    if (runs[0]) {
      onChange({
        workflowInstanceId: runs[0].id,
        workflowStageId: undefined,
      });
      return;
    }

    const workflow = workflows.find((item) => item.id === nextWorkflowId);
    if (!workflow || !projectId) return;
    // A Project SOP should be directly selectable from Task creation. The
    // runtime instance is an implementation detail, so create its first run
    // lazily instead of forcing the user back into a separate Run screen.
    createInstance.mutate(
      {
        workflowId: workflow.id,
        data: {
          title: t(($) => $.designer.run_default_name, {
            name: workflow.name,
            sequence:
              projectRuns.filter(
                (run) => run.workflow_id === workflow.id,
              ).length + 1,
          }),
          project_id: projectId,
          start: true,
        },
      },
      {
        onSuccess: (created) =>
          onChange({
            workflowInstanceId: created.id,
            workflowStageId: undefined,
          }),
        onError: () => setWorkflowId(undefined),
      },
    );
  };

  if (!projectId || workflows.length === 0) return null;

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <PillButton>
              <GitBranch className="size-3.5 text-brand" />
              <span className="max-w-36 truncate">
                {selectedWorkflow
                  ? `SOP · ${selectedWorkflow.name}`
                  : t(($) => $.assignment.link_sop)}
              </span>
            </PillButton>
          }
        />
        <DropdownMenuContent align="start" className="w-64">
          <DropdownMenuGroup>
            <DropdownMenuLabel>
              {t(($) => $.assignment.management)}
            </DropdownMenuLabel>
            <DropdownMenuItem onClick={() => selectWorkflow(undefined)}>
              <Route className="size-3.5" />
              <div className="min-w-0 flex-1">
                <div>{t(($) => $.assignment.lifecycle)}</div>
                <div className="text-micro text-muted-foreground">
                  {t(($) => $.assignment.lifecycle_hint)}
                </div>
              </div>
              {!selectedWorkflow && <Check className="size-3.5" />}
            </DropdownMenuItem>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            {workflows.map((workflow) => {
              const runCount = assignableRuns.filter(
                (run) => run.workflow_id === workflow.id,
              ).length;
              return (
                <DropdownMenuItem
                  key={workflow.id}
                  disabled={createInstance.isPending}
                  onClick={() => selectWorkflow(workflow.id)}
                >
                  <GitBranch className="size-3.5" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate">{workflow.name}</div>
                    <div className="text-micro text-muted-foreground">
                      {runCount > 0
                        ? t(($) => $.assignment.run_count, { count: runCount })
                        : t(($) => $.assignment.no_run)}
                    </div>
                  </div>
                  {workflow.id === workflowId && (
                    <Check className="size-3.5" />
                  )}
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>

      {selectedWorkflow && workflowRuns.length > 1 && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <PillButton>
                <Route className="size-3.5 text-muted-foreground" />
                <span className="max-w-32 truncate">
                  {selectedRun?.title ?? t(($) => $.assignment.select_run)}
                </span>
              </PillButton>
            }
          />
          <DropdownMenuContent align="start" className="w-60">
            <DropdownMenuGroup>
              <DropdownMenuLabel>
                {t(($) => $.assignment.run)}
              </DropdownMenuLabel>
              {workflowRuns.map((run) => (
                <DropdownMenuItem
                  key={run.id}
                  onClick={() =>
                    onChange({
                      workflowInstanceId: run.id,
                      workflowStageId: undefined,
                    })
                  }
                >
                  <Route className="size-3.5" />
                  <span className="min-w-0 flex-1 truncate">{run.title}</span>
                  {run.id === workflowInstanceId && (
                    <Check className="size-3.5" />
                  )}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      )}

      {workflowInstanceId && stages.length > 0 && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <PillButton>
                <Milestone className="size-3.5 text-muted-foreground" />
                <span className="max-w-32 truncate">
                  {selectedStage
                    ? `Stage · ${selectedStage.name}`
                    : t(($) => $.assignment.select_stage)}
                </span>
              </PillButton>
            }
          />
          <DropdownMenuContent align="start" className="w-60">
            <DropdownMenuGroup>
              <DropdownMenuLabel>
                {t(($) => $.assignment.task_stage)}
              </DropdownMenuLabel>
              {stages.map((stage, index) => (
                <DropdownMenuItem
                  key={stage.id}
                  onClick={() =>
                    onChange({
                      workflowInstanceId,
                      workflowStageId: stage.id,
                    })
                  }
                >
                  <span className="flex size-5 items-center justify-center rounded-full bg-muted text-micro font-semibold">
                    {index + 1}
                  </span>
                  <span className="min-w-0 flex-1 truncate">{stage.name}</span>
                  {stage.id === workflowStageId && (
                    <Check className="size-3.5" />
                  )}
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </>
  );
}
