"use client";

import { TasksPage } from "@silieco/views/tasks/components";
import { ErrorBoundary } from "@silieco/ui/components/common/error-boundary";

export default function TasksRoute() {
  return (
    <ErrorBoundary>
      <TasksPage />
    </ErrorBoundary>
  );
}
