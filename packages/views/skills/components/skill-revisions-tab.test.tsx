// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@silieco/core/i18n/react";
import type { Skill } from "@silieco/core/types";
import enSkills from "../../locales/en/skills.json";
import { SkillRevisionsTab } from "./skill-revisions-tab";
const mocks = vi.hoisted(() => ({ create: vi.fn(), list: vi.fn(), get: vi.fn() }));
vi.mock("@silieco/core/api", () => ({ api: { createSkillRevision: mocks.create, listSkillRevisions: mocks.list, getSkillRevision: mocks.get } }));
const skill = { id: "source", content: "# Saved", files: [{ path: "references/example.md", content: "Original file" }] } as Skill;
function mount(canEdit = true) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: 3 } } });
  return render(<QueryClientProvider client={client}><I18nProvider locale="en" resources={{ en: { skills: enSkills } }}><SkillRevisionsTab workspaceId="workspace" skill={skill} canEdit={canEdit} /></I18nProvider></QueryClientProvider>);
}
beforeEach(() => { vi.clearAllMocks(); mocks.list.mockResolvedValue({ revisions: [], next_cursor: null }); });
describe("skill revisions", () => {
  it("creates a separate candidate with saved files and explicit workspace", async () => {
    mocks.create.mockResolvedValue({ id: "revision", revision_hash: "sha256:example", bundle: { content: "# Candidate" }, manifest: {} });
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Create candidate" }));
    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByLabelText("Saved skill content")).toHaveValue("# Saved");
    fireEvent.change(within(dialog).getByLabelText("Candidate content"), { target: { value: "# Candidate" } });
    fireEvent.change(within(dialog).getByLabelText("Compatible contract versions", { exact: false }), { target: { value: "contract/v1\ncontract/v2" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Add tool" }));
    fireEvent.change(within(dialog).getByLabelText("Tool name"), { target: { value: "node" } });
    fireEvent.change(within(dialog).getByLabelText("Exact version"), { target: { value: "22.1.0" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create candidate" }));
    await waitFor(() => expect(mocks.create).toHaveBeenCalledWith("source", { content: "# Candidate", files: [{ path: "references/example.md", content: "Original file" }], compatible_contracts: ["contract/v1", "contract/v2"], tool_dependencies: [{ name: "node", version: "22.1.0" }] }, "workspace"));
    await screen.findByRole("heading", { name: "View revision" });
    expect(skill.content).toBe("# Saved");
  });
  it("preserves the draft after an uncertain response and never auto-resubmits", async () => {
    mocks.create.mockRejectedValue(new Error("Disconnected"));
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Create candidate" }));
    const dialog = screen.getByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Candidate content"), { target: { value: "# Keep draft" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create candidate" }));
    await screen.findByRole("alert");
    expect(screen.getByLabelText("Candidate content")).toHaveValue("# Keep draft");
    expect(mocks.create).toHaveBeenCalledTimes(1);
  });
  it("allows members to read while hiding candidate creation from non-editors", async () => {
    mount(false);
    await screen.findByText("No revisions yet.");
    expect(screen.queryByRole("button", { name: "Create candidate" })).not.toBeInTheDocument();
    expect(mocks.list).toHaveBeenCalledWith("source", undefined, 25, "workspace");
  });
});
