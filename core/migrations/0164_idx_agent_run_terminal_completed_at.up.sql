CREATE INDEX CONCURRENTLY idx_agent_run_terminal_completed_at ON public.agent_run USING btree (completed_at) WHERE (status = ANY (ARRAY['completed'::text, 'failed'::text]));
