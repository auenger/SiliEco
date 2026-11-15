import { useEffect, useMemo } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import {
  bucketDiagnosticPath,
  setDiagnosticRoute,
} from "@silieco/core/diagnostics";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "@silieco/views/navigation";
import { parseTaskWindowPath } from "../../../shared/task-window";

/**
 * Answer the `silieco:navigate` event inside a dedicated task window (SILI-5208).
 *
 * The event is what a link in content (comment, description) fires once it
 * resolves to an in-app destination, including an absolute URL on this
 * deployment's own origin. Only the main shell listened for it, so in this
 * window such a click did nothing at all.
 *
 * Another task opens in place — the same thing a mention chip does here, since
 * the window's adapter push is `navigateToTask`. Any other app page cannot be
 * hosted by this single-route window, so it goes to the browser rather than
 * being swallowed.
 */
function useContentLinkHandler(
  navigate: ReturnType<typeof useNavigate>,
  runtimeConfig: typeof window.desktopAPI.runtimeConfig,
) {
  useEffect(() => {
    const handler = (e: Event) => {
      const path = (e as CustomEvent<{ path?: string }>).detail?.path;
      if (!path) return;
      const taskPath = parseTaskWindowPath(path);
      if (taskPath) {
        void navigate(taskPath.path);
        return;
      }
      if (!runtimeConfig.ok) return;
      void window.desktopAPI.openExternal(
        `${runtimeConfig.config.appUrl}${path}`,
      );
    };
    window.addEventListener("silieco:navigate", handler);
    return () => window.removeEventListener("silieco:navigate", handler);
  }, [navigate, runtimeConfig]);
}

/**
 * Navigation bridge for a dedicated task window. Unlike the main Desktop
 * shell, this window owns a tiny MemoryRouter and intentionally accepts only
 * task-detail routes. Keeping the bridge in the platform layer preserves the
 * SILI-4741 boundary around direct router navigation.
 */
export function TaskWindowNavigationProvider({
  children,
}: {
  children: React.ReactNode;
}) {
  const location = useLocation();
  const navigate = useNavigate();
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  const currentPath = `${location.pathname}${location.search}${location.hash}`;

  useEffect(() => {
    // Both freeze observers need the route: main for a hang this window never
    // returns from, the in-renderer watchdog for one it survives (its
    // `location.pathname` is the packaged index.html path).
    // Bucketed template only — this payload ends up in a freeze report, so the
    // workspace slug and the task id must not travel with it.
    const bucketed = bucketDiagnosticPath(currentPath);
    setDiagnosticRoute(bucketed);
    window.desktopAPI.setRendererRouteContext({
      surface: "tab",
      path: bucketed,
    });
  }, [currentPath]);

  useContentLinkHandler(navigate, runtimeConfig);

  const adapter = useMemo<NavigationAdapter>(() => {
    const navigateToTask = (path: string, replace = false) => {
      const taskPath = parseTaskWindowPath(path);
      if (!taskPath) return;
      void navigate(taskPath.path, { replace });
    };

    return {
      push: (path) => navigateToTask(path),
      replace: (path) => navigateToTask(path, true),
      back: () => void navigate(-1),
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      openInNewTab: (path, title) => {
        void window.desktopAPI.openTaskWindow({
          path,
          title: title ?? "Task",
        });
      },
      getShareableUrl: (path) =>
        runtimeConfig.ok ? `${runtimeConfig.config.appUrl}${path}` : path,
    };
  }, [location.pathname, location.search, navigate, runtimeConfig]);

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}
