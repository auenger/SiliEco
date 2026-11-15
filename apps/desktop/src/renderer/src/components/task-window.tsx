import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  createMemoryRouter,
  RouterProvider,
  useParams,
  useRouteError,
} from "react-router-dom";
import { AlertTriangle, RotateCw, X } from "lucide-react";
import { useAuthStore } from "@silieco/core/auth";
import { setCurrentWorkspace } from "@silieco/core/platform";
import { WorkspaceSlugProvider } from "@silieco/core/paths";
import { workspaceBySlugOptions } from "@silieco/core/workspace";
import { Button } from "@silieco/ui/components/ui/button";
import { SiliecoIcon } from "@silieco/ui/components/common/silieco-icon";
import { ModalRegistry } from "@silieco/views/modals/registry";
import { WorkspacePresencePrefetch } from "@silieco/views/layout";
import { DragStrip } from "@silieco/views/platform";
import type { TaskWindowContext } from "../../../shared/task-window";
import { TaskDetailPage } from "../pages/task-detail-page";
import { TaskWindowNavigationProvider } from "../platform/task-window-navigation";

export function TaskWindow({ context }: { context: TaskWindowContext }) {
  const router = useMemo(
    () =>
      createMemoryRouter(
        [
          {
            path: ":workspaceSlug/tasks/:id",
            element: <TaskWindowRoute />,
            errorElement: <TaskWindowRouteError />,
          },
        ],
        { initialEntries: [context.path] },
      ),
    [context.path],
  );

  return <RouterProvider router={router} />;
}

function TaskWindowRoute() {
  const { workspaceSlug } = useParams<{ workspaceSlug: string }>();
  const user = useAuthStore((state) => state.user);
  const { data: workspace, isFetched } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug ?? ""),
    enabled: !!user && !!workspaceSlug,
  });

  if (workspace && workspaceSlug) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
  }

  if (!isFetched) {
    return (
      <TaskWindowFrame>
        <div className="flex min-h-0 flex-1 items-center justify-center">
          <SiliecoIcon className="size-6 animate-pulse" />
        </div>
      </TaskWindowFrame>
    );
  }

  if (!workspace || !workspaceSlug) {
    return <TaskWindowUnavailable />;
  }

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      <TaskWindowNavigationProvider>
        <WorkspacePresencePrefetch />
        <TaskWindowFrame>
          <TaskDetailPage onDelete={() => window.desktopAPI.closeWindow()} />
        </TaskWindowFrame>
        <ModalRegistry />
      </TaskWindowNavigationProvider>
    </WorkspaceSlugProvider>
  );
}

function TaskWindowFrame({ children }: { children: React.ReactNode }) {
  return (
    <div
      data-dedicated-task-window="true"
      className="flex h-screen min-h-0 flex-col bg-page-canvas text-foreground"
    >
      <DragStrip />
      <div className="flex min-h-0 flex-1 overflow-hidden">{children}</div>
    </div>
  );
}

function TaskWindowUnavailable() {
  return (
    <TaskWindowFrame>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-4 p-8 text-center">
        <div className="rounded-full bg-muted p-3 text-muted-foreground">
          <AlertTriangle className="size-6" aria-hidden="true" />
        </div>
        <div className="space-y-1">
          <h1 className="text-title font-semibold">Task unavailable</h1>
          <p className="text-body text-muted-foreground">
            This workspace is no longer available in your account.
          </p>
        </div>
        <Button variant="outline" onClick={() => window.desktopAPI.closeWindow()}>
          Close window
        </Button>
      </div>
    </TaskWindowFrame>
  );
}

function TaskWindowRouteError() {
  const error = useRouteError();
  const message = error instanceof Error ? error.message : "Unknown route error";

  return (
    <TaskWindowFrame>
      <div
        role="alert"
        className="flex min-h-0 flex-1 flex-col items-center justify-center gap-4 p-8 text-center"
      >
        <div className="rounded-full bg-destructive/10 p-3 text-destructive">
          <AlertTriangle className="size-6" aria-hidden="true" />
        </div>
        <div className="space-y-1">
          <h1 className="text-title font-semibold">Something went wrong</h1>
          <p className="max-w-lg truncate text-body text-muted-foreground">
            {message}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => window.location.reload()}>
            <RotateCw className="size-4" aria-hidden="true" />
            Reload
          </Button>
          <Button variant="outline" onClick={() => window.desktopAPI.closeWindow()}>
            <X className="size-4" aria-hidden="true" />
            Close window
          </Button>
        </div>
      </div>
    </TaskWindowFrame>
  );
}
