CREATE INDEX CONCURRENTLY skill_improvement_proposal_listing ON public.skill_improvement_proposal USING btree (workspace_id, skill_id, created_at DESC, id DESC);
