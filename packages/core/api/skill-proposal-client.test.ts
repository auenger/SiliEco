import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { SkillImprovementProposalSchema, type CreateSkillImprovementProposalInput } from "./skill-proposal-schema";
const id = "11111111-1111-4111-8111-111111111111";
const other = "22222222-2222-4222-8222-222222222222";
const input: CreateSkillImprovementProposalInput = {request_id: id, baseline_revision_id: id, candidate_revision_id: other, category: "human_correction", reason: "Correction", hypothesis: "Improve reports", applicable_scope: "Reports"};
const proposal = {...input, id, skill_id: id, workspace_id: id, evidence: {}, created_by: id, created_at: "2026-09-09"};
afterEach(() => vi.unstubAllGlobals());
function respond(value: unknown) {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(value), {status: 200, headers: {"Content-Type": "application/json"}}));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}
it("creates a workspace-bound proposal with a stable caller request ID", async () => {
  const fetch = respond(proposal);
  await expect(new ApiClient("https://api.example.test").createSkillImprovementProposal(id, input, id)).resolves.toMatchObject({id});
  expect(fetch.mock.calls[0]?.[1]).toMatchObject({method: "POST", body: JSON.stringify(input), headers: {"X-Workspace-ID": id, "X-Workspace-Slug": ""}});
});
it.each([{}, {...proposal, workspace_id: other}, {...proposal, skill_id: other}, {...proposal, request_id: other}, {...proposal, evidence: {result_id: id}}, {...proposal, candidate_revision_id: id}])("rejects malformed or mismatched writes without resubmission", async (value) => {
  const fetch = respond(value);
  await expect(new ApiClient("https://api.example.test").createSkillImprovementProposal(id, input, id)).rejects.toThrow("Invalid skill proposal response");
  expect(fetch).toHaveBeenCalledTimes(1);
});
it("rejects a detail returned for another proposal", async () => {
  respond(proposal);
  await expect(new ApiClient("https://api.example.test").getSkillImprovementProposal(id, other, id)).rejects.toThrow("Invalid skill proposal response");
});
it("accepts complete execution/result provenance and future classifications", () => {
  const evidence = {execution_id: id, workflow_instance_id: id, workflow_stage_id: id, attempt: 1, plan_revision: 1, result_id: id, result_revision: 2, result_hash: `sha256:${"a".repeat(64)}`};
  expect(SkillImprovementProposalSchema.safeParse({...proposal, category: "future_classification", evidence}).success).toBe(true);
  expect(SkillImprovementProposalSchema.safeParse({...proposal, evidence: {...evidence, workflow_stage_id: undefined}}).success).toBe(false);
});
it("loads a scoped page and passes pagination parameters", async () => {
  const fetch = respond({proposals: [proposal], next_cursor: null});
  await expect(new ApiClient("https://api.example.test").listSkillImprovementProposals(id, id, other, 10)).resolves.toMatchObject({next_cursor: null});
  expect(fetch.mock.calls[0]?.[0]).toBe(`https://api.example.test/api/skills/${id}/proposals?page_size=10&cursor=${other}`);
});
it.each([{proposals: [proposal, proposal], next_cursor: null}, {proposals: [proposal], next_cursor: other}, {proposals: [{...proposal, workspace_id: other}], next_cursor: null}, {proposals: [proposal], next_cursor: id}])("rejects ambiguous or out-of-scope pages", async (value) => {
  respond(value);
  await expect(new ApiClient("https://api.example.test").listSkillImprovementProposals(id, id, id)).rejects.toThrow("Invalid skill proposal page");
});
it("does not send unscoped requests", async () => {
  const fetch = respond(proposal);
  await expect(new ApiClient("https://api.example.test").createSkillImprovementProposal(id, input, "")).rejects.toThrow("A workspace is required");
  expect(fetch).not.toHaveBeenCalled();
});

it("rejects an otherwise valid write response with unrequested evidence", async () => {
  respond({...proposal, evidence: {execution_id: id, workflow_instance_id: id, workflow_stage_id: id, attempt: 1, plan_revision: 1}});
  await expect(new ApiClient("https://api.example.test").createSkillImprovementProposal(id, input, id)).rejects.toThrow("Invalid skill proposal response");
});
