import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { useEffect } from "react";

// SILI-4741: the adapter mutates tab sessions in the REAL store (no router,
// no mocks needed for it anymore) — the Coordinator, not tested here, is
// what projects sessions into the single router. Overlay and auth stay
// mocked so we can spy on their entry points.

const overlay = vi.hoisted(() => ({
  overlay: null as null | { type: string },
  open: vi.fn(),
  close: vi.fn(),
}));

vi.mock("@/stores/window-overlay-store", () => ({
  useWindowOverlayStore: Object.assign(() => null, {
    getState: () => overlay,
  }),
}));

const auth = vi.hoisted(() => ({ logout: vi.fn() }));

const committedLocation = vi.hoisted(() => {
  let url = "/acme/tasks";
  const listeners = new Set<() => void>();
  return {
    get: () => url,
    set(next: string) {
      url = next;
      listeners.forEach((listener) => listener());
    },
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    reset() {
      url = "/acme/tasks";
      listeners.clear();
    },
  };
});

vi.mock("@/platform/tab-coordinator", () => ({
  getAppRouterLocation: committedLocation.get,
  subscribeAppRouterLocation: committedLocation.subscribe,
}));

vi.mock("@silieco/core/auth", () => ({
  useAuthStore: Object.assign(() => null, {
    getState: () => auth,
  }),
}));

import { DesktopNavigationProvider, routeContentLinkPath } from "./navigation";
import { useNavigation } from "@silieco/views/navigation";
import { useTabStore, getActiveTab } from "@/stores/tab-store";

beforeEach(() => {
  overlay.open.mockReset();
  overlay.close.mockReset();
  overlay.overlay = null;
  auth.logout.mockReset();
  committedLocation.reset();
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme"); // default tab /acme/tasks
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: {
      runtimeConfig: { ok: true, config: { appUrl: "https://app.example" } },
    },
  });
});

function captureAdapter(onAdapter: (adapter: ReturnType<typeof useNavigation>) => void) {
  function Probe() {
    const nav = useNavigation();
    useEffect(() => {
      onAdapter(nav);
    }, [nav]);
    return null;
  }
  return Probe;
}

function renderProvider() {
  let adapter: ReturnType<typeof useNavigation> | null = null;
  const Probe = captureAdapter((a) => {
    adapter = a;
  });
  render(
    <DesktopNavigationProvider>
      <Probe />
    </DesktopNavigationProvider>,
  );
  return () => adapter!;
}

function acmeGroup() {
  return useTabStore.getState().byWorkspace.acme;
}

describe("openInNewTab", () => {
  it("opens a background tab (active tab unchanged) for a same-workspace path", () => {
    const getAdapter = renderProvider();
    const activeBefore = acmeGroup().activeTabId;

    getAdapter().openInNewTab!("/acme/agents", "Agents");

    const group = acmeGroup();
    expect(group.tabs.map((t) => t.url)).toEqual(["/acme/tasks", "/acme/agents"]);
    expect(group.activeTabId).toBe(activeBefore);
  });

  it("activates the new tab when opts.activate is true (foreground)", () => {
    const getAdapter = renderProvider();

    getAdapter().openInNewTab!("/acme/agents", "Agents", { activate: true });

    const group = acmeGroup();
    const agents = group.tabs.find((t) => t.url === "/acme/agents")!;
    expect(group.activeTabId).toBe(agents.id);
  });

  it("delegates to switchWorkspace for a cross-workspace path", () => {
    const getAdapter = renderProvider();

    getAdapter().openInNewTab!("/butter/inbox");

    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("butter");
    expect(getActiveTab(s)?.url).toBe("/butter/inbox");
    // acme's group is untouched.
    expect(s.byWorkspace.acme.tabs).toHaveLength(1);
  });
});

describe("push", () => {
  it("navigates the active session in-tab (url + virtual history)", () => {
    const getAdapter = renderProvider();

    getAdapter().push("/acme/projects?sort=name");

    const active = getActiveTab(useTabStore.getState())!;
    expect(active.url).toBe("/acme/projects?sort=name");
    expect(active.history).toEqual({
      stack: ["/acme/tasks", "/acme/projects?sort=name"],
      index: 1,
    });
  });

  it("keeps page-visible location on the committed route while navigation is pending", () => {
    committedLocation.set("/acme/projects/project-1?run=run-1");
    const getAdapter = renderProvider();

    getAdapter().push("/acme/tasks/TDP-18");

    expect(getActiveTab(useTabStore.getState())?.url).toBe(
      "/acme/tasks/TDP-18",
    );
    expect(getAdapter().pathname).toBe("/acme/projects/project-1");
    expect(getAdapter().searchParams.get("run")).toBe("run-1");
  });

  it("is a no-op when the target exactly matches the active session url", () => {
    const getAdapter = renderProvider();
    const before = acmeGroup();

    getAdapter().push("/acme/tasks");

    expect(acmeGroup()).toBe(before);
  });

  it("switches workspace for a cross-workspace path", () => {
    const getAdapter = renderProvider();

    getAdapter().push("/butter/inbox");

    const s = useTabStore.getState();
    expect(s.activeWorkspaceSlug).toBe("butter");
    expect(getActiveTab(s)?.url).toBe("/butter/inbox");
  });

  it("logs out instead of navigating for /login", () => {
    const getAdapter = renderProvider();

    getAdapter().push("/login");

    expect(auth.logout).toHaveBeenCalledOnce();
    expect(getActiveTab(useTabStore.getState())?.url).toBe("/acme/tasks");
  });

  it("routes transition paths to the window overlay without touching sessions", () => {
    const getAdapter = renderProvider();
    const before = acmeGroup();

    getAdapter().push("/workspaces/new");

    expect(overlay.open).toHaveBeenCalledWith({ type: "new-workspace" });
    expect(acmeGroup()).toBe(before);
  });
});

describe("push with pinned active tab", () => {
  function pinActive() {
    const store = useTabStore.getState();
    store.togglePin(acmeGroup().activeTabId);
  }

  it("redirects push to a new foreground tab when pathname differs", () => {
    pinActive();
    const getAdapter = renderProvider();
    const pinnedId = acmeGroup().activeTabId;

    getAdapter().push("/acme/projects");

    const group = acmeGroup();
    const pinned = group.tabs.find((t) => t.id === pinnedId)!;
    const projects = group.tabs.find((t) => t.url === "/acme/projects")!;
    // The pinned tab stays parked on its url; focus follows the new tab.
    expect(pinned.url).toBe("/acme/tasks");
    expect(group.activeTabId).toBe(projects.id);
  });

  it("allows in-tab navigation when only search/hash changes (RFC §3 D2b)", () => {
    pinActive();
    const getAdapter = renderProvider();

    getAdapter().push("/acme/tasks?filter=open");

    const group = acmeGroup();
    expect(group.tabs).toHaveLength(1); // no new tab
    expect(group.tabs[0].url).toBe("/acme/tasks?filter=open");
  });

  it("leaves cross-workspace push to the workspace switcher (not pin)", () => {
    pinActive();
    const getAdapter = renderProvider();

    getAdapter().push("/butter/inbox");

    expect(useTabStore.getState().activeWorkspaceSlug).toBe("butter");
    // No extra tab was opened in acme by the pin interception.
    expect(useTabStore.getState().byWorkspace.acme.tabs).toHaveLength(1);
  });
});

describe("back", () => {
  it("moves the session's virtual history backwards", () => {
    const getAdapter = renderProvider();
    getAdapter().push("/acme/projects");

    getAdapter().back!();

    const active = getActiveTab(useTabStore.getState())!;
    expect(active.url).toBe("/acme/tasks");
    expect(active.history.index).toBe(0);
  });
});

// Consumed by `useBackOrReplace` — a page whose subject was deleted steps back
// only when this says there is somewhere to step back to.
describe("canGoBack", () => {
  it("is false on a tab sitting at the start of its history", () => {
    const getAdapter = renderProvider();

    expect(getActiveTab(useTabStore.getState())!.history.index).toBe(0);
    expect(getAdapter().canGoBack!()).toBe(false);
  });

  it("is true once the tab has navigated in place", () => {
    const getAdapter = renderProvider();

    getAdapter().push("/acme/projects");

    expect(getActiveTab(useTabStore.getState())!.history.index).toBe(1);
    expect(getAdapter().canGoBack!()).toBe(true);
  });

  it("is false again after stepping back to the start", () => {
    const getAdapter = renderProvider();
    getAdapter().push("/acme/projects");

    getAdapter().back!();

    expect(getAdapter().canGoBack!()).toBe(false);
  });

  it("is false for a freshly opened tab, which starts its own history", () => {
    const getAdapter = renderProvider();
    getAdapter().push("/acme/projects");

    getAdapter().openInNewTab!("/acme/agents", "Agents", { activate: true });

    expect(getAdapter().canGoBack!()).toBe(false);
  });
});

describe("routeContentLinkPath (links inside content — SILI-5208)", () => {
  it("opens a same-workspace path in a foreground tab of its own", () => {
    routeContentLinkPath("/acme/tasks/SILI-1");

    const group = useTabStore.getState().byWorkspace.acme;
    const opened = group.tabs.find((t) => t.url === "/acme/tasks/SILI-1")!;
    expect(opened).toBeDefined();
    expect(group.activeTabId).toBe(opened.id);
  });

  it("switches workspace instead of opening the path under the active group", () => {
    routeContentLinkPath("/butter/tasks/BUT-1");

    const state = useTabStore.getState();
    expect(state.activeWorkspaceSlug).toBe("butter");
    expect(
      state.byWorkspace.acme.tabs.some((t) => t.url === "/butter/tasks/BUT-1"),
    ).toBe(false);
  });
});
