import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { Agent, ProjectResource } from "@silieco/core/types";
import enCommon from "../../locales/en/common.json";
import enResources from "../../locales/en/resources.json";
import enProjects from "../../locales/en/projects.json";

const TEST_RESOURCES = { en: { common: enCommon, projects: enProjects, resources: enResources } };

const mocks = vi.hoisted(() => ({
  resources: [] as ProjectResource[],
  agents: [] as Agent[],
  daemon: {
    daemonId: "daemon-local" as string | null,
    deviceName: "This Mac" as string | null,
    running: true,
  },
  machines: [] as Array<{
    daemonId: string;
    title: string;
    health: "online" | "offline";
    isCurrent: boolean;
    runtimes: Array<{ id: string; owner_id: string | null }>;
  }>,
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    if (key === "projects") return { data: mocks.resources, isPending: false };
    if (key === "runtimes") return { data: [], isPending: false };
    if (options.queryKey?.includes("agents")) {
      return { data: mocks.agents, isPending: false };
    }
    if (options.queryKey?.includes("members")) {
      return {
        data: [{ user_id: "user-ryan", name: "Ryan" }],
        isPending: false,
      };
    }
    return { data: [], isPending: false };
  },
}));

vi.mock("@silieco/core/projects", () => ({
  projectResourcesOptions: () => ({ queryKey: ["projects"] }),
  useCheckProjectDirectory: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useCreateProjectResource: () => ({ isPending: false, mutateAsync: vi.fn() }),
  useUpdateProjectResource: () => ({ mutateAsync: vi.fn() }),
  useDeleteProjectResource: () => ({ mutateAsync: vi.fn() }),
}));

vi.mock("@silieco/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
}));

vi.mock("@silieco/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["workspaces", "agents"] }),
  memberListOptions: () => ({ queryKey: ["workspaces", "members"] }),
}));

vi.mock("@silieco/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@silieco/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-ryan" } }),
}));

vi.mock("@silieco/core/paths", () => ({
  useCurrentWorkspace: () => ({ repos: [] }),
}));

vi.mock("../../platform", () => ({
  isDesktopShell: () => true,
  pickDirectory: vi.fn(),
  useLocalDaemonStatus: () => mocks.daemon,
  validateLocalDirectory: vi.fn(),
}));

vi.mock("../../runtimes/components/runtime-machines", () => ({
  buildRuntimeMachines: () => mocks.machines,
}));

import { ProjectResourcesSection } from "./project-resources-section";

function localResource(
  daemonId: string,
  repositoryResourceId: string | null = "repository-1",
): ProjectResource {
  return {
    id: "resource-1",
    project_id: "project-1",
    workspace_id: "ws-1",
    resource_type: "local_directory",
    resource_ref: {
      daemon_id: daemonId,
      local_path: "/Users/ryan/mycode/VideoHub",
      ...(repositoryResourceId
        ? { repository_resource_id: repositoryResourceId }
        : {}),
      label: "VideoHub",
    },
    label: null,
    position: 0,
    created_at: new Date(0).toISOString(),
    created_by: "user-ryan",
  };
}

function repositoryResource(): ProjectResource {
  return {
    id: "repository-1",
    project_id: "project-1",
    workspace_id: "ws-1",
    resource_type: "github_repo",
    resource_ref: {
      url: "https://github.com/example/VideoHub",
      primary: true,
    },
    label: null,
    position: 0,
    created_at: new Date(0).toISOString(),
    created_by: "user-ryan",
  };
}

function agent(overrides: Partial<Agent> & Pick<Agent, "id" | "name" | "runtime_id">): Agent {
  return {
    workspace_id: "ws-1",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    permission_mode: "public_to",
    invocation_targets: [],
    status: "idle",
    max_concurrent_runs: 1,
    model: "",
    archived_at: null,
    archived_by: null,
    ...overrides,
  } as Agent;
}

function renderSection() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ProjectResourcesSection projectId="project-1" />
    </I18nProvider>,
  );
}

describe("ProjectResourcesSection local execution directory", () => {
  beforeEach(() => {
    mocks.resources = [];
    mocks.agents = [];
    mocks.machines = [];
    mocks.daemon.daemonId = "daemon-local";
    mocks.daemon.deviceName = "This Mac";
    mocks.daemon.running = true;
  });

  it("shows ownership, machine, status, scope, and hides a foreign absolute path", () => {
    mocks.resources = [repositoryResource(), localResource("daemon-ryan")];
    mocks.agents = [
      agent({ id: "agent-1", name: "Frontend Agent", runtime_id: "runtime-1" }),
    ];
    mocks.machines = [
      {
        daemonId: "daemon-ryan",
        title: "Ryan's Mac",
        health: "online",
        isCurrent: false,
        runtimes: [{ id: "runtime-1", owner_id: "user-ryan" }],
      },
    ];

    renderSection();

    expect(screen.getByText("VideoHub")).toBeInTheDocument();
    expect(screen.getByText("Ryan")).toBeInTheDocument();
    expect(screen.getByText("Ryan's Mac")).toBeInTheDocument();
    expect(screen.getByText("Local execution directory")).toBeInTheDocument();
    expect(screen.getByText("Online")).toBeInTheDocument();
    expect(
      screen.getByText(/Frontend Agent use this directory/),
    ).toBeInTheDocument();
    expect(screen.getByText(/aren't synced to the workspace/)).toBeInTheDocument();
    expect(
      screen.queryByText("/Users/ryan/mycode/VideoHub"),
    ).not.toBeInTheDocument();
  });

  it("shows the absolute path only on the directory's own machine", () => {
    mocks.daemon.daemonId = "daemon-ryan";
    mocks.resources = [repositoryResource(), localResource("daemon-ryan")];
    mocks.machines = [
      {
        daemonId: "daemon-ryan",
        title: "Ryan's Mac",
        health: "online",
        isCurrent: true,
        runtimes: [{ id: "runtime-1", owner_id: "user-ryan" }],
      },
    ];

    renderSection();

    expect(screen.getByText("/Users/ryan/mycode/VideoHub")).toBeInTheDocument();
  });

  it("allows a project without Git to configure a standalone working directory", () => {
    mocks.resources = [];
    mocks.machines = [
      {
        daemonId: "daemon-local",
        title: "Ryan's Mac",
        health: "online",
        isCurrent: true,
        runtimes: [{ id: "runtime-1", owner_id: "user-ryan" }],
      },
    ];

    renderSection();

    expect(screen.getByText("Project working directory")).toBeInTheDocument();
    expect(
      screen.getByText("Add local execution directory"),
    ).toBeInTheDocument();
  });

  it("keeps a legacy standalone local directory visible", () => {
    mocks.resources = [localResource("daemon-local", null)];
    mocks.machines = [
      {
        daemonId: "daemon-local",
        title: "Ryan's Mac",
        health: "online",
        isCurrent: true,
        runtimes: [{ id: "runtime-1", owner_id: "user-ryan" }],
      },
    ];

    renderSection();

    expect(screen.getByText("Project working directory")).toBeInTheDocument();
    expect(screen.getByText("/Users/ryan/mycode/VideoHub")).toBeInTheDocument();
  });
});
