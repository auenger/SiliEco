CREATE UNIQUE INDEX CONCURRENTLY idx_one_pending_run_per_task_agent ON public.agent_run USING btree (task_id, agent_id) WHERE (status = ANY (ARRAY['queued'::text, 'dispatched'::text]));
