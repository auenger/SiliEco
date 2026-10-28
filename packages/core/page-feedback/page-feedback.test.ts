// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { browserCommandSchema, normalizeBrowserUrl, browserSessionFromPath, type PageCapture } from "./protocol";
import { emptyFeedbackDraft, feedbackDraftKey, usePageFeedbackStore, type FeedbackDraft } from "./draft-store";
import { submitFeedbackTask } from "./submit";
import type { Attachment, Issue, CreateIssueRequest } from "../types";

const capture: PageCapture = { id: "capture-1", url: "https://example.com/settings", title: "Settings", capturedAt: "2026-10-24T00:00:00Z", target: { kind: "element", selector: "#save", text: "Save", name: "Save", rect: { x: 10, y: 10, width: 40, height: 20 }, viewport: { width: 800, height: 600, scrollX: 0, scrollY: 20, devicePixelRatio: 2 } } };
const makeDraft = (): FeedbackDraft => ({ ...emptyFeedbackDraft, title: "Fix save", request: "Make the button visible", items: [{ capture, note: "Increase contrast", marks: [] }] });
function fixture() {
  let draft = makeDraft();
  const uploaded: Attachment[] = [];
  const api = {
    uploadFile: vi.fn(async (file: File) => { const a = { id: `attachment-${uploaded.length}`, markdown_url: `https://example.com/files/${uploaded.length}`, filename: file.name } as Attachment; uploaded.push(a); return a; }),
    createIssue: vi.fn(async (_data: CreateIssueRequest) => ({ id: "task-1" }) as Issue),
    updateIssue: vi.fn(async () => ({ id: "task-1" }) as Issue),
    listAttachments: vi.fn(async () => uploaded),
  };
  const deps = { api, guard: vi.fn(), image: vi.fn(async () => new File(["image"], "shot.png")), checkpoint: (patch: Partial<FeedbackDraft>) => { draft = { ...draft, ...patch }; } };
  return { api, deps, draft: () => draft, uploaded };
}

describe("page feedback isolation and submission", () => {
  beforeEach(() => usePageFeedbackStore.setState({ drafts: {}, pending: {} }));
  it("accepts web addresses and rejects privileged schemes and embedded credentials", () => {
    expect(normalizeBrowserUrl("example.com/path")).toBe("https://example.com/path");
    expect(normalizeBrowserUrl("http://localhost:3000")).toBe("http://localhost:3000/");
    expect(normalizeBrowserUrl("localhost:3000")).toBe("http://localhost:3000/");
    for (const value of ["javascript:alert(1)", "file:///etc/passwd", "data:text/html,test", "https://user:pass@example.com"]) expect(() => normalizeBrowserUrl(value)).toThrow();
    expect(browserCommandSchema.safeParse({ type: "execute", code: "anything" }).success).toBe(false);
    expect(browserSessionFromPath("/team/browser/one?x=1")).toBe("one");
    expect(browserSessionFromPath("/team/tasks/one")).toBeNull();
  });
  it("keeps drafts separate across users, workspaces and tabs", () => {
    const context = { userId: "u1", workspaceId: "w1", sessionId: "s1" };
    const key = feedbackDraftKey(context);
    usePageFeedbackStore.getState().patch(key, { title: "Private draft" });
    for (const other of [{ ...context, userId: "u2" }, { ...context, workspaceId: "w2" }, { ...context, sessionId: "s2" }]) expect(usePageFeedbackStore.getState().drafts[feedbackDraftKey(other)]).toBeUndefined();
  });
  it("creates a backlog task with image and machine-readable evidence", async () => {
    const f = fixture();
    await expect(submitFeedbackTask(f.draft(), f.deps)).resolves.toBe("task-1");
    expect(f.api.createIssue).toHaveBeenCalledWith(expect.objectContaining({ status: "backlog", attachment_ids: ["attachment-0", "attachment-1"], description: expect.stringContaining("> Selector: #save") }));
    expect(f.api.createIssue.mock.calls[0]?.[0]).not.toHaveProperty("assignee_id");
  });
  it("retries missing attachments on the existing task without duplicate uploads or create", async () => {
    const f = fixture();
    f.api.listAttachments.mockResolvedValue([]);
    await expect(submitFeedbackTask(f.draft(), f.deps)).rejects.toThrow("Task created");
    expect(f.draft().taskId).toBe("task-1");
    f.api.listAttachments.mockResolvedValue(f.uploaded);
    await submitFeedbackTask(f.draft(), f.deps);
    expect(f.api.createIssue).toHaveBeenCalledTimes(1);
    expect(f.api.uploadFile).toHaveBeenCalledTimes(2);
  });
  it("stops before another request after workspace changes during an upload", async () => {
    const f = fixture(); let switched = false;
    f.deps.guard.mockImplementation(() => { if (switched) throw new Error("workspace changed"); });
    f.api.uploadFile.mockImplementation(async () => { switched = true; return { id: "uploaded" } as Attachment; });
    await expect(submitFeedbackTask(f.draft(), f.deps)).rejects.toThrow("workspace changed");
    expect(f.api.createIssue).not.toHaveBeenCalled();
    expect(f.api.uploadFile).toHaveBeenCalledTimes(1);
  });
  it("records a successful create even if the user switched workspaces while waiting", async () => {
    const f = fixture(); let switched = false;
    f.deps.guard.mockImplementation(() => { if (switched) throw new Error("workspace changed"); });
    f.api.createIssue.mockImplementation(async () => { switched = true; return { id: "created-in-original-workspace" } as Issue; });
    await expect(submitFeedbackTask(f.draft(), f.deps)).rejects.toThrow("workspace changed");
    expect(f.draft().taskId).toBe("created-in-original-workspace");
    expect(f.api.listAttachments).not.toHaveBeenCalled();
  });
});
