import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@silieco/core/i18n/react";
import workspaceEn from "../locales/en/workspace.json";
import en from "../locales/en/onboarding.json";
const mocks = vi.hoisted(() => ({ complete: vi.fn(), onComplete: vi.fn(), error: vi.fn(), logout: vi.fn() }));
vi.mock("@silieco/core/auth", () => ({ useAuthStore: (select: (s: unknown) => unknown) => select({ user: { id: "user" } }) }));
vi.mock("@silieco/core/onboarding", () => ({ completeOnboarding: mocks.complete, ONBOARDING_STEP_ORDER: ["runtime", "workspace", "workspace_description"], useWelcomeStore: { getState: () => ({ set: vi.fn() }) } }));
vi.mock("@silieco/core/workspace/queries", () => ({ workspaceListOptions: () => ({ queryKey: ["workspaces"], queryFn: async () => [] }) }));
vi.mock("../auth", () => ({ useLogout: () => mocks.logout }));
vi.mock("sonner", () => ({ toast: { error: mocks.error } }));
vi.mock("./steps/step-welcome", () => ({ StepWelcome: ({ onNext }: { onNext: () => void }) => <button onClick={onNext}>Continue</button> }));
vi.mock("./steps/step-runtime-detection", () => ({ StepRuntimeDetection: () => <div>Runtime setup</div> }));
vi.mock("./steps/step-workspace", () => ({ StepWorkspace: () => null }));
vi.mock("./steps/step-workspace-description", () => ({ StepWorkspaceDescription: () => null }));
import { OnboardingFlow } from "./onboarding-flow";
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><I18nProvider locale="en" resources={{ en: { onboarding: en, workspace: workspaceEn } }}><OnboardingFlow onComplete={mocks.onComplete} /></I18nProvider></QueryClientProvider>);
  return client;
}
beforeEach(() => { vi.clearAllMocks(); mocks.complete.mockResolvedValue(undefined); });
it("lets a new user skip all setup without creating a workspace", async () => {
  const client = setup();
  const skip = screen.getByRole("button", { name: "Skip all for now" });
  await waitFor(() => expect(skip).not.toBeDisabled());
  fireEvent.click(skip);
  await waitFor(() => expect(mocks.onComplete).toHaveBeenCalledWith(undefined));
  expect(mocks.complete).toHaveBeenCalledTimes(1);
  client.clear();
});
it("keeps skip available on subsequent steps and stays put on failure", async () => {
  mocks.complete.mockRejectedValue(new Error("Network failed"));
  const client = setup();
  fireEvent.click(screen.getByText("Continue"));
  expect(screen.getByText("Runtime setup")).toBeInTheDocument();
  const skip = screen.getByRole("button", { name: "Skip all for now" });
  await waitFor(() => expect(skip).not.toBeDisabled());
  fireEvent.click(skip);
  await waitFor(() => expect(mocks.error).toHaveBeenCalledWith("Network failed"));
  expect(mocks.onComplete).not.toHaveBeenCalled();
  client.clear();
});

it("allows switching accounts without completing onboarding", () => {
  const client = setup();
  fireEvent.click(screen.getByRole("button", { name: "Log out" }));
  expect(mocks.logout).toHaveBeenCalledOnce();
  expect(mocks.complete).not.toHaveBeenCalled();
  client.clear();
});
