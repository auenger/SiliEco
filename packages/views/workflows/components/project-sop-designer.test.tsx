import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@silieco/core/i18n/react";
import enWorkflows from "../../locales/en/workflows.json";
import { ProjectSopDesigner } from "./project-sop-designer";

const { agents, archiveInstance, createInstance, createWorkflow, members, navigateTo, runs, skills, workflows } = vi.hoisted(() => ({
  agents: { current: [] as unknown[] },
  archiveInstance: vi.fn(),
  createInstance: vi.fn(),
  createWorkflow: vi.fn(),
  members: { current: [] as unknown[] },
  navigateTo: vi.fn(),
  runs: { current: [] as unknown[] },
  skills: { current: [] as unknown[] },
  workflows: { current: [] as unknown[] },
}));

vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@silieco/core/paths", () => ({
  useWorkspacePaths: () => ({
    projectWorkflowRun: (projectId: string, runId: string) =>
      `/projects/${projectId}?run=${runId}`,
  }),
}));

vi.mock("@silieco/core/mcp", () => ({
  workspaceMCPServersOptions: () => ({ queryKey: ["workspace-mcp"] }),
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ push: navigateTo }),
}));

vi.mock("@silieco/core/workflows", () => ({
  useWorkflowChatEditorStore: Object.assign((selector: (state: { undo: null }) => unknown) => selector({ undo: null }), { getState: () => ({ undo: null }) }),
  workflowListOptions: () => ({ queryKey: ["workflows"] }),
  workflowInstancesOptions: () => ({ queryKey: ["workflow-runs"] }),
  useCreateWorkflow: () => ({
    isPending: false,
    mutate: createWorkflow,
  }),
  useCreateWorkflowInstance: () => ({
    isPending: false,
    mutate: createInstance,
  }),
  useArchiveWorkflowInstance: () => ({
    isPending: false,
    mutate: archiveInstance,
  }),
}));

vi.mock("@silieco/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["workspace-agents"] }),
  memberListOptions: () => ({ queryKey: ["workspace-members"] }),
  skillListOptions: () => ({ queryKey: ["workspace-skills"] }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    const data =
      queryKey[0] === "workflow-runs"
        ? runs.current
        : queryKey[0] === "workspace-mcp"
          ? { servers: [] }
        : queryKey[0] === "workspace-members"
          ? members.current
          : queryKey[0] === "workspace-agents"
            ? agents.current
            : queryKey[0] === "workspace-skills"
              ? skills.current
              : workflows.current;
    return { data, isLoading: false };
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

vi.mock("../../editor", () => ({
  ContentEditor: ({
    defaultValue,
    value,
    onUpdate,
    placeholder,
  }: {
    defaultValue?: string;
    value?: string;
    onUpdate: (value: string) => void;
    placeholder: string;
  }) => (
    <textarea
      defaultValue={value ?? defaultValue}
      placeholder={placeholder}
      onChange={(event) => onUpdate(event.currentTarget.value)}
    />
  ),
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
}));

vi.mock("./workflow-run-plan-dialog", () => ({
  WorkflowRunPlanDialog: ({ sourceName }: { sourceName: string }) => (
    <div role="dialog">Adjusting {sourceName}</div>
  ),
}));

function renderDesigner() {
  return render(
    <I18nProvider locale="en" resources={{ en: { workflows: enWorkflows } }}>
      <ProjectSopDesigner projectId="project-1" />
    </I18nProvider>,
  );
}

describe("ProjectSopDesigner", () => {
  beforeEach(() => {
    createInstance.mockReset();
    archiveInstance.mockReset();
    createWorkflow.mockReset();
    navigateTo.mockReset();
    agents.current = [];
    members.current = [
      {
        user_id: "owner-1",
        role: "owner",
        name: "Workspace Owner",
        email: "owner@example.com",
      },
    ];
    runs.current = [];
    skills.current = [];
    workflows.current = [];
  });

  it("offers the prototype templates without rendering Task management", () => {
    renderDesigner();

    expect(screen.getByRole("button", { name: /散热仿真 POC/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Feature 研发工作流/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /投标模板/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /软件开发模板/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Bug 修复模板/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /通用文档协作模板/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /空白 SOP/ })).toBeInTheDocument();
    expect(screen.queryByText("Task board")).not.toBeInTheDocument();
    expect(screen.queryByText("Start a Workflow")).not.toBeInTheDocument();
  });

  it("publishes the thermal simulation POC with built-in stage skills and human gates", () => {
    renderDesigner();

    fireEvent.click(screen.getByRole("button", { name: /散热仿真 POC/ }));
    expect(screen.getByDisplayValue("散热仿真 POC（Mock）")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /散热需求提取/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /候选仿真（Mock）/ })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Publish SOP" }));

    const [payload] = createWorkflow.mock.calls[0]!;
    expect(payload.stages).toHaveLength(8);
    expect(payload.stages.map((item: { required_skills: string[] }) => item.required_skills)).toEqual([
      ["silieco-thermal-requirements"],
      ["silieco-thermal-requirements-review"],
      ["silieco-thermal-model-precheck", "silieco-thermal-mock-runner"],
      ["silieco-thermal-baseline", "silieco-thermal-mock-runner"],
      ["silieco-thermal-result-review", "silieco-thermal-mock-runner"],
      ["silieco-thermal-candidate-review"],
      ["silieco-thermal-candidate-simulation", "silieco-thermal-mock-runner"],
      ["silieco-thermal-report", "silieco-thermal-mock-runner"],
    ]);
    expect(payload.stages[1].gate).toMatchObject({ type: "human", require_human: true });
    expect(payload.stages[5].gate).toMatchObject({ type: "human", require_human: true });
    expect(payload.stages[7].gate).toMatchObject({ type: "human", require_human: true });
    expect(payload.stages[3].output_spec).toEqual({
      default_artifact: "baseline-job.json + baseline-result.json",
    });
  });

  it("maps the built-in Feature workflow stages to their platform skills", () => {
    renderDesigner();

    fireEvent.click(screen.getByRole("button", { name: /Feature 研发工作流/ }));

    expect(screen.getByDisplayValue("Feature 研发工作流")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /需求输入与分析/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /PRD 产出/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /PRD 校验/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /原型或 MVP/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Feature 拆分与开发准备/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /开发实现/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /本地化测试与验收/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /完成与归档/ })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Publish SOP" }));

    const [payload] = createWorkflow.mock.calls[0]!;
    expect(payload.stages).toHaveLength(8);
    expect(payload.stages.map((item: { required_skills: string[] }) => item.required_skills)).toEqual([
      ["silieco-new-feature"],
      ["silieco-enrich-feature"],
      ["silieco-review-spec"],
      ["silieco-prototype-feature"],
      ["silieco-split-feature"],
      ["silieco-run-feature"],
      ["silieco-verify-feature"],
      ["silieco-complete-feature"],
    ]);
    expect(payload.stages[4].output_spec).toEqual({
      default_artifact: "已关联的开发子 Task / 分组顺序 / 覆盖摘要",
    });
    expect(payload.stages[2].gate).toMatchObject({
      type: "human",
      require_human: true,
    });
    expect(payload.stages[5].gate).toMatchObject({ type: "none" });
    expect(payload.stages[6].gate).toMatchObject({
      type: "human",
      require_human: true,
    });
  });

  it("starts a published SOP with a distinct editable run name", () => {
    workflows.current = [
      {
        id: "workflow-1",
        name: "Release SOP",
        description: "Ship safely",
        status: "published",
        current_version: { stages: [{ id: "stage-1" }] },
      },
    ];
    renderDesigner();

    fireEvent.click(screen.getByRole("button", { name: "Start SOP" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    const runName = screen.getByLabelText("Run name");
    expect(runName).toHaveValue("Release SOP · Run 1");
    fireEvent.change(runName, { target: { value: "Mobile 2.0 release" } });
    const inputs = screen.getByLabelText(/Run inputs \(JSON\)/);
    fireEvent.change(inputs, {target: {value: "{"}});
    expect(screen.getByRole("button", {name: "Start run"})).toBeDisabled();
    fireEvent.change(inputs, {target: {value: '{"goal":"Publish the report"}'}});
    fireEvent.click(screen.getByRole("button", { name: "Start run" }));

    fireEvent.click(screen.getByRole("button", { name: "Start run" }));
    fireEvent.keyDown(runName, {key: "Enter"});
    expect(createInstance).toHaveBeenCalledTimes(1);

    expect(createInstance).toHaveBeenCalledWith(
      {
        workflowId: "workflow-1",
        data: {
          title: "Mobile 2.0 release",
          inputs: {goal: "Publish the report"},
          project_id: "project-1",
          start: true,
        },
      },
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
        onSettled: expect.any(Function),
      }),
    );
  });

  it("shows SOP runs that belong to the current Project", () => {
    workflows.current = [
      {
        id: "workflow-1",
        project_id: null,
        name: "Release SOP",
        description: "Ship safely",
        status: "published",
        current_version: {
          stages: [
            {
              id: "stage-build",
              name: "Build",
              completion_rule: {},
              input_spec: {},
              output_spec: {},
              required_skills: [],
              gate: { type: "none" },
            },
          ],
        },
      },
    ];
    runs.current = [
      {
        id: "run-1",
        workflow_id: "workflow-1",
        title: "Mobile rollout",
        status: "active",
        current_stage_id: "stage-build",
        current_stage_name: "Build",
        current_stage_index: 0,
        stage_count: 1,
        source_version: 1,
        revision: 1,
        can_edit: true,
        task_count: 0,
      },
    ];

    renderDesigner();

    expect(screen.getByText("SOP runs in this Project")).toBeInTheDocument();
    expect(screen.getByText("Mobile rollout")).toBeInTheDocument();
    expect(screen.getAllByText("Release SOP").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Build").length).toBeGreaterThan(0);
    expect(screen.getByText("Active")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Open run board" }));
    expect(navigateTo).toHaveBeenCalledWith("/projects/project-1?run=run-1");
    fireEvent.click(screen.getByRole("button", { name: "Adjust this run" }));
    expect(screen.getByRole("dialog")).toHaveTextContent("Adjusting Release SOP");
  });

  it("loads the software template into a five-Stage SOP editor", () => {
    renderDesigner();

    fireEvent.click(screen.getByRole("button", { name: /软件开发模板/ }));

    expect(screen.getByDisplayValue("软件开发模板")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /需求设计/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /架构设计/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /开发实现/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /E2E 测试/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /发布验收/ })).toBeInTheDocument();
  });

  it("publishes the selected template with Stage configuration only", () => {
    renderDesigner();
    fireEvent.click(screen.getByRole("button", { name: /软件开发模板/ }));

    fireEvent.click(screen.getByRole("button", { name: "Publish SOP" }));

    expect(createWorkflow).toHaveBeenCalledTimes(1);
    const [payload] = createWorkflow.mock.calls[0]!;
    expect(payload).toMatchObject({
      name: "软件开发模板",
      publish: true,
    });
    expect(payload).not.toHaveProperty("project_id");
    expect(payload.stages).toHaveLength(5);
    expect(payload.stages[2]).toMatchObject({
      stable_key: "stage_3",
      name: "开发实现",
      input_spec: { default_artifact: "架构文档.md" },
      output_spec: { default_artifact: "代码 + 单元测试" },
      required_skills: ["编码", "TDD"],
      gate: {
        type: "agent",
        decider_type: "self_agent",
        decider: "@self",
      },
      completion_rule: expect.objectContaining({
        evaluation_mode: "on_task_change",
      }),
    });
  });

  it("preserves the executor and contracts when inheriting an automatic SOP", () => {
    agents.current = [{id:"agent-1",name:"Builder",archived_at:null}];
    const contract = { schema_version: "silieco.stage/v1", data: { type: "object" } };
    workflows.current = [{
      id: "automatic-source", project_id: null, name: "Automatic source", status: "published",
      current_version: { version: 1, stages: [{
        id: "automatic-stage", stable_key: "build-stable", input_bindings: {schema_version:"silieco.input-bindings/v1",data:[]}, name: "Build", execution_mode: "agent", executor_id: "agent-1",
        completion_rule: { type: "validated_result" }, input_spec: { contract }, output_spec: { contract },
        required_skills: [], gate: { type: "none" },
      }, {
        id: "review-stage", stable_key: "review-stable", name: "Review", execution_mode: "agent", executor_id: "agent-1",
        completion_rule: {type: "validated_result"}, output_spec: {contract}, input_spec: {}, required_skills: [],
        gate: {type: "none"}, rollback_stage_key: "build-stable",
      }] },
    }];
    renderDesigner();
    fireEvent.click(screen.getByRole("button", { name: "Inherit and customize" }));
    fireEvent.click(screen.getByRole("button", { name: "Publish SOP" }));
    expect(createWorkflow.mock.calls[0]?.[0].stages[0]).toMatchObject({
      stable_key: "build-stable", input_bindings: {schema_version:"silieco.input-bindings/v1",data:[]}, execution_mode: "agent", executor_id: "agent-1", completion_rule: { type: "validated_result" },
      input_spec: { contract }, output_spec: { contract },
    });
    expect(createWorkflow.mock.calls[0]?.[0].stages[1].rollback_stage_key).toBe("build-stable");
  });

  it("publishes an agent stage with an earlier manual rollback target", () => {
    agents.current = [{id:"agent-1",name:"Builder",archived_at:null}];
    workflows.current = [{id:"mixed-source",project_id:null,name:"Mixed source",status:"published",current_version:{version:1,stages:[
      {id:"manual-stage",stable_key:"manual-input",name:"Manual input",execution_mode:"manual",executor_id:null,completion_rule:{type:"all_tasks_terminal"},input_spec:{},output_spec:{},required_skills:[],gate:{type:"none"}},
      {id:"agent-stage",stable_key:"build",name:"Build",execution_mode:"agent",executor_id:"agent-1",completion_rule:{type:"validated_result"},input_spec:{},output_spec:{contract:{schema_version:"silieco.stage/v1",data:{type:"object"}}},required_skills:[],gate:{type:"none"},rollback_stage_key:"manual-input"},
    ]}}];
    renderDesigner();fireEvent.click(screen.getByRole("button",{name:"Inherit and customize"}));
    expect(screen.getByRole("button",{name:"Publish SOP"})).toBeEnabled();
    fireEvent.click(screen.getByTitle("Build"));
    fireEvent.click(screen.getByRole("button", { name: "Execution & resources" }));
    fireEvent.click(screen.getByRole("combobox",{name:"Rework rollback target"}));
    expect(screen.getByRole("option",{name:"Manual input"})).toBeInTheDocument();
    fireEvent.click(screen.getByRole("option",{name:"Manual input"}));
    fireEvent.click(screen.getByRole("button",{name:"Publish SOP"}));
    expect(createWorkflow.mock.calls[0]?.[0].stages[1]).toMatchObject({execution_mode:"agent",rollback_stage_key:"manual-input"});
  });

  it("creates a Project variant from a reusable Space SOP", () => {
    workflows.current = [
      {
        id: "space-workflow-1",
        project_id: null,
        name: "Release SOP",
        description: "Ship safely",
        status: "published",
        current_version: {
          version: 2,
          stages: [
            {
              id: "stage-1",
              name: "Review",
              description: "Review the complete delivery",
              completion_rule: {
                type: "all_tasks_terminal",
                evaluation_mode: "manual",
                allowed_task_statuses: ["in_review", "done"],
              },
              input_spec: { default_artifact: "release.zip" },
              output_spec: { default_artifact: "review.md" },
              required_skills: ["release-review"],
              gate: {
                type: "human",
                decider_type: "human",
                decider: "@owner",
                require_human: true,
              },
              rollback_stage_key: null,
            },
          ],
        },
      },
    ];
    renderDesigner();

    expect(screen.getByText("Space asset")).toBeInTheDocument();
    expect(screen.getByText("Evaluate on manual submit")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: "Inherit and customize" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Publish SOP" }));

    const [payload] = createWorkflow.mock.calls[0]!;
    expect(payload).toMatchObject({
      project_id: "project-1",
      name: "Release SOP Project 版",
      publish: true,
      stages: [
        expect.objectContaining({
          name: "Review",
          description: "Review the complete delivery",
          completion_rule: expect.objectContaining({
            evaluation_mode: "manual",
          }),
          input_spec: { default_artifact: "release.zip" },
          output_spec: { default_artifact: "review.md" },
          required_skills: ["release-review"],
        }),
      ],
    });
  });

  it("shows and removes a custom Stage status", () => {
    renderDesigner();
    fireEvent.click(screen.getByRole("button", { name: /Bug 修复模板/ }));
    fireEvent.click(screen.getByRole("navigation").querySelectorAll("button")[1]!);
    fireEvent.click(screen.getByRole("button", { name: "Execution & resources" }));
    const customStatus = screen.getByPlaceholderText("Custom status, Enter");

    fireEvent.change(customStatus, { target: { value: "QA ready" } });
    fireEvent.keyDown(customStatus, { key: "Enter" });

    const statusButton = screen.getByRole("button", { name: "QA ready" });
    expect(statusButton).toBeInTheDocument();
    fireEvent.click(statusButton);
    expect(screen.queryByRole("button", { name: "QA ready" })).not.toBeInTheDocument();
  });

  it("authors artifacts in Markdown and selects real Skills and gate actors", () => {
    skills.current = [
      {
        id: "skill-review",
        name: "Code review",
        description: "Review implementation quality",
        config: {},
      },
    ];
    agents.current = [
      {
        id: "agent-reviewer",
        name: "Review Agent",
        description: "Independent reviewer",
        archived_at: null,
      },
    ];
    renderDesigner();

    fireEvent.click(screen.getByRole("button", { name: /空白 SOP/ }));
    fireEvent.change(screen.getByLabelText("SOP name"), {
      target: { value: "Delivery SOP" },
    });
    fireEvent.click(screen.getByRole("navigation").querySelectorAll("button")[1]!);
    fireEvent.change(screen.getByPlaceholderText("Stage name"), {
      target: { value: "Review" },
    });
    fireEvent.change(screen.getByPlaceholderText("e.g. requirements.md"), {
      target: { value: "## Required input\n\n- `PRD.md`" },
    });
    fireEvent.change(screen.getByPlaceholderText("e.g. acceptance-report.md"), {
      target: { value: "## Required output\n\n- `review.md`" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Execution & resources" }));
    fireEvent.click(screen.getByRole("button", { name: "Choose Skills" }));
    fireEvent.click(screen.getByRole("button", { name: /Code review/ }));
    fireEvent.click(screen.getByRole("button", { name: "Review & approval" }));
    fireEvent.click(screen.getByRole("switch", { name: "Stage exit gate" }));
    fireEvent.click(
      screen.getByRole("radio", { name: /Assigned Agent decision/ }),
    );
    fireEvent.click(
      screen.getByRole("switch", {
        name: "Require final human confirmation even after an Agent decision",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Publish SOP" }));

    const [payload] = createWorkflow.mock.calls[0]!;
    expect(payload.stages[0]).toMatchObject({
      input_spec: {
        default_artifact: "## Required input\n\n- `PRD.md`",
      },
      output_spec: {
        default_artifact: "## Required output\n\n- `review.md`",
      },
      required_skills: ["skill-review"],
      gate: {
        type: "hybrid",
        decider_type: "agent",
        decider: "agent-reviewer",
        require_human: true,
        human_decider: "owner-1",
      },
    });
  });
});
