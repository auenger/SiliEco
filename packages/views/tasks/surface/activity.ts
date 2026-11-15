"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { agentRunSnapshotOptions } from "@silieco/core/agents";
import { useWorkspaceId } from "@silieco/core/hooks";
import type { AgentRun } from "@silieco/core/types";

export interface TaskActivityState {
  isWorking: boolean;
  isQueued: boolean;
  runningTasks: AgentRun[];
  queuedTasks: AgentRun[];
}

export interface TaskSurfaceActivity {
  activityByTaskId: Map<string, TaskActivityState>;
  runningTaskIds: Set<string>;
}

function isQueuedTaskStatus(status: AgentRun["status"]) {
  return (
    status === "queued" ||
    status === "dispatched" ||
    status === "waiting_local_directory"
  );
}

export interface TaskRunGroups {
  running: AgentRun[];
  queued: AgentRun[];
}

/**
 * Per-task slice of the workspace-wide agent task snapshot. Used as the
 * `select` for a row-level `useQuery(agentRunSnapshotOptions)`: every row
 * still observes the one shared snapshot query, but React Query's structural
 * sharing keeps this returned object referentially stable when *this* task's
 * tasks are unchanged, so a snapshot invalidation only re-renders the rows
 * whose own tasks actually moved — not the whole list. Terminal statuses are
 * dropped (they belong on task history, not the live indicator).
 */
export function selectTaskRuns(
  snapshot: readonly AgentRun[],
  taskId: string,
): TaskRunGroups {
  const running: AgentRun[] = [];
  const queued: AgentRun[] = [];
  for (const task of snapshot) {
    if (task.task_id !== taskId) continue;
    if (task.status === "running") running.push(task);
    else if (isQueuedTaskStatus(task.status)) queued.push(task);
  }
  return { running, queued };
}

export function deriveTaskSurfaceActivity(
  tasks: readonly AgentRun[],
): TaskSurfaceActivity {
  const activityByTaskId = new Map<string, TaskActivityState>();

  for (const task of tasks) {
    if (!task.task_id) continue;
    if (task.status !== "running" && !isQueuedTaskStatus(task.status)) {
      continue;
    }

    const current =
      activityByTaskId.get(task.task_id) ?? {
        isWorking: false,
        isQueued: false,
        runningTasks: [],
        queuedTasks: [],
      };

    if (task.status === "running") {
      current.runningTasks.push(task);
      current.isWorking = true;
    } else {
      current.queuedTasks.push(task);
      current.isQueued = true;
    }

    activityByTaskId.set(task.task_id, current);
  }

  const runningTaskIds = new Set<string>();
  for (const [taskId, activity] of activityByTaskId) {
    if (activity.isWorking) runningTaskIds.add(taskId);
  }

  return { activityByTaskId, runningTaskIds };
}

export function useTaskSurfaceActivity(): TaskSurfaceActivity {
  const wsId = useWorkspaceId();
  const { data: snapshot = [] } = useQuery(agentRunSnapshotOptions(wsId));
  return useMemo(() => deriveTaskSurfaceActivity(snapshot), [snapshot]);
}
