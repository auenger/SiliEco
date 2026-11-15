"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@silieco/core/hooks";
import {
  taskDetailOptions,
  childTasksOptions,
} from "@silieco/core/tasks/queries";
import { useUpdateTask } from "@silieco/core/tasks/mutations";
import { isRevisionConflictError } from "@silieco/core/revisions";
import { TaskPickerModal } from "./task-picker-modal";
import { useT } from "../i18n";

export function AddChildTaskModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const taskId = (data?.taskId as string) || "";
  const wsId = useWorkspaceId();
  const updateTask = useUpdateTask();

  const { data: task = null } = useQuery({
    ...taskDetailOptions(wsId, taskId),
    enabled: !!taskId,
  });
  const { data: children = [] } = useQuery({
    ...childTasksOptions(wsId, taskId),
    enabled: !!taskId,
  });

  const excludeIds = [
    taskId,
    ...(task?.parent_task_id ? [task.parent_task_id] : []),
    ...children.map((c) => c.id),
  ];

  return (
    <TaskPickerModal
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      title={t(($) => $.add_child.title)}
      description={t(($) => $.add_child.description)}
      excludeIds={excludeIds}
      onSelect={(selected) => {
        updateTask.mutate(
          { id: selected.id, parent_task_id: taskId },
          {
            onError: (err) => {
              if (!isRevisionConflictError(err)) {
                toast.error(t(($) => $.add_child.toast_failed));
              }
            },
          },
        );
        toast.success(t(($) => $.add_child.toast_success, { identifier: selected.identifier }));
      }}
    />
  );
}
