CREATE UNIQUE INDEX CONCURRENTLY skill_evaluation_suite_hash ON public.skill_evaluation_suite USING btree (workspace_id, skill_id, suite_hash);
