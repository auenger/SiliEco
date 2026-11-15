CREATE INDEX CONCURRENTLY skill_evaluation_suite_listing ON public.skill_evaluation_suite USING btree (workspace_id, skill_id, created_at DESC, id DESC);
