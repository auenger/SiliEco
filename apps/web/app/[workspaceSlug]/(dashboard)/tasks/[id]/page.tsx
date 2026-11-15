"use client";

import { use } from "react";
import { TaskDetail } from "@silieco/views/tasks/components";
import { ErrorBoundary } from "@silieco/ui/components/common/error-boundary";

export default function TaskDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return (
    <ErrorBoundary resetKeys={[id]}>
      <TaskDetail taskId={id} />
    </ErrorBoundary>
  );
}
