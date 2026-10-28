import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { usePageFeedbackStore, feedbackDraftKey, type BrowserBridge, type PageCapture } from "@silieco/core/page-feedback";
import { PageFeedbackPage } from "./page-feedback-page";

const submit = vi.hoisted(() => vi.fn(async () => "task-1"));
const prepareChat = vi.hoisted(() => vi.fn(async () => ({ sessionId: "chat-1", agentId: "agent-1" })));
vi.mock("@silieco/core/page-feedback", async (original) => ({ ...await original<typeof import("@silieco/core/page-feedback")>(), useSubmitFeedback: () => ({ mutateAsync: submit, isPending: false }), useFeedbackChat: () => ({ mutateAsync: prepareChat, isPending: false }) }));
const context = { userId: "user", workspaceId: "workspace", sessionId: "session" };
const capture: PageCapture = { id: "capture", title: "Settings", url: "https://example.com", capturedAt: "2026-10-24", target: { kind: "element", selector: "#save", text: "Save", name: "Save", rect: { x: 10, y: 20, width: 100, height: 30 }, viewport: { width: 1000, height: 800, scrollX: 0, scrollY: 0, devicePixelRatio: 2 } } };
describe("page feedback workbench", () => {
  beforeEach(() => { usePageFeedbackStore.setState({ drafts: {}, pending: {} }); submit.mockClear(); prepareChat.mockClear(); });
  const mount = () => {
    const bridge: BrowserBridge = { command: vi.fn(async (request) => request.type === "capture" ? capture : request.type === "image" ? "data:image/png;base64,aGVsbG8=" : null), subscribe: () => () => {} };
    const onCovered = vi.fn(), onTaskCreated = vi.fn(), onChatPrepared = vi.fn();
    renderWithI18n(<PageFeedbackPage context={context} bridge={bridge} state={{ url: "https://example.com", title: "Example", loading: false, canGoBack: false, canGoForward: false }} surface={<div />} onCovered={onCovered} onTaskCreated={onTaskCreated} onChatPrepared={onChatPrepared} />);
    return { bridge, onCovered, onTaskCreated, onChatPrepared };
  };
  it("collects selection evidence, covers the native surface and previews the requested change", async () => {
    const { onCovered } = mount();
    fireEvent.click(screen.getByRole("button", { name: "Element" }));
    await waitFor(() => expect(onCovered).toHaveBeenCalledWith(true));
    fireEvent.change(screen.getByLabelText("Describe the change for this selection…"), { target: { value: "Increase contrast" } });
    fireEvent.click(screen.getByText("Create manually (optional)"));
    fireEvent.click(screen.getByRole("button", { name: "Preview requirements" }));
    expect(screen.getByText(/Selector: #save/)).toHaveTextContent("Increase contrast");
    expect(usePageFeedbackStore.getState().drafts[feedbackDraftKey(context)]?.items[0]?.capture.id).toBe("capture");
  });
  it("submits its own draft, then clears it only after creation succeeds", async () => {
    const { onTaskCreated } = mount();
    fireEvent.click(screen.getByRole("button", { name: "Expand requirements" }));
    fireEvent.click(screen.getByText("Create manually (optional)"));
    fireEvent.change(screen.getByLabelText("Task title"), { target: { value: "Improve the page" } });
    fireEvent.change(screen.getByLabelText("What would you like to change? (optional)"), { target: { value: "Make the save action clearer" } });
    fireEvent.click(screen.getByRole("button", { name: "Create task" }));
    await waitFor(() => expect(onTaskCreated).toHaveBeenCalledWith("task-1"));
    expect(submit).toHaveBeenCalledWith(expect.objectContaining({ title: "Improve the page", request: "Make the save action clearer" }));
    expect(usePageFeedbackStore.getState().drafts[feedbackDraftKey(context)]).toBeUndefined();
  });
  it("starts with more browser space and preserves the draft across panel toggles", () => {
    mount();
    expect(screen.queryByLabelText("Task title")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Expand requirements" }));
    fireEvent.click(screen.getByText("Create manually (optional)"));
    fireEvent.change(screen.getByLabelText("Task title"), { target: { value: "Keep this draft" } });
    fireEvent.click(screen.getByRole("button", { name: "Collapse requirements" }));
    expect(screen.queryByLabelText("Task title")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Expand requirements" }));
    expect(screen.getByLabelText("Task title")).toHaveValue("Keep this draft");
  });
  it("captures page context for the agent without asking for a title or requirement form", async () => {
    const { bridge, onChatPrepared } = mount();
    fireEvent.click(screen.getByRole("button", { name: "Discuss with agent" }));
    await waitFor(() => expect(onChatPrepared).toHaveBeenCalledWith({ sessionId: "chat-1", agentId: "agent-1" }));
    expect(bridge.command).toHaveBeenCalledWith(expect.objectContaining({ type: "capture", mode: "page" }));
    expect(prepareChat).toHaveBeenCalledWith(expect.objectContaining({ draft: expect.objectContaining({ title: "", request: "", items: [expect.objectContaining({ capture })] }) }));
    expect(submit).not.toHaveBeenCalled();
    expect(usePageFeedbackStore.getState().drafts[feedbackDraftKey(context)]?.items).toHaveLength(1);
  });
});
