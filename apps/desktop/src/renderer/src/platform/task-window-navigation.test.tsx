/**
 * SILI-5208 — a dedicated task window must answer content-link navigation.
 *
 * `silieco:navigate` is fired by the shared link handler for every in-app
 * destination, including an absolute URL on this deployment's own origin. Only
 * the main shell listened for it, so a link clicked inside an task window did
 * nothing at all. This window hosts exactly one task route: another task opens
 * in place, anything else goes to the browser — never a silent no-op.
 */
import { describe, expect, it, vi, beforeEach } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";

const APP_URL = "https://app.example";

vi.mock("@silieco/core/paths", () => ({
  useCurrentWorkspace: () => ({ slug: "acme", id: "ws-1" }),
}));

import { TaskWindowNavigationProvider } from "./task-window-navigation";

const openExternal = vi.fn().mockResolvedValue(undefined);
const setRendererRouteContext = vi.fn();

beforeEach(() => {
  openExternal.mockClear();
  setRendererRouteContext.mockClear();
  Object.defineProperty(window, "desktopAPI", {
    configurable: true,
    value: {
      runtimeConfig: { ok: true, config: { appUrl: APP_URL } },
      openExternal,
      setRendererRouteContext,
      openTaskWindow: vi.fn(),
    },
  });
});

function CurrentPath() {
  const location = useLocation();
  return <span data-testid="path">{location.pathname}</span>;
}

function renderWindow() {
  return render(
    <MemoryRouter initialEntries={["/acme/tasks/SILI-1"]}>
      <Routes>
        <Route
          path=":workspaceSlug/tasks/:id"
          element={
            <TaskWindowNavigationProvider>
              <CurrentPath />
            </TaskWindowNavigationProvider>
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

function navigate(path: string) {
  // The listener runs outside React's event system; act() flushes the router
  // state update it triggers.
  act(() => {
    window.dispatchEvent(
      new CustomEvent("silieco:navigate", { detail: { path } }),
    );
  });
}

describe("TaskWindowNavigationProvider content links", () => {
  it("opens another task in place", () => {
    renderWindow();

    navigate("/acme/tasks/SILI-2");

    expect(screen.getByTestId("path")).toHaveTextContent("/acme/tasks/SILI-2");
    expect(openExternal).not.toHaveBeenCalled();
  });

  it("hands a page this window cannot host to the browser instead of swallowing it", () => {
    renderWindow();

    navigate("/acme/chat");

    expect(screen.getByTestId("path")).toHaveTextContent("/acme/tasks/SILI-1");
    expect(openExternal).toHaveBeenCalledWith(`${APP_URL}/acme/chat`);
  });

  it("stops listening once unmounted", () => {
    const { unmount } = renderWindow();

    unmount();
    navigate("/acme/chat");

    expect(openExternal).not.toHaveBeenCalled();
  });
});
