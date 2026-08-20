import { act, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";

const routerLocation = vi.hoisted(() => {
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

vi.mock("react-router-dom", async (importOriginal) => {
  const actual = await importOriginal<typeof import("react-router-dom")>();
  return {
    ...actual,
    RouterProvider: () => <div data-testid="committed-router-page" />,
  };
});

vi.mock("@/platform/tab-coordinator", () => ({
  createScrollRestorationAdapter: () => ({ get: () => undefined }),
  getAppRouter: () => ({}),
  getAppRouterLocation: routerLocation.get,
  initTabCoordinator: vi.fn(),
  registerActiveHostElement: vi.fn(),
  registerCoordinatorQueryClient: vi.fn(),
  subscribeAppRouterLocation: routerLocation.subscribe,
}));

import { useTabStore } from "@/stores/tab-store";
import { TabContent } from "./tab-content";

beforeEach(() => {
  routerLocation.reset();
  useTabStore.getState().reset();
  useTabStore.getState().switchWorkspace("acme");
});

describe("TabContent route alignment", () => {
  it("does not render the previous router page in a tab awaiting its target route", () => {
    const queryClient = new QueryClient();
    render(
      <QueryClientProvider client={queryClient}>
        <TabContent />
      </QueryClientProvider>,
    );
    expect(screen.getByTestId("committed-router-page")).toBeInTheDocument();

    act(() => {
      useTabStore
        .getState()
        .navigateActiveSession("/acme/tasks/TDP-18");
    });

    expect(screen.getByTestId("tab-route-pending")).toBeInTheDocument();
    expect(screen.queryByTestId("committed-router-page")).toBeNull();

    act(() => routerLocation.set("/acme/tasks/TDP-18"));

    expect(screen.queryByTestId("tab-route-pending")).toBeNull();
    expect(screen.getByTestId("committed-router-page")).toBeInTheDocument();
  });
});
