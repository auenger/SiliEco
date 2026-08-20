import { beforeEach, describe, expect, it, vi } from "vitest";

const routerHarness = vi.hoisted(() => {
  let subscriber: (() => void) | undefined;
  const state = {
    location: { pathname: "/acme/tasks", search: "", hash: "" },
  };
  const navigate = vi.fn((_target: string) =>
    new Promise<void>(() => {
      subscriber?.();
    }),
  );
  return {
    router: {
      state,
      navigate,
      subscribe: vi.fn((callback: () => void) => {
        subscriber = callback;
        return () => undefined;
      }),
    },
    reset() {
      state.location.pathname = "/acme/tasks";
      navigate.mockClear();
      subscriber = undefined;
    },
  };
});

vi.mock("@/routes", () => ({
  createAppRouter: () => routerHarness.router,
}));

import { useTabStore, getActiveTab } from "@/stores/tab-store";
import {
  __resetTabCoordinatorForTests,
  initTabCoordinator,
} from "./tab-coordinator";

beforeEach(() => {
  __resetTabCoordinatorForTests();
  useTabStore.getState().reset();
  routerHarness.reset();
  useTabStore.getState().switchWorkspace("acme");
});

describe("tab coordinator navigation deduplication", () => {
  it("does not restart a pending route when tab metadata changes", () => {
    initTabCoordinator();

    useTabStore
      .getState()
      .navigateActiveSession("/acme/tasks/TDP-18");
    expect(routerHarness.router.navigate).toHaveBeenCalledTimes(1);

    const active = getActiveTab(useTabStore.getState());
    expect(active).not.toBeNull();
    useTabStore.getState().updateTab(active!.id, { title: "TDP-18" });

    expect(routerHarness.router.navigate).toHaveBeenCalledTimes(1);
  });
});
