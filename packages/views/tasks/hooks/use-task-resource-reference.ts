"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@silieco/core/api";
import { getCurrentWsId } from "@silieco/core/platform";
import { taskKeys } from "@silieco/core/tasks/queries";
import { fileResourceKeys } from "@silieco/core/resources";
import type { FileResource } from "@silieco/core/types";
import { useT } from "../../i18n";

/** Persist the Task edge behind a file inserted from the editor's `@` picker. */
export function useTaskResourceReference(taskId: string) {
  const queryClient = useQueryClient();
  const { t } = useT("resources");

  return useCallback((file: FileResource) => {
    void api.createAttachmentReference("task", taskId, file.id)
      .then(() => {
        const wsId = getCurrentWsId();
        if (wsId) {
          void queryClient.invalidateQueries({ queryKey: fileResourceKeys.all(wsId) });
        }
        void queryClient.invalidateQueries({ queryKey: taskKeys.attachments(taskId) });
      })
      .catch((error: unknown) => {
        toast.error(error instanceof Error ? error.message : t(($) => $.reference_failed));
      });
  }, [taskId, queryClient, t]);
}
