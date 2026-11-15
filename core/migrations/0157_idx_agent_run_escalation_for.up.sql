CREATE INDEX CONCURRENTLY idx_agent_run_escalation_for ON public.agent_run USING btree (escalation_for_run_id) WHERE (escalation_for_run_id IS NOT NULL);
