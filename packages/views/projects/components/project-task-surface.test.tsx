import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import enWorkflows from "../../locales/en/workflows.json";
import { ProjectTaskSurface } from "./project-task-surface";

const { openModal, runsByProject, transitionInstance } = vi.hoisted(() => ({
  openModal: vi.fn(),
  runsByProject: {
    current: new Map<string, Array<Record<string, unknown>>>(),
  },
  transitionInstance: vi.fn(),
}));

vi.mock("@silieco/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "owner-1" } }),
}));

vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@silieco/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: openModal }),
  },
}));

vi.mock("@silieco/core/paths", () => ({
  useWorkspacePaths: () => ({
    projectDetail: (projectId: string) => `/projects/${projectId}`,
    projectSop: (projectId: string) => `/projects/${projectId}?section=sop`,
  }),
}));

vi.mock("@silieco/core/workflows", () => ({
  workflowInstancesOptions: (
    _workspaceId: string,
    _workflowId: string | undefined,
    projectId: string,
  ) => ({ queryKey: ["workflow-runs", projectId] }),
  workflowInstanceOptions: (_workspaceId: string, runId: string) => ({
    queryKey: ["workflow-run", runId],
  }),
  useTransitionWorkflowInstance: () => ({
    isPending: false,
    mutate: transitionInstance,
  }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === "workflow-runs") {
      return { data: runsByProject.current.get(queryKey[1]!) ?? [] };
    }

    return {
      data: queryKey[1]
        ? {
            id: queryKey[1],
            title: "Release SOP · Run 1",
            status: [...runsByProject.current.values()]
              .flat()
              .find((run) => run.id === queryKey[1])?.status ?? "active",
            accepts_new_tasks: [...runsByProject.current.values()].flat().find((run) => run.id === queryKey[1])?.accepts_new_tasks,
            current_stage_id: "stage-2",
            stages: [{ id: "stage-1", name: "Planning" }, { id: "stage-2", name: "Review" }],
          }
        : undefined,
    };
  },
}));

vi.mock("../../issues/surface/issue-surface", () => ({
  IssueSurface: ({ scope }: { scope: { projectId: string } }) => (
    <div data-testid="project-task-board">{scope.projectId}</div>
  ),
}));

vi.mock("../../workflows/components", () => ({
  WorkflowBoard: ({ instance }: { instance: { id: string } }) => (
    <div data-testid="workflow-board">{instance.id}</div>
  ),
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ replace: vi.fn() }),
}));

function surface(projectId: string, workflowRunId?: string) {
  return (
    <I18nProvider locale="en" resources={{ en: { workflows: enWorkflows } }}>
      <ProjectTaskSurface
        projectId={projectId}
        workflowRunId={workflowRunId}
      />
    </I18nProvider>
  );
}

describe("ProjectTaskSurface", () => {
  beforeEach(() => {
    runsByProject.current = new Map();
    openModal.mockReset();
    transitionInstance.mockReset();
  });

  it("creates a Task directly from the lifecycle board", () => {
    render(surface("project-1"));

    fireEvent.click(screen.getByRole("button", { name: "Create Task" }));

    expect(openModal).toHaveBeenCalledWith("create-issue", {
      project_id: "project-1",
    });
  });

  it("falls back to Project Tasks when switching from an SOP run to a Project without runs", () => {
    runsByProject.current.set("project-with-run", [
      { id: "run-1", title: "Release SOP · Run 1", status: "active" },
    ]);

    const { rerender } = render(surface("project-with-run"));
    fireEvent.click(screen.getByRole("button", { name: "SOP Stage" }));
    expect(screen.getByTestId("workflow-board")).toHaveTextContent("run-1");

    rerender(surface("project-without-run"));

    expect(screen.getByTestId("project-task-board")).toHaveTextContent(
      "project-without-run",
    );
    expect(screen.getByRole("button", { name: "SOP Stage" })).toBeDisabled();
    expect(screen.queryByText("No available SOP runs")).not.toBeInTheDocument();
  });

  it("does not offer an archived SOP run as a Stage board", () => {
    runsByProject.current.set("project-1", [
      {
        id: "archived-run",
        title: "Unused run",
        status: "active",
        archived_at: "2026-08-03T08:00:00Z",
      },
    ]);

    render(surface("project-1"));

    expect(screen.getByRole("button", { name: "SOP Stage" })).toBeDisabled();
    expect(screen.getByTestId("project-task-board")).toBeInTheDocument();
  });

  it("keeps a completed run available for Stage history without creating new Tasks", () => {
    runsByProject.current.set("project-1", [
      { id: "done-run", title: "Completed development", status: "completed" },
    ]);

    render(surface("project-1"));
    fireEvent.click(screen.getByRole("button", { name: "SOP Stage" }));

    expect(screen.getByTestId("workflow-board")).toHaveTextContent("done-run");
    expect(screen.getByText("completed")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create Task" })).toBeDisabled();
    expect(transitionInstance).not.toHaveBeenCalled();
    expect(openModal).not.toHaveBeenCalled();
  });

  it("starts follow-up work at the first stage of a reusable completed run", () => {
    runsByProject.current.set("project-1", [
      { id: "done-run", title: "Development", status: "completed", accepts_new_tasks: true },
    ]);
    render(surface("project-1"));
    fireEvent.click(screen.getByRole("button", { name: "SOP Stage" }));
    fireEvent.click(screen.getByRole("button", { name: "Create Task" }));
    expect(openModal).toHaveBeenCalledWith("create-issue", expect.objectContaining({workflow_instance_id: "done-run", workflow_stage_id: "stage-1"}));
    expect(transitionInstance).not.toHaveBeenCalled();
  });

  it("opens a sidebar-selected SOP run directly without a run dropdown", () => {
    runsByProject.current.set("project-1", [
      { id: "run-1", title: "Release SOP · Run 1", status: "active" },
      { id: "run-2", title: "Release SOP · Run 2", status: "active" },
    ]);

    render(surface("project-1", "run-1"));

    expect(screen.getByTestId("workflow-board")).toHaveTextContent("run-1");
    expect(screen.getByText("Release SOP · Run 1")).toBeInTheDocument();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });
});
