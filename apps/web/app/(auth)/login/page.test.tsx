import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@silieco/core/i18n/react";
import enCommon from "@silieco/views/locales/en/common.json";
import enAuth from "@silieco/views/locales/en/auth.json";
import enSettings from "@silieco/views/locales/en/settings.json";
import type { ReactNode } from "react";

const TEST_RESOURCES = {
  en: { common: enCommon, auth: enAuth, settings: enSettings },
};

function createWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

const {
  mockPasswordAuth,
  mockMintCliToken,
  mockListWorkspaces,
  mockListMyInvitations,
  mockPush,
  mockReplace,
  searchParamsState,
  authStateRef,
} = vi.hoisted(() => ({
  mockPasswordAuth: vi.fn(),
  mockMintCliToken: vi.fn(),
  mockListWorkspaces: vi.fn(),
  mockListMyInvitations: vi.fn(),
  mockPush: vi.fn(),
  mockReplace: vi.fn(),
  searchParamsState: { params: new URLSearchParams() },
  authStateRef: {
    state: {
      passwordAuth: vi.fn(),
      user: null as null | { id: string; email: string; onboarded_at?: string | null },
      isLoading: false,
    },
  },
}));

// Mock next/navigation — router spies are hoisted so tests can assert
// which navigation (if any) the page taskd.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  usePathname: () => "/login",
  useSearchParams: () => searchParamsState.params,
}));

// Mock auth store — shared LoginPage uses getState().passwordAuth,
// web wrapper uses useAuthStore((s) => s.user/isLoading). Keep the real
// sanitizeNextUrl so the redirect-sanitization rules are exercised rather
// than silently drifting behind a mock reimplementation.
vi.mock("@silieco/core/auth", async () => {
  const actual =
    await vi.importActual<typeof import("@silieco/core/auth")>(
      "@silieco/core/auth",
    );
  authStateRef.state.passwordAuth = mockPasswordAuth;
  const useAuthStore = Object.assign(
    (selector: (s: typeof authStateRef.state) => unknown) =>
      selector(authStateRef.state),
    { getState: () => authStateRef.state },
  );
  return { ...actual, useAuthStore };
});

// Mock auth-cookie
vi.mock("@/features/auth/auth-cookie", () => ({
  setLoggedInCookie: vi.fn(),
}));

// Mock api
vi.mock("@silieco/core/api", () => ({
  api: {
    listWorkspaces: mockListWorkspaces,
    listMyInvitations: mockListMyInvitations,
    verifyCode: vi.fn(),
    setToken: vi.fn(),
    getMe: vi.fn(),
    mintCliToken: mockMintCliToken,
  },
}));

import LoginPage from "./page";

describe("LoginPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    searchParamsState.params = new URLSearchParams();
    authStateRef.state.user = null;
    authStateRef.state.isLoading = false;
    mockListWorkspaces.mockResolvedValue([]);
    mockListMyInvitations.mockResolvedValue([]);
  });

  it("renders password login with Silieco account actions", () => {
    render(<LoginPage />, { wrapper: createWrapper() });

    expect(screen.getByText("Create or sign in to Silieco")).toBeInTheDocument();
    expect(
      screen.getByText("Sign in with email and password, or create a new account"),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "No account? Create one" }),
    ).toBeInTheDocument();
  });

  it("keeps password login disabled until both fields are valid", () => {
    render(<LoginPage />, { wrapper: createWrapper() });

    expect(screen.getByRole("button", { name: "Sign in" })).toBeDisabled();
    expect(mockPasswordAuth).not.toHaveBeenCalled();
  });

  it("calls passwordAuth in login mode on submit", async () => {
    mockPasswordAuth.mockResolvedValueOnce(undefined);
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText("Email"), "test@silieco.ai");
    await user.type(screen.getByLabelText("Password"), "secret12");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => {
      expect(mockPasswordAuth).toHaveBeenCalledWith(
        "login",
        "test@silieco.ai",
        "secret12",
      );
    });
  });

  it("shows a working state while password authentication is pending", async () => {
    mockPasswordAuth.mockReturnValueOnce(new Promise(() => {}));
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText("Email"), "test@silieco.ai");
    await user.type(screen.getByLabelText("Password"), "secret12");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => {
      expect(screen.getByText("Working...")).toBeInTheDocument();
    });
  });

  it("switches to registration and submits register mode", async () => {
    mockPasswordAuth.mockResolvedValueOnce(undefined);
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.click(
      screen.getByRole("button", { name: "No account? Create one" }),
    );
    await user.type(screen.getByLabelText("Email"), "test@silieco.ai");
    await user.type(screen.getByLabelText("Password"), "secret12");
    await user.click(screen.getByRole("button", { name: "Create account" }));

    await waitFor(() => {
      expect(mockPasswordAuth).toHaveBeenCalledWith(
        "register",
        "test@silieco.ai",
        "secret12",
      );
    });
  });

  it("shows an error when password authentication fails", async () => {
    mockPasswordAuth.mockRejectedValueOnce(new Error("Invalid credentials"));
    const user = userEvent.setup();
    render(<LoginPage />, { wrapper: createWrapper() });

    await user.type(screen.getByLabelText("Email"), "test@silieco.ai");
    await user.type(screen.getByLabelText("Password"), "secret12");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    await waitFor(() => {
      expect(screen.getByText("Invalid credentials")).toBeInTheDocument();
    });
  });

  // Regression: SILI-1080 — if the user is already authenticated on the web
  // and the Desktop app redirects them to /login?platform=desktop, the web
  // must exchange the cookie session for a bearer token and hand it off via
  // the silieco:// deep link, not silently redirect to the workspace page.
  it("mints a token and deep-links to Desktop when already logged in with platform=desktop", async () => {
    searchParamsState.params = new URLSearchParams({ platform: "desktop" });
    authStateRef.state.user = { id: "u1", email: "test@silieco.ai" };
    mockMintCliToken.mockImplementation(() =>
      Promise.resolve({ token: "handoff-jwt" }),
    );

    const hrefSetter = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...originalLocation, set href(value: string) { hrefSetter(value); } },
    });

    try {
      render(<LoginPage />, { wrapper: createWrapper() });

      await waitFor(() => {
        expect(mockMintCliToken).toHaveBeenCalledTimes(1);
      });
      await waitFor(() => {
        expect(hrefSetter).toHaveBeenCalledWith(
          "silieco://auth/callback?token=handoff-jwt",
        );
      });
      expect(
        await screen.findByRole("button", { name: "Open Silieco Desktop" }),
      ).toBeInTheDocument();
    } finally {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: originalLocation,
      });
    }
  });

  // Regression: #5009 — the "already authenticated on arrival" effect used to
  // fire for fresh form logins too. verifyCode writes `user` while handleVerify
  // is still fetching the workspace list, so the effect read an empty cache and
  // raced handleSuccess with replace("/workspaces/new"); depending on the
  // interleaving the user could end up stuck on the create-workspace page
  // despite having workspaces.
  describe("post-login redirect ownership (#5009)", () => {
    const onboardedUser = {
      id: "u1",
      email: "test@silieco.ai",
      onboarded_at: "2026-01-01T00:00:00Z",
    };

    it("does not redirect from the arrival effect when the user logs in via the form", async () => {
      // Auth settles as logged-out first — the page latches "any user from
      // now on came from the form".
      const wrapper = createWrapper();
      const { rerender } = render(<LoginPage />, { wrapper });
      // verifyCode set the user; the workspace list fetch is still in flight
      // (cache cold). The arrival effect must stay silent — handleSuccess
      // owns this navigation.
      authStateRef.state.user = onboardedUser;
      rerender(<LoginPage />);

      await act(async () => {});
      expect(mockReplace).not.toHaveBeenCalled();
      expect(mockPush).not.toHaveBeenCalled();
      expect(mockListWorkspaces).not.toHaveBeenCalled();
    });

    it("fetches the workspace list before redirecting a visitor who arrived authenticated", async () => {
      // Cold Query cache on a fresh page load: reading it would say "no
      // workspaces" and misroute to /workspaces/new. The effect must fetch.
      authStateRef.state.user = onboardedUser;
      mockListWorkspaces.mockResolvedValue([{ id: "ws-1", slug: "acme" }]);

      render(<LoginPage />, { wrapper: createWrapper() });

      await waitFor(() => {
        expect(mockReplace).toHaveBeenCalledWith("/acme/tasks");
      });
      expect(mockListWorkspaces).toHaveBeenCalledTimes(1);
    });

    it("still honors ?next= for a visitor who arrived authenticated", async () => {
      searchParamsState.params = new URLSearchParams({
        next: "/invite/abc",
      });
      authStateRef.state.user = onboardedUser;

      render(<LoginPage />, { wrapper: createWrapper() });

      await waitFor(() => {
        expect(mockReplace).toHaveBeenCalledWith("/invite/abc");
      });
      expect(mockListWorkspaces).not.toHaveBeenCalled();
    });
  });
});
