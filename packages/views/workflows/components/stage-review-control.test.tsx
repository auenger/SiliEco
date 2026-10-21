import { act, fireEvent, render, screen } from "@testing-library/react";
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
  taskStatus: "done" | "in_progress" | "cancelled" = "done",
): WorkflowInstance {
  return {
    state_token: "a".repeat(64),
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
    fireEvent.click(screen.getByRole("checkbox", {name:"I understand that affected work must be completed and reviewed again."}));
    fireEvent.click(screen.getByRole("button", { name: "Return to Build" }));

    expect(transitionInstance).toHaveBeenCalledWith(
      {
        instanceId: "run-1",
        data: {
          expected_state_token: "a".repeat(64),
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

  it.each(["in_progress", "cancelled"] as const)("keeps approval disabled for %s required Tasks", (status) => {
    renderControl(
      workflowRun({ type: "human", decider: "member-1" }, status),
    );

    fireEvent.click(screen.getByRole("button", { name: "Review Stage" }));

    expect(screen.getByText("1 required Tasks are not completed")).toBeInTheDocument();
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

it("does not count invalidated manual work in the new completion barrier", () => {
  const instance = workflowRun({type:"human",decider:"member-1"}, "in_progress");
  instance.tasks![0]!.workflow_invalidated_by_rework_id = "old-execution";
  renderControl(instance);
  fireEvent.click(screen.getByRole("button", {name:"Review Stage"}));
  expect(screen.getByRole("button", {name:"Approve and complete this run"})).toBeEnabled();
});


beforeEach(() => transitionInstance.mockReset());

it("blocks a stale review after the Run returns to the same Stage", () => {
  const instance = workflowRun({type:"human", decider:"member-1"});
  const view = renderControl(instance);
  fireEvent.click(screen.getByRole("button", {name:"Review Stage"}));
  view.rerender(<I18nProvider locale="en" resources={{en:{workflows:enWorkflows}}}><StageReviewControl instance={{...instance,state_token:"b".repeat(64)}} /></I18nProvider>);
  expect(screen.getByRole("alert")).toHaveTextContent("The Run changed");
  expect(screen.getByRole("button", {name:"Approve and complete this run"})).toBeDisabled();
  expect(screen.getByRole("button", {name:"Return to Build"})).toBeDisabled();
  expect(transitionInstance).not.toHaveBeenCalled();
});

it("does not submit twice before pending state renders and locks uncertain decisions", () => {
  renderControl(workflowRun({type:"human",decider:"member-1"}));
  fireEvent.click(screen.getByRole("button", {name:"Review Stage"}));
  const approve = screen.getByRole("button", {name:"Approve and complete this run"});
  fireEvent.click(approve);
  fireEvent.click(approve);
  expect(transitionInstance).toHaveBeenCalledTimes(1);
  act(() => transitionInstance.mock.calls[0]![1].onError(new Error("Conflict")));
  expect(approve).toBeDisabled();
  expect(screen.getByRole("button", {name:"Return to Build"})).toBeDisabled();
});

it("requires a valid state token from the backend", () => {
  renderControl({...workflowRun({type:"human",decider:"member-1"}),state_token:null});
  fireEvent.click(screen.getByRole("button", {name:"Review Stage"}));
  expect(screen.getByRole("button", {name:"Approve and complete this run"})).toBeDisabled();
});


it("requires a reason and impact acknowledgement before returning", () => {
  renderControl(workflowRun({type:"human",decider:"member-1"}));
  fireEvent.click(screen.getByRole("button",{name:"Review Stage"}));
  const rollback=screen.getByRole("button",{name:"Return to Build"});
  expect(rollback).toBeDisabled();
  fireEvent.change(screen.getByLabelText("Review note"),{target:{value:"New evidence required"}});
  expect(rollback).toBeDisabled();
  fireEvent.click(screen.getByRole("checkbox",{name:"I understand that affected work must be completed and reviewed again."}));
  expect(rollback).toBeEnabled();
  fireEvent.change(screen.getByLabelText("Review note"),{target:{value:"界".repeat(1400)}});
  expect(rollback).toBeDisabled();
  expect(screen.getByLabelText("Review note")).toBeEnabled();
});
