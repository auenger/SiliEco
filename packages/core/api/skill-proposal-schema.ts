import { z } from "zod";

const uuid = z.string().uuid();
const evidenceSchema = z.object({
  execution_id: uuid.optional(),
  workflow_instance_id: uuid.optional(),
  workflow_stage_id: uuid.optional(),
  attempt: z.number().int().positive().optional(),
  plan_revision: z.number().int().positive().optional(),
  result_id: uuid.optional(),
  result_revision: z.number().int().positive().optional(),
  result_hash: z.string().regex(/^sha256:[0-9a-f]{64}$/).optional(),
}).loose().refine((value) => {
  const execution = [value.execution_id, value.workflow_instance_id, value.workflow_stage_id, value.attempt, value.plan_revision];
  const result = [value.result_id, value.result_revision, value.result_hash];
  return (execution.every((field) => field === undefined) || execution.every((field) => field !== undefined)) &&
    (result.every((field) => field === undefined) || (value.execution_id !== undefined && result.every((field) => field !== undefined)));
}, "Incomplete proposal evidence");

export const SkillImprovementProposalSchema = z.object({
  id: uuid,
  workspace_id: uuid,
  skill_id: uuid,
  request_id: uuid,
  baseline_revision_id: uuid,
  candidate_revision_id: uuid,
  category: z.string().min(1),
  reason: z.string().min(1),
  hypothesis: z.string().min(1),
  applicable_scope: z.string().min(1),
  evidence: evidenceSchema,
  created_by: uuid,
  created_at: z.string(),
}).loose().refine((value) => value.baseline_revision_id !== value.candidate_revision_id, "Proposal revisions must differ");

export const SkillImprovementProposalPageSchema = z.object({
  proposals: z.array(SkillImprovementProposalSchema).max(100),
  next_cursor: uuid.nullable(),
}).loose().refine((value) => new Set(value.proposals.map((proposal) => proposal.id)).size === value.proposals.length &&
  (value.next_cursor === null || value.proposals.at(-1)?.id === value.next_cursor), "Invalid proposal page cursor");

export type SkillImprovementProposal = z.infer<typeof SkillImprovementProposalSchema>;
export type SkillImprovementProposalPage = z.infer<typeof SkillImprovementProposalPageSchema>;
export type SkillProposalCategory = "skill_instruction" | "insufficient_input" | "tool_failure" | "contract_issue" | "model_capability" | "human_correction" | "positive_example";
export interface CreateSkillImprovementProposalInput {
  request_id: string;
  baseline_revision_id: string;
  candidate_revision_id: string;
  source_execution_id?: string;
  source_result_revision?: number;
  category: SkillProposalCategory;
  reason: string;
  hypothesis: string;
  applicable_scope: string;
}
