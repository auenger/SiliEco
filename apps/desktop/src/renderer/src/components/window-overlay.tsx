import { useQuery } from "@tanstack/react-query";
import { WorkspacesPage } from "@silieco/views/workspace/workspaces-page";
import { NewWorkspacePage } from "@silieco/views/workspace/new-workspace-page";
import { InvitePage } from "@silieco/views/invite";
import { InvitationsPage } from "@silieco/views/invitations";
import { OnboardingFlow } from "@silieco/views/onboarding";
import { useNavigation } from "@silieco/views/navigation";
import { paths } from "@silieco/core/paths";
import { workspaceListOptions } from "@silieco/core/workspace/queries";
import { useWindowOverlayStore } from "@/stores/window-overlay-store";

/**
 * Window-level transition overlay: renders above the tab system when the
 * user is in a pre-workspace flow (onboarding, create workspace, accept
 * invite).
 *
 * This component is intentionally thin — just a fixed positioning shell
 * that covers the tab system. It does NOT hide traffic lights or provide
 * a drag strip: each contained view (OnboardingFlow, NewWorkspacePage,
 * InvitePage) renders its own `<DragStrip />` as a flex-child at top so
 * native macOS traffic lights stay visible and the page content can fill
 * the window edge-to-edge. This matches the Linear/Notion/Arc pattern for
 * pre-dashboard flows and keeps platform chrome consistent across every
 * "not-in-dashboard" surface.
 *
 * All UX affordances (Back button, Log out button, welcome copy, invite
 * card) live inside the shared view components under `packages/views/`,
 * so web and desktop render identical content.
 */
export function WindowOverlay() {
  const overlay = useWindowOverlayStore((s) => s.overlay);
  if (!overlay) return null;
  return <WindowOverlayInner />;
}

function WindowOverlayInner() {
  const overlay = useWindowOverlayStore((s) => s.overlay);
  const close = useWindowOverlayStore((s) => s.close);
  const { push } = useNavigation();
  const { data: wsList = [] } = useQuery(workspaceListOptions());
  if (!overlay) return null;

  // Existing members can return to their tabs; creation also supports the waiting page.
  const onBack = wsList.length > 0 ? close : undefined;

  return (
    <div className="fixed inset-0 z-50 flex flex-col overflow-auto bg-background">
      {overlay.type === "workspaces" && <WorkspacesPage />}
      {overlay.type === "new-workspace" && (
        <NewWorkspacePage
          onSuccess={(ws) => push(paths.workspace(ws.slug).tasks())}
          onBack={onBack ?? (() => push(paths.workspaces()))}
        />
      )}
      {overlay.type === "invite" && (
        <InvitePage
          invitationId={overlay.invitationId}
          onBack={onBack}
        />
      )}
      {overlay.type === "invitations" && <InvitationsPage />}
      {overlay.type === "onboarding" && (
        <OnboardingFlow
          onComplete={(ws, taskId) => {
            close();
            // Runtime-connected onboarding lands on its single guide
            // task. Runtime-less exits still land on the tasks list.
            if (ws && taskId) {
              push(paths.workspace(ws.slug).taskDetail(taskId));
            } else if (ws) {
              push(paths.workspace(ws.slug).tasks());
            } else {
              push(paths.workspaces());
            }
          }}
          onRuntimeProbe={() => window.daemonAPI.probeRuntimes()}
          // Restart the bundled daemon when the user hits Refresh on
          // Step 3. The daemon's PATH probe runs once at boot, so a
          // newly-installed CLI (Claude / Codex / Cursor) doesn't show
          // up until the daemon is bounced.
          onRuntimeRefresh={async () => {
            await window.daemonAPI?.restart?.();
          }}
        />
      )}
    </div>
  );
}
