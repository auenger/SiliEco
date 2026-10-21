// @vitest-environment jsdom
import { beforeEach, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@silieco/core/i18n/react";
import enSkills from "../../locales/en/skills.json";
import { ApiError } from "@silieco/core/api";
import { SkillProposalsTab } from "./skill-proposals-tab";
const mocks = vi.hoisted(() => ({ create: vi.fn(), list: vi.fn(), detail: vi.fn(), revisions: vi.fn(), revision: vi.fn() }));
vi.mock("@silieco/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@silieco/core/api")>();
  return {...actual, api: {createSkillImprovementProposal: mocks.create, listSkillImprovementProposals: mocks.list, getSkillImprovementProposal: mocks.detail, listSkillRevisions: mocks.revisions, getSkillRevision: mocks.revision}};
});
const baseline = {id: "baseline", revision_hash: `sha256:${"a".repeat(64)}`, created_at: "2026-09-09"};
const candidate = {id: "candidate", revision_hash: `sha256:${"b".repeat(64)}`, created_at: "2026-09-09"};
function mount(canEdit = true) {
  return render(<QueryClientProvider client={new QueryClient({defaultOptions: {queries: {retry: false}}})}><I18nProvider locale="en" resources={{en: {skills: enSkills}}}><SkillProposalsTab workspaceId="workspace" skillId="skill" canEdit={canEdit} /></I18nProvider></QueryClientProvider>);
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.list.mockResolvedValue({proposals: [], next_cursor: null});
  mocks.revisions.mockResolvedValue({revisions: [candidate, baseline], next_cursor: null});
  mocks.revision.mockImplementation((_id: string, revisionId: string) => Promise.resolve({id: revisionId, revision_hash: revisionId, bundle: {content: `# ${revisionId}`}, manifest: {}}));
});
async function choose(label: string, name: RegExp) {
  fireEvent.click(screen.getByRole("combobox", {name: label}));
  const option = await screen.findByRole("option", {name});
  fireEvent.mouseMove(option); fireEvent.click(option);
}
async function fill() {
  fireEvent.click(screen.getByRole("button", {name: "Create proposal"}));
  await choose("Baseline revision", /aaaaaaaa/);
  await choose("Candidate revision", /bbbbbbbb/);
  const dialog = screen.getByRole("dialog");
  fireEvent.change(within(dialog).getByLabelText("Reason for the change"), {target: {value: "A missing check"}});
  fireEvent.change(within(dialog).getByLabelText("Improvement hypothesis"), {target: {value: "Explicit steps help"}});
  fireEvent.change(within(dialog).getByLabelText("Applicable scope"), {target: {value: "Reports"}});
  return dialog;
}
it("submits selected immutable revisions and renders proposal analysis", async () => {
  mocks.create.mockImplementation((_id, data) => Promise.resolve({...data, id: "proposal", evidence: {}, created_at: "2026-09-09"}));
  mount(); const dialog = await fill();
  expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  fireEvent.click(within(dialog).getByRole("button", {name: "Submit proposal"}));
  await screen.findByRole("heading", {name: "View proposal"});
  expect(mocks.create).toHaveBeenCalledWith("skill", expect.objectContaining({request_id: expect.any(String), baseline_revision_id: "baseline", candidate_revision_id: "candidate", category: "human_correction", reason: "A missing check", hypothesis: "Explicit steps help", applicable_scope: "Reports"}), "workspace");
  expect(await screen.findByText("Manual feedback; no execution evidence attached.")).toBeVisible();
}, 15000);
it("locks an uncertain submission and retries the identical request", async () => {
  mocks.create.mockRejectedValue(new Error("Connection lost"));
  mount(); const dialog = await fill();
  fireEvent.click(within(dialog).getByRole("button", {name: "Submit proposal"}));
  await screen.findByRole("alert");
  expect(screen.getByLabelText("Reason for the change")).toBeDisabled();
  expect(screen.getByRole("combobox", {name: "Baseline revision"})).toBeDisabled();
  const first = mocks.create.mock.calls[0];
  fireEvent.click(screen.getByRole("button", {name: "Retry same request"}));
  await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(2));
  expect(mocks.create.mock.calls[1]).toEqual(first);
});
it("explains missing revisions and prevents incomplete submission", async () => {
  mocks.revisions.mockResolvedValue({revisions: [baseline], next_cursor: null});
  mount(); fireEvent.click(screen.getByRole("button", {name: "Create proposal"}));
  await screen.findByText("Create at least two distinct revisions in Versions before submitting a proposal.");
  expect(screen.getByRole("button", {name: "Submit proposal"})).toBeDisabled();
  expect(mocks.create).not.toHaveBeenCalled();
});
it("hides creation from non-editors", async () => {
  mount(false); await screen.findByText("No improvement proposals yet.");
  expect(screen.queryByRole("button", {name: "Create proposal"})).not.toBeInTheDocument();
});

it("allows correction after a definitive rejection", async () => {
  mocks.create.mockRejectedValueOnce(new ApiError("Invalid evidence", 400, "Bad Request"));
  mocks.create.mockImplementationOnce((_id, data) => Promise.resolve({...data, id: "proposal", evidence: {}, created_at: "2026-09-09"}));
  mount(); const dialog = await fill();
  fireEvent.click(within(dialog).getByRole("button", {name: "Submit proposal"}));
  await screen.findByRole("alert");
  expect(screen.getByLabelText("Reason for the change")).not.toBeDisabled();
  fireEvent.change(screen.getByLabelText("Reason for the change"), {target: {value: "Corrected reason"}});
  fireEvent.click(within(dialog).getByRole("button", {name: "Submit proposal"}));
  await screen.findByRole("heading", {name: "View proposal"});
  expect(mocks.create.mock.calls[1]?.[1]).toMatchObject({request_id: mocks.create.mock.calls[0]?.[1].request_id, reason: "Corrected reason"});
}, 15000);
