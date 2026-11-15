"use client";

import { useMemo } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api } from "@silieco/core/api";
import { taskKeys } from "@silieco/core/tasks/queries";
import type { TaskAssigneeType, TaskStatus, TaskTriggerPreviewItem } from "@silieco/core/types";

export interface UseTaskTriggerPreviewParams {
  /** Existing tasks to evaluate (single assign/status or batch). */
  taskIds?: string[];
  /** Preview a not-yet-persisted task from assignee/status (create modal). */
  isCreate?: boolean;
  assigneeType?: TaskAssigneeType | null;
  assigneeId?: string | null;
  status?: TaskStatus;
  /** Caller gate — e.g. only fetch while a picker/modal is open. */
  enabled?: boolean;
}

export interface UseTaskTriggerPreviewResult {
  triggers: TaskTriggerPreviewItem[];
  totalCount: number;
  isLoading: boolean;
  /** True when every trigger's target runtime can render a handoff note, so
   *  the note box is safe to enable. False if any started run would drop it. */
  handoffSupported: boolean;
}

const EMPTY: TaskTriggerPreviewItem[] = [];

function previewSignature(params: UseTaskTriggerPreviewParams): string {
  return JSON.stringify({
    ids: [...(params.taskIds ?? [])].sort(),
    create: params.isCreate ?? false,
    at: params.assigneeType ?? null,
    aid: params.assigneeId ?? null,
    status: params.status ?? null,
  });
}

/** Reads the unified backend predicate via POST /api/tasks/preview-trigger so
 *  the four entry points never re-implement "will this start a run" (SILI-3375).
 *
 *  The verdict changes only with the inputs (assignee / status), so the query
 *  refetches solely on signature change — it is deliberately NOT invalidated by
 *  WS task events. The assign source (create / assignee change) cancels existing
 *  tasks before enqueuing, so its verdict can't shift from a task event at all;
 *  the status source's pending dedup could, but the preview is advisory and the
 *  write path re-evaluates authoritatively, so a rare stale status label is
 *  harmless — far better than refetching every mounted preview on every
 *  workspace task event (the source of the visible flicker, SILI-3375).
 *
 *  Mirrors the comment-trigger preview's data handling: keepPreviousData so an
 *  input switch swaps the answer in place instead of collapsing, and only the
 *  very first load (no prior data) counts as loading. */
export function useTaskTriggerPreview(
  params: UseTaskTriggerPreviewParams,
): UseTaskTriggerPreviewResult {
  const hasTarget =
    (!!params.assigneeType && !!params.assigneeId) ||
    !!params.status ||
    (params.isCreate ?? false);
  const enabled = (params.enabled ?? true) && hasTarget;

  const signature = useMemo(() => previewSignature(params), [params]);

  const previewQuery = useQuery({
    queryKey: taskKeys.taskTriggerPreview(signature),
    queryFn: () =>
      api.previewTaskTrigger({
        taskIds: params.taskIds,
        isCreate: params.isCreate,
        assigneeType: params.assigneeType,
        assigneeId: params.assigneeId,
        status: params.status,
      }),
    enabled,
    retry: false,
    staleTime: 0,
    // Keep the prior verdict visible while a new signature (assignee/status
    // switch) refetches, so the hint swaps in place rather than collapsing.
    placeholderData: keepPreviousData,
  });

  const triggers = previewQuery.data?.triggers ?? EMPTY;
  return {
    triggers,
    totalCount: previewQuery.data?.total_count ?? 0,
    // Only the first load (no prior data) is "loading"; a background/placeholder
    // refetch is not, so reveal animations gated on this never collapse mid-fetch.
    isLoading: enabled && previewQuery.isLoading,
    handoffSupported: triggers.length > 0 && triggers.every((t) => t.handoff_supported === true),
  };
}
