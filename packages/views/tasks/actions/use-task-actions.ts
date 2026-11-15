"use client";

import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Task, UpdateTaskRequest } from "@silieco/core/types";
import { useAuthStore } from "@silieco/core/auth";
import { useWorkspaceId } from "@silieco/core/hooks";
import { useWorkspacePaths } from "@silieco/core/paths";
import { useModalStore } from "@silieco/core/modals";
import { isRevisionConflictError } from "@silieco/core/revisions";
import { useUpdateTask } from "@silieco/core/tasks/mutations";
import { pinListOptions, useCreatePin, useDeletePin } from "@silieco/core/pins";
import { copyText } from "@silieco/ui/lib/clipboard";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { useTaskSurfaceActionsOptional } from "../surface/actions-context";

export interface UseTaskActionsResult {
  isPinned: boolean;
  updateField: (updates: Partial<UpdateTaskRequest>) => void;
  openInNewTab: () => void;
  togglePin: () => void;
  copyLink: () => Promise<void>;
  openCreateSubTask: () => void;
  openSetParent: () => void;
  removeParent: () => void;
  openAddChild: () => void;
  openDeleteConfirm: (opts?: { onDeletedFallbackPath?: string }) => void;
}

/**
 * Accepts a nullable task so callers can invoke the hook before they've
 * early-returned on a missing task. Returned handlers are safe no-ops when
 * `task` is null.
 */
export function useTaskActions(task: Task | null): UseTaskActionsResult {
  const { t } = useT("tasks");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const user = useAuthStore((s) => s.user);
  const userId = user?.id;

  const { data: pinnedItems = [] } = useQuery({
    ...pinListOptions(wsId, userId ?? ""),
    enabled: !!userId,
  });

  const isPinned =
    !!task &&
    pinnedItems.some(
      (p) => p.item_type === "task" && p.item_id === task.id,
    );

  const updateTask = useUpdateTask();
  const surfaceActions = useTaskSurfaceActionsOptional();
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const openModal = useModalStore((s) => s.open);

  const taskId = task?.id ?? null;
  const taskIdentifier = task?.identifier ?? null;
  const taskProjectId = task?.project_id ?? null;
  const taskStatus = task?.status ?? null;

  const updateField = useCallback(
    (updates: Partial<UpdateTaskRequest>) => {
      if (!taskId) return;
      // Assigning to an agent/squad may start a run. Route through the
      // pre-trigger confirm modal (preview + optional handoff note + "暂不开始"),
      // which applies the change itself — the four entry points share this one
      // backend-driven flow instead of guessing (SILI-3375). Every other field
      // change (status, priority, member assign, unassign) applies directly.
      //
      // Backlog is the parking lot: assigning a backlog task never starts a run
      // (core/internal/service/task_trigger.go), so the modal would only show
      // an empty "won't start" box with a single Apply button. Apply directly,
      // matching the batch backlog short-circuit in BatchActionToolbar.
      if (
        (updates.assignee_type === "agent" || updates.assignee_type === "squad") &&
        updates.assignee_id &&
        taskStatus !== "backlog"
      ) {
        openModal("task-run-confirm", {
          taskIds: [taskId],
          mode: "assign",
          assigneeType: updates.assignee_type,
          assigneeId: updates.assignee_id,
        });
        return;
      }
      if (surfaceActions) {
        surfaceActions.updateTask(taskId, updates, {
          errorMessage: t(($) => $.detail.update_failed),
        });
      } else {
        updateTask.mutate(
          { id: taskId, ...updates },
          {
            onError: (err) => {
              if (!isRevisionConflictError(err)) {
                toast.error(t(($) => $.detail.update_failed));
              }
            },
          },
        );
      }
    },
    [taskId, taskStatus, surfaceActions, updateTask, openModal, t],
  );

  // Explicit "open it somewhere else" CTA, so the new tab takes focus
  // (`activate: true`) — the user is asking to move into the new context, not
  // to stash it for later the way modifier-click does. Same contract as the
  // table row open and the attachment preview's "Open in new tab".
  //
  // Only desktop implements `openInNewTab`; on web it is undefined and we fall
  // back to a real browser tab via the shareable URL.
  const openInNewTab = useCallback(() => {
    if (!taskId) return;
    const path = paths.taskDetail(taskId);
    if (navigation.openInNewTab) {
      navigation.openInNewTab(path, taskIdentifier ?? undefined, {
        activate: true,
      });
      return;
    }
    window.open(
      navigation.getShareableUrl(path),
      "_blank",
      "noopener,noreferrer",
    );
  }, [taskId, taskIdentifier, navigation, paths]);

  const togglePin = useCallback(() => {
    if (!taskId) return;
    if (isPinned) {
      deletePin.mutate({ itemType: "task", itemId: taskId });
    } else {
      createPin.mutate({ item_type: "task", item_id: taskId });
    }
  }, [isPinned, taskId, createPin, deletePin]);

  const copyLink = useCallback(async () => {
    if (!taskId) return;
    const url = navigation.getShareableUrl(paths.taskDetail(taskId));
    if (await copyText(url)) {
      toast.success(t(($) => $.detail.link_copied));
    } else {
      toast.error(t(($) => $.detail.link_copy_failed));
    }
  }, [paths, taskId, navigation, t]);

  const openCreateSubTask = useCallback(() => {
    if (!taskId) return;
    openModal("create-task", {
      parent_task_id: taskId,
      parent_task_identifier: taskIdentifier,
      ...(taskProjectId ? { project_id: taskProjectId } : {}),
    });
  }, [openModal, taskId, taskIdentifier, taskProjectId]);

  const openSetParent = useCallback(() => {
    if (!taskId) return;
    openModal("task-set-parent", { taskId });
  }, [openModal, taskId]);

  // Detach from the parent and promote to a standalone task. Reversible
  // (Set parent re-links it), non-destructive, and mirrors the clear-date
  // actions — so it applies directly instead of a confirm modal. `stage`
  // only orders sub-tasks under a parent, so clear it in the same write to
  // avoid an orphaned value on a standalone task. The success toast fires
  // from onSuccess, not eagerly after mutate() — otherwise a request that
  // fails on permission/network/validation would flash "removed" before the
  // error toast and the optimistic rollback (false confirmation).
  const removeParent = useCallback(() => {
    if (!taskId) return;
    if (surfaceActions) {
      surfaceActions.updateTask(
        taskId,
        { parent_task_id: null, stage: null },
        {
          onSuccess: () =>
            toast.success(t(($) => $.actions.remove_parent_task_success)),
          errorMessage: t(($) => $.detail.update_failed),
        },
      );
    } else {
      updateTask.mutate(
        { id: taskId, parent_task_id: null, stage: null },
        {
          onSuccess: () =>
            toast.success(t(($) => $.actions.remove_parent_task_success)),
          onError: (err) => {
            if (!isRevisionConflictError(err)) {
              toast.error(t(($) => $.detail.update_failed));
            }
          },
        },
      );
    }
  }, [taskId, surfaceActions, updateTask, t]);

  const openAddChild = useCallback(() => {
    if (!taskId) return;
    openModal("task-add-child", { taskId });
  }, [openModal, taskId]);

  const openDeleteConfirm = useCallback(
    (opts?: { onDeletedFallbackPath?: string }) => {
      if (!taskId) return;
      openModal("task-delete-confirm", {
        taskId,
        identifier: taskIdentifier,
        onDeletedFallbackPath: opts?.onDeletedFallbackPath,
      });
    },
    [openModal, taskId, taskIdentifier],
  );

  return {
    isPinned,
    updateField,
    openInNewTab,
    togglePin,
    copyLink,
    openCreateSubTask,
    openSetParent,
    removeParent,
    openAddChild,
    openDeleteConfirm,
  };
}
