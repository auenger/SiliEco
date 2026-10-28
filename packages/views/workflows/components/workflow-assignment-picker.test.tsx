import { useState } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import enWorkflows from "../../locales/en/workflows.json";
import {
  WorkflowAssignmentPicker,
  type WorkflowAssignment,
} from "./workflow-assignment-picker";

const { createInstance, workflowRuns } = vi.hoisted(() => ({
  createInstance: vi.fn(),
  workflowRuns: {
    current: [
      {
        id: "run-1",
        workflow_id: "workflow-1",
        title: "Release A",
        status: "active", accepts_new_tasks: false,
      },
      {
        id: "run-2",
        workflow_id: "workflow-1",
        title: "Release B",
        status: "active", accepts_new_tasks: false,
      },
    ],
  },
}));

vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@silieco/core/workflows", () => ({
  workflowListOptions: () => ({ queryKey: ["workflows"] }),
  workflowInstancesOptions: () => ({ queryKey: ["workflow-runs"] }),
  workflowInstanceOptions: (_workspaceId: string, runId: string) => ({
    queryKey: ["workflow-run", runId],
  }),
  useCreateWorkflowInstance: () => ({
    isPending: false,
    mutate: (variables: unknown, options?: { onSuccess?: (created: { id: string }) => void }) => {
      createInstance(variables);
      options?.onSuccess?.({ id: "run-created" });
    },
  }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === "workflows") {
      return {
        data: [{ id: "workflow-1", name: "Release SOP" }],
      };
    }
    if (queryKey[0] === "workflow-runs") {
      return { data: workflowRuns.current };
    }
    if (queryKey[0] === "workflow-run" && queryKey[1] === "run-1") {
      return {
        data: {
          id: "run-1",
          status: workflowRuns.current[0]?.status,
          current_stage_id: "stage-2",
          stages: [
            { id: "stage-1", name: "Build" },
            { id: "stage-2", name: "Review" },
          ],
        },
      };
    }
    return { data: undefined };
  },
}));

vi.mock("@silieco/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="menu-group">{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    disabled,
    onClick,
  }: {
    children: React.ReactNode;
    disabled?: boolean;
    onClick?: () => void;
  }) => (
    <button type="button" disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuSeparator: () => null,
}));

vi.mock("../../common/pill-button", () => ({
  PillButton: ({ children }: { children: React.ReactNode }) => (
    <button type="button">{children}</button>
  ),
}));

function Harness() {
  const [assignment, setAssignment] = useState<WorkflowAssignment>({});
  return (
    <I18nProvider locale="en" resources={{ en: { workflows: enWorkflows } }}>
      <WorkflowAssignmentPicker
        projectId="project-1"
        workflowInstanceId={assignment.workflowInstanceId}
        workflowStageId={assignment.workflowStageId}
        onChange={setAssignment}
      />
      <output data-testid="assignment">
        {JSON.stringify(assignment)}
      </output>
    </I18nProvider>
  );
}

describe("WorkflowAssignmentPicker", () => {
  beforeEach(() => {
    createInstance.mockReset();
    workflowRuns.current = [
      {
        id: "run-1",
        workflow_id: "workflow-1",
        title: "Release A",
        status: "active", accepts_new_tasks: false,
      },
      {
        id: "run-2",
        workflow_id: "workflow-1",
        title: "Release B",
        status: "active", accepts_new_tasks: false,
      },
    ];
  });

  it("selects a concrete run and its current Stage atomically", async () => {
    render(<Harness />);

    expect(screen.getByText("Task management").closest("[data-testid=menu-group]")).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Release SOP/ }));

    await waitFor(() =>
      expect(screen.getByTestId("assignment")).toHaveTextContent(
        '{"workflowInstanceId":"run-1","workflowStageId":"stage-2"}',
      ),
    );
    expect(screen.getAllByText("Release A")).toHaveLength(2);
    expect(screen.getByText("Stage · Review")).toBeInTheDocument();
  });

  it("returns to the Project-only lifecycle without stale SOP coordinates", async () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole("button", { name: /Release SOP/ }));
    await screen.findByText("Stage · Review");

    fireEvent.click(
      screen.getByRole("button", { name: /Straight lifecycle/ }),
    );

    await waitFor(() =>
      expect(screen.getByTestId("assignment")).toHaveTextContent("{}"),
    );
  });

  it("creates the first run lazily when selecting a new SOP", async () => {
    workflowRuns.current = [];
    render(<Harness />);

    fireEvent.click(screen.getByRole("button", { name: /Release SOP/ }));

    expect(createInstance).toHaveBeenCalledWith({
      workflowId: "workflow-1",
      data: {
        title: "Release SOP · Run 1",
        project_id: "project-1",
        start: true,
      },
    });
    await waitFor(() =>
      expect(screen.getByTestId("assignment")).toHaveTextContent(
        '{"workflowInstanceId":"run-created"}',
      ),
    );
  });
});

it("reuses a completed task pipeline and selects its first stage", async () => {
  createInstance.mockReset();
  workflowRuns.current = [{id:"run-1", workflow_id:"workflow-1", title:"Release A", status:"completed", accepts_new_tasks:true}];
  render(<Harness/>);
  fireEvent.click(screen.getByRole("button", {name:/Release SOP/}));
  await waitFor(() => expect(screen.getByTestId("assignment")).toHaveTextContent('"workflowStageId":"stage-1"'));
  expect(createInstance).not.toHaveBeenCalled();
});
