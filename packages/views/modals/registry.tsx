"use client";

import { useModalStore } from "@silieco/core/modals";
import { CreateWorkspaceModal } from "./create-workspace";
import { CreateTaskDialog } from "./create-task-dialog";
import { CreateProjectModal } from "./create-project";
import { CreateSquadModal } from "./create-squad";
import { FeedbackModal } from "./feedback";
import { SetParentTaskModal } from "./set-parent-task";
import { AddChildTaskModal } from "./add-child-task";
import { DeleteTaskConfirmModal } from "./delete-task-confirm";
import { RunConfirmModal } from "./run-confirm";

export function ModalRegistry() {
  const modal = useModalStore((s) => s.modal);
  const data = useModalStore((s) => s.data);
  const close = useModalStore((s) => s.close);

  switch (modal) {
    case "create-workspace":
      return <CreateWorkspaceModal onClose={close} />;
    // Both modal types open the same shell so the in-modal mode switch is
    // instant — only the inner panel swaps, the Dialog Root stays mounted.
    case "create-task":
      return <CreateTaskDialog onClose={close} initialMode="manual" data={data} />;
    case "quick-create-task":
      return <CreateTaskDialog onClose={close} initialMode="agent" data={data} />;
    case "create-project":
      return <CreateProjectModal onClose={close} />;
    case "create-squad":
      return <CreateSquadModal onClose={close} />;
    case "feedback":
      return <FeedbackModal onClose={close} data={data} />;
    case "task-set-parent":
      return <SetParentTaskModal onClose={close} data={data} />;
    case "task-add-child":
      return <AddChildTaskModal onClose={close} data={data} />;
    case "task-delete-confirm":
      return <DeleteTaskConfirmModal onClose={close} data={data} />;
    case "task-run-confirm":
      return <RunConfirmModal onClose={close} data={data} />;
    default:
      return null;
  }
}
