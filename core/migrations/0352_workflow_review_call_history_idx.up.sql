CREATE INDEX CONCURRENTLY workflow_review_call_history_idx ON public.agent_run USING btree (workflow_review_request_id) WHERE (workflow_review_request_id IS NOT NULL);
