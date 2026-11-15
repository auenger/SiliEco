"use client";

import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@silieco/ui/components/ui/alert-dialog";
import { useDeleteTask } from "@silieco/core/tasks/mutations";
import { useBackOrReplace } from "../navigation";
import { useT } from "../i18n";

export function DeleteTaskConfirmModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const taskId = (data?.taskId as string) || "";
  // Set only by callers that are rendering the task we are about to delete
  // (the detail page). List surfaces leave it undefined and simply stay put.
  const fallbackPath = (data?.onDeletedFallbackPath as string | undefined) || undefined;
  const [deleting, setDeleting] = useState(false);
  const deleteTask = useDeleteTask();
  const backOrReplace = useBackOrReplace();

  const handleDelete = async () => {
    if (!taskId) return;
    setDeleting(true);
    try {
      await deleteTask.mutateAsync(taskId);
      toast.success(t(($) => $.delete_task.toast_deleted));
      onClose();
      // Back to whichever list the user opened this task from; `fallbackPath`
      // only kicks in when there is no in-app history to step back into.
      if (fallbackPath) backOrReplace(fallbackPath);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.delete_task.toast_delete_failed),
      );
      setDeleting(false);
    }
  };

  return (
    <AlertDialog open onOpenChange={(v) => { if (!v && !deleting) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.delete_task.title)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.delete_task.description)}
            <span className="mt-2 block text-caption text-muted-foreground/80">
              {t(($) => $.delete_task.hint)}
            </span>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={deleting}>{t(($) => $.delete_task.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleDelete}
            disabled={deleting}
            className="bg-destructive text-white hover:bg-destructive/90"
          >
            {deleting ? t(($) => $.delete_task.deleting) : t(($) => $.delete_task.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
