import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import enWorkflows from "../../locales/en/workflows.json";
import { WorkflowRunPlanDialog } from "./workflow-run-plan-dialog";

const { emptyList, mutate, testSkills } = vi.hoisted(() => ({
  emptyList: [] as unknown[],
  mutate: vi.fn(),
  testSkills: [{ id: "skill-1", name: "TDD" }],
}));

const run = {
  id: "run-1",
  workspace_id: "workspace-1",
  workflow_id: "workflow-1",
  workflow_version_id: "version-1",
  title: "Release run",
  description: null,
  revision: 2,
  can_edit: true,
  source_version: 3,
  status: "active",
  current_stage_id: "stage-current",
  current_stage_name: "Build",
  current_stage_index: 1,
  stage_count: 3,
  project_id: "project-1",
  created_by: "user-1",
  started_at: "2026-01-01T00:00:00Z",
  completed_at: null,
  task_count: 0,
  archived_at: null,
  archived_by: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  stages: [
    {
      id: "stage-past",
      workspace_id: "workspace-1",
      workflow_instance_id: "run-1",
      source_stage_id: "source-1",
      stable_key: "stage_1",
      name: "Plan",
      description: null,
      position: 0,
      completion_rule: { type: "all_tasks_terminal" },
      input_spec: {},
      output_spec: {},
      required_skills: [],
      gate: { type: "none" },
      rollback_stage_key: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    {
      id: "stage-current",
      workspace_id: "workspace-1",
      workflow_instance_id: "run-1",
      source_stage_id: "source-2",
      stable_key: "stage_2",
      name: "Build",
      description: null,
      position: 1,
      completion_rule: { type: "all_tasks_terminal" },
      input_spec: {},
      output_spec: { default_artifact: "build.md" },
      required_skills: ["skill-1"],
      gate: { type: "none" },
      rollback_stage_key: null,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
    {
      id: "stage-future",
      workspace_id: "workspace-1",
      workflow_instance_id: "run-1",
      source_stage_id: "source-3",
      stable_key: "stage_3",
      name: "Review",
      description: null,
      position: 2,
      input_bindings: {schema_version:"silieco.input-bindings/v1",data:[]},
      execution_mode: "agent",
      executor_id: "agent-1",
      completion_rule: { type: "validated_result" },
      input_spec: {},
      output_spec: {contract:{schema_version:"silieco.stage/v1",data:{type:"object"}}},
      required_skills: [],
      gate: { type: "human" },
      rollback_stage_key: "stage_2",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
  ],
  tasks: [],
  decisions: [],
  changes: [],
};

vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@silieco/core/mcp", () => ({
  workspaceMCPServersOptions: () => ({ queryKey: ["workspace-mcp"] }),
}));

vi.mock("@silieco/core/workflows", () => ({
  workflowInstanceOptions: () => ({ queryKey: ["workflow-run"] }),
  useUpdateWorkflowInstancePlan: () => ({ isPending: false, mutate }),
}));

vi.mock("@silieco/core/workspace/queries", () => ({
  skillListOptions: () => ({ queryKey: ["skills"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => ({
    data:
      queryKey[0] === "skills"
        ? testSkills
        : queryKey[0] === "agents" ? [{id:"agent-1",name:"Builder",archived_at:null}]
        : queryKey[0] === "members" || queryKey[0] === "workspace-mcp"
          ? emptyList
          : run,
    isLoading: false,
  }),
}));

describe("WorkflowRunPlanDialog", () => {
  beforeEach(() => mutate.mockReset());

  it("locks completed Stages and saves current/future adjustments with the run revision", async () => {
    render(
      <I18nProvider locale="en" resources={{ en: { workflows: enWorkflows } }}>
        <WorkflowRunPlanDialog
          instanceId="run-1"
          sourceName="Release SOP"
          open
          onOpenChange={vi.fn()}
        />
      </I18nProvider>,
    );

    const stageNames = screen.getAllByLabelText("Stage name");
    expect(stageNames[0]).toBeDisabled();
    expect(stageNames[1]).toBeEnabled();
    expect(stageNames[2]).toBeEnabled();

    const deleteButtons = screen.getAllByRole("button", { name: "Delete Stage" });
    expect(deleteButtons[0]).toBeDisabled();
    expect(deleteButtons[1]).toBeDisabled();
    expect(deleteButtons[2]).toBeEnabled();

    const outputs = screen.getAllByLabelText("Output requirement");
    fireEvent.change(outputs[1]!, { target: { value: "release.md" } });
    expect(screen.getByRole("button", {name: "Save run adjustments"})).toBeEnabled();
    fireEvent.click(screen.getAllByRole("combobox", {name: "Rework rollback target"})[2]!);
    fireEvent.mouseMove(screen.getByRole("option", {name: "Plan"}));
    fireEvent.click(screen.getByRole("option", {name: "Plan"}));
    await waitFor(() => expect(screen.getAllByRole("combobox", {name: "Rework rollback target"})[2]).toHaveTextContent("Plan"));
    fireEvent.click(screen.getByRole("button", { name: "Save run adjustments" }));
    expect(screen.getByText("Save these run adjustments?")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Save run adjustments" }));

    expect(mutate).toHaveBeenCalledWith(
      expect.objectContaining({
        instanceId: "run-1",
        data: expect.objectContaining({
          expected_revision: 2,
          stages: expect.arrayContaining([
            expect.objectContaining({
              id:"stage-future", rollback_stage_key:"stage_1", input_bindings:{schema_version:"silieco.input-bindings/v1",data:[]}, execution_mode:"agent", executor_id:"agent-1",
              completion_rule:{type:"validated_result"},
              output_spec:{contract:{schema_version:"silieco.stage/v1",data:{type:"object"}}},
            }),
            expect.objectContaining({
              id: "stage-current",
              output_spec: { default_artifact: "release.md" },
              required_skills: ["skill-1"],
            }),
          ]),
        }),
      }),
      expect.objectContaining({ onSuccess: expect.any(Function), onError: expect.any(Function) }),
    );
  });
});
