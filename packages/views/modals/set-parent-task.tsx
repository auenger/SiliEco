"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@silieco/core/hooks";
import { childTasksOptions } from "@silieco/core/tasks/queries";
import { useUpdateTask } from "@silieco/core/tasks/mutations";
import { isRevisionConflictError } from "@silieco/core/revisions";
import { TaskPickerModal } from "./task-picker-modal";
import { useT } from "../i18n";

export function SetParentTaskModal({
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

  const { data: children = [] } = useQuery({
    ...childTasksOptions(wsId, taskId),
    enabled: !!taskId,
  });

  const excludeIds = [taskId, ...children.map((c) => c.id)];

  return (
    <TaskPickerModal
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      title={t(($) => $.set_parent.title)}
      description={t(($) => $.set_parent.description)}
      excludeIds={excludeIds}
      onSelect={(selected) => {
        updateTask.mutate(
          { id: taskId, parent_task_id: selected.id },
          {
            onError: (err) => {
              if (!isRevisionConflictError(err)) {
                toast.error(t(($) => $.set_parent.toast_failed));
              }
            },
          },
        );
        toast.success(t(($) => $.set_parent.toast_success, { identifier: selected.identifier }));
      }}
    />
  );
}
