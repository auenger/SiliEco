import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { TaskDetail } from "@silieco/views/tasks/components";
import { useWorkspaceId } from "@silieco/core/hooks";
import { taskDetailOptions } from "@silieco/core/tasks/queries";
import { useDocumentTitle } from "@/hooks/use-document-title";

export function TaskDetailPage({ onDelete }: { onDelete?: () => void }) {
  const { id } = useParams<{ id: string }>();
  const wsId = useWorkspaceId();
  const { data: task } = useQuery(taskDetailOptions(wsId, id!));

  useDocumentTitle(task ? `${task.identifier}: ${task.title}` : "Task");

  if (!id) return null;
  // Render errors bubble to the root route errorElement (DesktopRouteErrorPage),
  // which contains the crash inside the tab content pane. No page-level boundary
  // here — a whole-page wrapper duplicates the route-level error UI.
  return <TaskDetail taskId={id} onDelete={onDelete} />;
}
