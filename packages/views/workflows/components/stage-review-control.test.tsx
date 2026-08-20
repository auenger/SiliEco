import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { WorkflowInstance } from "@silieco/core/types";
import enWorkflows from "../../locales/en/workflows.json";
import { StageReviewControl } from "./stage-review-control";

const { transitionInstance } = vi.hoisted(() => ({
  transitionInstance: vi.fn(),
}));

vi.mock("@silieco/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "member-1" } }),
}));

vi.mock("@silieco/core/workflows", () => ({
  useTransitionWorkflowInstance: () => ({
    isPending: false,
    mutate: transitionInstance,
  }),
}));

function workflowRun(
  gate: Record<string, unknown>,
  taskStatus: "done" | "in_progress" = "done",
): WorkflowInstance {
  return {
    id: "run-1",
    workspace_id: "workspace-1",
    workflow_id: "workflow-1",
    workflow_version_id: "version-1",
    title: "Release run",
    description: null,
    revision: 1,
    can_edit: true,
    source_version: 1,
    status: "active",
    current_stage_id: "stage-review",
    current_stage_name: "Review",
    current_stage_index: 1,
    stage_count: 2,
    project_id: "project-1",
    created_by: "member-1",
    started_at: "2026-08-07T00:00:00Z",
    completed_at: null,
    task_count: 1,
    archived_at: null,
    archived_by: null,
    created_at: "2026-08-07T00:00:00Z",
    updated_at: "2026-08-07T00:00:00Z",
    stages: [
      {
        id: "stage-build",
        workspace_id: "workspace-1",
        workflow_instance_id: "run-1",
        source_stage_id: "source-build",
        stable_key: "stage_1",
        name: "Build",
        description: null,
        position: 0,
        completion_rule: {},
        input_spec: {},
        output_spec: {},
        required_skills: [],
        gate: { type: "none" },
        rollback_stage_key: null,
        created_at: "2026-08-07T00:00:00Z",
        updated_at: "2026-08-07T00:00:00Z",
      },
      {
        id: "stage-review",
        workspace_id: "workspace-1",
        workflow_instance_id: "run-1",
        source_stage_id: "source-review",
        stable_key: "stage_2",
        name: "Review",
        description: null,
        position: 1,
        completion_rule: {},
        input_spec: {},
        output_spec: {},
        required_skills: [],
        gate,
        rollback_stage_key: "stage_1",
        created_at: "2026-08-07T00:00:00Z",
        updated_at: "2026-08-07T00:00:00Z",
      },
    ],
    tasks: [
      {
        id: "task-1",
        workflow_instance_id: "run-1",
        workflow_stage_id: "stage-review",
        status: taskStatus,
      },
    ],
  } as unknown as WorkflowInstance;
}

function renderControl(instance: WorkflowInstance) {
  return render(
    <I18nProvider locale="en" resources={{ en: { workflows: enWorkflows } }}>
      <StageReviewControl instance={instance} />
    </I18nProvider>,
  );
}

describe("StageReviewControl", () => {
  beforeEach(() => {
    transitionInstance.mockReset();
  });

  it("submits a human rejection to the configured rollback Stage", () => {
    renderControl(workflowRun({ type: "human", decider: "member-1" }));

    fireEvent.click(screen.getByRole("button", { name: "Review Stage" }));
    fireEvent.change(screen.getByLabelText("Review note"), {
      target: { value: "Missing release evidence" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Return to Build" }));

    expect(transitionInstance).toHaveBeenCalledWith(
      {
        instanceId: "run-1",
        data: {
          outcome: "rejected",
          note: "Missing release evidence",
          target_stage_id: "stage-build",
        },
      },
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
      }),
    );
  });

  it("keeps approval disabled until every Task in the Stage is terminal", () => {
    renderControl(
      workflowRun({ type: "human", decider: "member-1" }, "in_progress"),
    );

    fireEvent.click(screen.getByRole("button", { name: "Review Stage" }));

    expect(screen.getByText("1 Tasks are still open")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Approve and complete this run" }),
    ).toBeDisabled();
  });

  it("does not offer a human action for an Agent gate", () => {
    renderControl(workflowRun({ type: "agent", decider: "agent-1" }));

    expect(screen.getByText("Awaiting Agent decision")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Review Stage" }),
    ).not.toBeInTheDocument();
  });

  it("disables review for a different configured member", () => {
    renderControl(workflowRun({ type: "human", decider: "member-2" }));

    expect(
      screen.getByRole("button", { name: "Awaiting assigned member" }),
    ).toBeDisabled();
  });

  it("keeps the validation panel visible when the run has no current Stage", () => {
    const instance = workflowRun({ type: "none" });
    instance.current_stage_id = null;

    renderControl(instance);

    expect(screen.getByText("Stage validation")).toBeInTheDocument();
    expect(
      screen.getByText("This run has no active Stage to validate."),
    ).toBeInTheDocument();
    expect(screen.getByText("Unavailable")).toBeInTheDocument();
  });
});
