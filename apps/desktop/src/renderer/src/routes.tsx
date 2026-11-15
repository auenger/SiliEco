import { useEffect, lazy, Suspense } from "react";
import {
  createMemoryRouter,
  Outlet,
  useMatches,
  useParams,
} from "react-router-dom";
import type { RouteObject } from "react-router-dom";
import { TaskDetailPage } from "./pages/task-detail-page";
import { ProjectFilesRoute } from "./pages/project-files-page";
import { ProjectDetailPage } from "./pages/project-detail-page";
import { AutopilotDetailPage } from "./pages/autopilot-detail-page";
import { SkillDetailPage } from "./pages/skill-detail-page";
import { AgentDetailPage } from "./pages/agent-detail-page";
import { MemberDetailPage } from "./pages/member-detail-page";
import {
  RuntimeDetailPage,
  RuntimeSettingsPage,
} from "./pages/runtime-detail-page";
import { AttachmentPreviewRoute } from "./pages/attachment-preview-page";
import { InlineHtmlPreviewRoute } from "./pages/inline-html-preview-page";
import { TasksPage } from "@silieco/views/tasks/components";
import { ProjectsPage } from "@silieco/views/projects/components";
import { WorkflowsPage } from "@silieco/views/workflows/components";
import { DashboardPage } from "@silieco/views/dashboard";
import { AutopilotsPage } from "@silieco/views/autopilots/components";
import { MyTasksPage } from "@silieco/views/my-tasks";
import { SkillsPage } from "@silieco/views/skills";
import { DesktopRuntimesPage } from "./components/desktop-runtimes-page";
import { DesktopAgentsPage } from "./components/desktop-agents-page";
import { AgentCreationStudio } from "@silieco/views/agents";
import { SquadsPage, SquadDetailPage as SquadDetailPageView } from "@silieco/views/squads/components";
import { InboxPage } from "@silieco/views/inbox";
import { ChatPage } from "@silieco/views/chat";
import { SettingsPage } from "@silieco/views/settings";
import { ResourcesPage } from "@silieco/views/resources";
import { useT } from "@silieco/views/i18n";
import { Download, Server } from "lucide-react";
import { DaemonSettingsTab } from "./components/daemon-settings-tab";
import { UpdatesSettingsTab } from "./components/updates-settings-tab";
import { WorkspaceRouteLayout } from "./components/workspace-route-layout";
import { DesktopRouteErrorPage } from "./components/route-error-page";

const BrowserToolRoute = lazy(() => import("./platform/browser-tool").then((module) => ({ default: module.BrowserToolRoute })));

/**
 * Wraps `SettingsPage` so the desktop-only extra tabs can pull their labels
 * from i18n. The route element has to be a component (not a literal JSX
 * value) for `useT` to run.
 */
function DesktopSettingsRoute() {
  const { t } = useT("settings");
  return (
    <SettingsPage
      extraAccountTabs={[
        {
          value: "daemon",
          label: "Daemon",
          icon: Server,
          content: <DaemonSettingsTab />,
        },
        {
          value: "updates",
          label: t(($) => $.desktop.tabs.updates),
          icon: Download,
          content: <UpdatesSettingsTab />,
        },
      ]}
    />
  );
}

/**
 * Sets document.title from the deepest matched route's handle.title.
 * The tab system observes document.title via MutationObserver.
 * Pages with dynamic titles (e.g. task detail) override by setting
 * document.title directly via useDocumentTitle().
 */
function TitleSync() {
  const matches = useMatches();
  const title = [...matches]
    .reverse()
    .find((m) => (m.handle as { title?: string })?.title)
    ?.handle as { title?: string } | undefined;

  useEffect(() => {
    if (title?.title) document.title = title.title;
  }, [title?.title]);

  return null;
}

/** Wrapper that renders route children + TitleSync */
function PageShell() {
  return (
    <>
      <TitleSync />
      <Outlet />
    </>
  );
}

function WorkflowRoutePage() {
  const { id } = useParams<{ id: string }>();
  return id ? <WorkflowsPage workflowId={id} /> : null;
}

/**
 * Route definitions shared by all tabs.
 *
 * Every tab path is workspace-scoped: `/{slug}/{route}/...`. Pre-workspace
 * flows (create workspace, accept invite) are NOT routes — they render as a
 * window-level overlay via `WindowOverlay`, dispatched by the navigation
 * adapter's transition-path interception. The `activeWorkspaceSlug` in the
 * tab store decides which workspace's tabs are visible in the TabBar;
 * workspace-less state (zero-workspace user) shows the overlay instead.
 *
 * The root index route stays as a harmless safety net. With per-workspace
 * tabs, nothing should construct a tab at `/` — but if one ever slips
 * through (malformed persisted state that dodges the migration, direct
 * router.navigate from unforeseen code), the index falls back to null
 * rather than 404; App.tsx's bootstrap repoints activeWorkspaceSlug on the
 * next render pass.
 */
export const appRoutes: RouteObject[] = [
  {
    element: <PageShell />,
    errorElement: <DesktopRouteErrorPage />,
    children: [
      { index: true, element: null },
      {
        path: ":workspaceSlug",
        element: <WorkspaceRouteLayout />,
        children: [
          // A bare `/{slug}` URL is normalized to `/{slug}/tasks` by
          // sanitizeTabPath before it ever becomes a session, so the index
          // route is unreachable in practice; null keeps it a harmless
          // safety net instead of an in-router <Navigate> (SILI-4741
          // invariant 1: the router never self-navigates).
          { index: true, element: null },
          { path: "browser/:sessionId", element: <Suspense fallback={<div className="flex-1 bg-background" />}><BrowserToolRoute /></Suspense> },
          {
            path: "tasks",
            element: <TasksPage />,
            handle: { title: "Tasks" },
          },
          {
            path: "tasks/:id",
            element: <TaskDetailPage />,
            handle: { title: "Task" },
          },


          {
            path: "projects",
            element: <ProjectsPage />,
            handle: { title: "Projects" },
          },
          {
            path: "projects/:id",
            element: <ProjectDetailPage />,
            handle: { title: "Project" },
          },
          {
            path: "projects/:id/files",
            element: <ProjectFilesRoute />,
            handle: { title: "Files" },
          },
          {
            path: "resources",
            element: <ResourcesPage />,
            handle: { title: "Resources" },
          },
          {
            path: "workflows",
            element: <WorkflowsPage />,
            handle: { title: "Workflows" },
          },
          {
            path: "workflows/:id",
            element: <WorkflowRoutePage />,
            handle: { title: "Workflow" },
          },
          {
            path: "autopilots",
            element: <AutopilotsPage />,
            handle: { title: "Autopilot" },
          },
          {
            path: "autopilots/:id",
            element: <AutopilotDetailPage />,
            handle: { title: "Autopilot" },
          },
          {
            path: "my-tasks",
            element: <MyTasksPage />,
            handle: { title: "My Tasks" },
          },

          {
            path: "runtimes",
            element: <DesktopRuntimesPage />,
            handle: { title: "Runtimes" },
          },
          {
            path: "runtimes/:id",
            element: <RuntimeDetailPage />,
            handle: { title: "Machine" },
          },
          {
            path: "runtimes/:id/runtime/:runtimeId",
            element: <RuntimeSettingsPage />,
            handle: { title: "Runtime" },
          },
          { path: "skills", element: <SkillsPage />, handle: { title: "Skills" } },
          {
            path: "skills/:id",
            element: <SkillDetailPage />,
            handle: { title: "Skill" },
          },
          { path: "agents", element: <DesktopAgentsPage />, handle: { title: "Agents" } },
          { path: "agents/new", element: <AgentCreationStudio />, handle: { title: "Create Agent" } },
          {
            path: "agents/:id",
            element: <AgentDetailPage />,
            handle: { title: "Agent" },
          },
          {
            path: "members/:id",
            element: <MemberDetailPage />,
            handle: { title: "Member" },
          },
          { path: "squads", element: <SquadsPage />, handle: { title: "Squads" } },
          {
            path: "squads/:id",
            element: <SquadDetailPageView />,
            handle: { title: "Squad" },
          },
          { path: "inbox", element: <InboxPage />, handle: { title: "Inbox" } },
          { path: "chat", element: <ChatPage />, handle: { title: "Chat" } },
          {
            path: "attachments/:id/preview",
            element: <AttachmentPreviewRoute />,
            handle: { title: "Attachment" },
          },
          {
            path: "html-previews/:id",
            element: <InlineHtmlPreviewRoute />,
            handle: { title: "HTML Preview" },
          },
          {
            path: "usage",
            element: <DashboardPage />,
            handle: { title: "Usage" },
          },
          {
            path: "settings",
            element: <DesktopSettingsRoute />,
            handle: { title: "Settings" },
          },
        ],
      },
    ],
  },
];

/**
 * Create THE app router (SILI-4741 single-router session architecture).
 * There is exactly one instance, owned by the tab Coordinator; it projects
 * the active tab session's URL and is never navigated by anything else.
 */
export function createAppRouter() {
  return createMemoryRouter(appRoutes, {
    initialEntries: ["/"],
  });
}
