import { useEffect } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";

// vi.hoisted shared state for all the stores / hooks the layout consumes.
const state = vi.hoisted(() => ({
  user: null as { id: string } | null,
  isAuthLoading: false,
  overlay: null as { type: string } | null,
  workspace: null as { id: string; slug: string } | null,
  listFetched: true,
  wsList: [] as { id: string; slug: string }[],
  workspaceSeen: true,
  modalRenders: 0,
  modalAriaLabel: "source-backfill-modal-marker",
  currentSlug: null as string | null,
  childQuerySlugs: [] as (string | null)[],
}));

vi.mock("@silieco/core/auth", () => {
  const useAuthStore = (selector: (s: typeof state) => unknown) => {
    if (selector.toString().includes("isLoading"))
      return state.isAuthLoading;
    return state.user;
  };
  return { useAuthStore };
});

vi.mock("@silieco/core/platform", () => ({
  setCurrentWorkspace: vi.fn((slug: string | null) => {
    if (state.currentSlug === slug) return;
    state.currentSlug = slug;
  }),
  getCurrentSlug: () => state.currentSlug,
}));

vi.mock("@silieco/core/workspace", async () => {
  const actual = await vi.importActual<typeof import("@silieco/core/workspace")>(
    "@silieco/core/workspace",
  );
  return {
    ...actual,
    workspaceBySlugOptions: () => ({
      queryKey: ["workspace-by-slug"],
      queryFn: async () => state.workspace,
    }),
    workspaceListOptions: () => ({
      queryKey: ["workspace-list"],
      queryFn: async () => state.wsList,
    }),
  };
});

vi.mock("@silieco/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@silieco/core/paths")>(
    "@silieco/core/paths",
  );
  return {
    ...actual,
    WorkspaceSlugProvider: ({ children }: { children: React.ReactNode }) => (
      <>{children}</>
    ),
    paths: {
      ...actual.paths,
      login: () => "/login",
    },
  };
});

vi.mock("@silieco/views/workspace/use-workspace-seen", () => ({
  useWorkspaceSeen: () => state.workspaceSeen,
}));

vi.mock("@silieco/views/workspace/welcome-after-onboarding", () => ({
  WelcomeAfterOnboarding: () => null,
}));

vi.mock("@silieco/views/layout", () => ({
  WorkspacePresencePrefetch: () => null,
}));

// The point of this whole test: assert the desktop layout mounts the
// SourceBackfillModal. We stub the real component with a marker that
// renders only when the layout actually rendered it (and not e.g.
// suppressed by overlayActive).
vi.mock("@silieco/views/onboarding", () => ({
  SourceBackfillModal: () => {
    state.modalRenders += 1;
    return <div data-testid={state.modalAriaLabel} />;
  },
}));

vi.mock("@/stores/tab-store", () => ({
  useTabStore: Object.assign(() => null, {
    getState: () => ({ validateWorkspaceSlugs: vi.fn() }),
  }),
}));

vi.mock("@/stores/window-overlay-store", () => {
  const useWindowOverlayStore = (selector: (s: typeof state) => unknown) =>
    selector(state);
  return { useWindowOverlayStore };
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkspaceRouteLayout } from "./workspace-route-layout";

function renderLayout() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  // Seed the workspace queries so the gate inside the layout passes
  // synchronously — the real hook reads from cache.
  qc.setQueryData(["workspace-by-slug"], state.workspace);
  qc.setQueryData(["workspace-list"], state.wsList);
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/acme/issues"]}>
        <Routes>
          <Route path=":workspaceSlug/*" element={<WorkspaceRouteLayout />}>
            <Route path="*" element={<div data-testid="outlet" />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.user = { id: "u1" };
  state.isAuthLoading = false;
  state.overlay = null;
  state.workspace = { id: "ws-1", slug: "acme" };
  state.listFetched = true;
  state.wsList = [{ id: "ws-1", slug: "acme" }];
  state.workspaceSeen = true;
  state.modalRenders = 0;
  state.currentSlug = null;
  state.childQuerySlugs = [];
});

describe("WorkspaceRouteLayout across a tab swap", () => {
  function ChildQuery() {
    useEffect(() => {
      state.childQuerySlugs.push(state.currentSlug);
    }, []);
    return <div data-testid="outlet" />;
  }

  function TabHost({ slug }: { slug: string }) {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, gcTime: 0 } },
    });
    qc.setQueryData(["workspace-by-slug"], state.workspace);
    qc.setQueryData(["workspace-list"], state.wsList);
    return (
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={[`/${slug}/issues`]}>
          <Routes>
            <Route path=":workspaceSlug/*" element={<WorkspaceRouteLayout />}>
              <Route path="*" element={<ChildQuery />} />
            </Route>
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    );
  }

  it("keeps the singleton during a same-workspace tab swap", () => {
    const { rerender } = render(<TabHost key="tab-1" slug="acme" />);
    rerender(<TabHost key="tab-2" slug="acme" />);

    expect(state.currentSlug).toBe("acme");
    expect(state.childQuerySlugs).toEqual(["acme", "acme"]);
  });

  it("adopts a different workspace during a cross-workspace tab swap", () => {
    const { rerender } = render(<TabHost key="tab-1" slug="acme" />);
    state.workspace = { id: "ws-2", slug: "globex" };
    state.wsList = [
      { id: "ws-1", slug: "acme" },
      { id: "ws-2", slug: "globex" },
    ];
    rerender(<TabHost key="tab-2" slug="globex" />);

    expect(state.currentSlug).toBe("globex");
    expect(state.childQuerySlugs).toEqual(["acme", "globex"]);
  });

  it("releases the singleton when the last layout unmounts", () => {
    const { unmount } = render(<TabHost key="tab-1" slug="acme" />);
    unmount();
    expect(state.currentSlug).toBeNull();
  });
});

describe("WorkspaceRouteLayout", () => {
  it("mounts SourceBackfillModal when no WindowOverlay is active", () => {
    const { queryByTestId } = renderLayout();
    expect(queryByTestId(state.modalAriaLabel)).not.toBeNull();
    expect(state.modalRenders).toBeGreaterThan(0);
  });

  it("suppresses SourceBackfillModal while a WindowOverlay is active", () => {
    state.overlay = { type: "new-workspace" };
    const { queryByTestId } = renderLayout();
    expect(queryByTestId(state.modalAriaLabel)).toBeNull();
    expect(state.modalRenders).toBe(0);
  });
});
