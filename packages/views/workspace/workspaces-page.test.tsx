import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@silieco/core/i18n/react";
import en from "../locales/en/workspace.json";
const mocks = vi.hoisted(() => ({ logout: vi.fn(), replace: vi.fn(), workspaces: vi.fn(), invitations: vi.fn() }));
vi.mock("@silieco/core/auth", () => ({ useAuthStore: (select: (s: unknown) => unknown) => select({ user: { email: "test@example.invalid" } }) }));
vi.mock("@silieco/core/config", () => ({ useConfigStore: (select: (s: unknown) => unknown) => select({ workspaceCreationDisabled: false }) }));
vi.mock("../auth", () => ({ useLogout: () => mocks.logout }));
vi.mock("../navigation", () => ({ useNavigation: () => ({ replace: mocks.replace }), AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a> }));
vi.mock("@silieco/core/workspace/queries", () => ({ workspaceListOptions: () => ({ queryKey: ["workspaces"], queryFn: mocks.workspaces }), myInvitationListOptions: () => ({ queryKey: ["invitations"], queryFn: mocks.invitations }) }));
import { WorkspacesPage } from "./workspaces-page";
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><I18nProvider locale="en" resources={{ en: { workspace: en } }}><WorkspacesPage /></I18nProvider></QueryClientProvider>);
  return client;
}
beforeEach(() => { vi.clearAllMocks(); mocks.workspaces.mockResolvedValue([]); mocks.invitations.mockResolvedValue([]); });
it("allows an account with no workspaces to wait, create, or log out", async () => {
  const client = setup();
  expect(screen.getByText("test@example.invalid")).toBeInTheDocument();
  expect(screen.getByRole("link", { name: en.create_form.submit })).toHaveAttribute("href", "/workspaces/new");
  await waitFor(() => expect(mocks.workspaces).toHaveBeenCalled());
  expect(mocks.replace).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Log out" }));
  expect(mocks.logout).toHaveBeenCalledOnce();
  client.clear();
});
it("shows pending invitations and enters a workspace once membership appears", async () => {
  mocks.invitations.mockResolvedValue([{ id: "invite" }]);
  const client = setup();
  await waitFor(() => expect(screen.getByRole("link", { name: /invitation/i })).toHaveAttribute("href", "/invitations"));
  mocks.workspaces.mockResolvedValue([{ slug: "joined" }]);
  fireEvent.click(screen.getByRole("button", { name: en.empty.refresh }));
  await waitFor(() => expect(mocks.replace).toHaveBeenCalledWith("/joined/tasks"));
  client.clear();
});
