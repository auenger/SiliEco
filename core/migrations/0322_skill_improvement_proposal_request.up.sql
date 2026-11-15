CREATE UNIQUE INDEX CONCURRENTLY skill_improvement_proposal_request ON public.skill_improvement_proposal USING btree (workspace_id, created_by, request_id);
